package multiuser

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

// holdCreatesOpen makes every insert_edition wait until the returned func is
// called. The func is idempotent and also runs at cleanup so no request leaks.
func holdCreatesOpen(t *testing.T, f *editionFixture) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	f.hardcover.HoldInsert(gate)
	var once atomic.Bool
	release = func() {
		if once.CompareAndSwap(false, true) {
			close(gate)
		}
	}
	t.Cleanup(release)
	return release
}

func startHeldCreate(t *testing.T, f *editionFixture) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
		if err == nil && (created.EditionID != 777 || len(created.Warnings) != 0) {
			err = context.DeadlineExceeded // any non-nil marker: the create must return the edition with no warnings
		}
		done <- err
	}()
	select {
	case <-f.hardcover.Entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the create never reached Hardcover")
	}
	return done
}

func newHeldCreateFixture(t *testing.T) *editionFixture {
	t.Helper()
	return newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
}

func newHeldDraftFixture(t *testing.T) *editionFixture {
	t.Helper()
	return newHeldCreateFixture(t)
}

// installCancelableRun registers an active run for a second profile, like a sync
// worker would, and reports whether shutdown invoked its cancel function.
func installCancelableRun(t *testing.T, service *MultiUserService, profileID string) *atomic.Bool {
	t.Helper()
	require.NoError(t, service.repository.CreateProfile(
		profileID, "Running", "http://audiobookshelf.invalid", "abs-token", "hc-token", database.SyncConfigData{},
	))
	queuedAt := time.Now().UTC()
	report := acceptTestSyncRun(t, service.repository, profileID, "run-active", false, queuedAt)
	run := activeSyncRun{generation: report.Generation, runID: "run-active", startedAt: queuedAt, profileName: profileID}
	cancelled := &atomic.Bool{}
	service.syncMutex.Lock()
	service.activeSyncs[profileID] = func() { cancelled.Store(true) }
	service.activeRuns[profileID] = run
	service.latestRuns[profileID] = run
	service.syncMutex.Unlock()
	return cancelled
}

func TestShutdownCancelsRunningSyncsWhileAnEditionCreateIsInFlight(t *testing.T) {
	f := newHeldCreateFixture(t)
	syncCancelled := installCancelableRun(t, f.service, "profile-running")
	release := holdCreatesOpen(t, f)
	createDone := startHeldCreate(t, f)

	shortCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := f.service.Shutdown(shortCtx)

	require.ErrorIs(t, err, context.DeadlineExceeded, "shutdown must report that the create outlived its context")
	require.True(t, syncCancelled.Load(), "running syncs must be cancelled even though a create is still in flight")

	release()
	select {
	case err := <-createDone:
		require.NoError(t, err, "the in-flight create must finish with its edition and no warnings")
	case <-time.After(10 * time.Second):
		t.Fatal("the create did not finish after release")
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	require.NoError(t, f.service.Shutdown(drainCtx))
}

func TestDeleteProfileWaitsForAnInFlightEditionCreate(t *testing.T) {
	f := newHeldCreateFixture(t)
	release := holdCreatesOpen(t, f)
	createDone := startHeldCreate(t, f)

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- f.service.DeleteProfile("profile-1") }()
	select {
	case err := <-deleteDone:
		t.Fatalf("DeleteProfile returned while a create was in flight: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	release()
	for name, done := range map[string]<-chan error{"create": createDone, "delete": deleteDone} {
		select {
		case err := <-done:
			require.NoError(t, err, name)
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not finish after release", name)
		}
	}
}

func TestDeleteProfileKeepsAdmittedCreateProfileActiveUntilLookupCompletes(t *testing.T) {
	f := newHeldCreateFixture(t)
	profileLookupEntered := make(chan struct{})
	releaseProfileLookup := make(chan struct{})
	var lookupEnteredOnce sync.Once
	var releaseLookupOnce sync.Once
	const callbackName = "multiuser_test_hold_admitted_edition_profile_lookup"
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "SyncProfile" {
			return
		}
		lookupEnteredOnce.Do(func() { close(profileLookupEntered) })
		<-releaseProfileLookup
	}))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove(callbackName)) })
	defer releaseLookupOnce.Do(func() { close(releaseProfileLookup) })

	createDone := make(chan error, 1)
	go func() {
		_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
		createDone <- err
	}()
	select {
	case <-profileLookupEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the admitted create did not reach profile lookup")
	}

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- f.service.DeleteProfile("profile-1") }()
	gate := f.service.profileGate("profile-1")
	require.Eventually(t, func() bool {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		return gate.deleted
	}, time.Second, time.Millisecond, "deletion should close the profile gate while waiting for admitted work")

	var active bool
	require.NoError(t, f.db.Raw("SELECT active FROM sync_profiles WHERE id = ?", "profile-1").Scan(&active).Error)
	require.True(t, active, "the repository profile must remain active until the admitted create resolves it")

	releaseLookupOnce.Do(func() { close(releaseProfileLookup) })
	select {
	case err := <-createDone:
		require.NoError(t, err, "the create admitted before deletion must retain access to profile state")
	case <-time.After(10 * time.Second):
		t.Fatal("the admitted create did not finish after profile lookup was released")
	}
	select {
	case err := <-deleteDone:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("profile deletion did not finish after the admitted create")
	}
}

func TestInFlightEditionDraftDoesNotDelayShutdown(t *testing.T) {
	f := newHeldDraftFixture(t)
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	draftDone := make(chan error, 1)
	go func() {
		_, err := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
		draftDone <- err
	}()
	select {
	case <-f.abs.ReadEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the draft never reached Audiobookshelf")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	require.NoError(t, f.service.Shutdown(shutdownCtx), "an in-flight draft must not block shutdown")
	require.Less(t, time.Since(started), 2*time.Second)

	_, err := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
	require.ErrorIs(t, err, ErrServiceShuttingDown)
	_, err = f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.ErrorIs(t, err, ErrServiceShuttingDown)

	f.abs.ReleaseReads()
	select {
	case <-draftDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the draft did not finish after release")
	}
}

func TestPrepareEditionDraftStopsOnCallerCancellation(t *testing.T) {
	f := newHeldDraftFixture(t)
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	ctx, cancel := context.WithCancel(context.Background())
	draftDone := make(chan error, 1)
	go func() {
		_, err := f.service.PrepareEditionDraft(ctx, "profile-1", "run-1", "item-1")
		draftDone <- err
	}()

	select {
	case <-f.abs.ReadEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the draft never reached Audiobookshelf")
	}
	cancel()

	select {
	case err := <-draftDone:
		require.ErrorIs(t, err, context.Canceled)
		var upstream *EditionUpstreamError
		require.False(t, errors.As(err, &upstream), "caller cancellation must not become an upstream failure")
	case <-time.After(2 * time.Second):
		t.Fatal("the draft did not stop after its caller canceled")
	}
}

func TestPrepareEditionDraftReturnsCallerDeadlineWithoutUpstreamClassification(t *testing.T) {
	f := newHeldDraftFixture(t)
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := f.service.PrepareEditionDraft(ctx, "profile-1", "run-1", "item-1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var upstream *EditionUpstreamError
	require.False(t, errors.As(err, &upstream), "caller deadline must not become an upstream failure")
}

func TestPrepareEditionDraftOverallTimeoutStopsBlockedAudiobookshelfRead(t *testing.T) {
	f := newHeldDraftFixture(t)
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	const timeout = 50 * time.Millisecond
	draftDone := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := f.service.prepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1", timeout)
		draftDone <- err
	}()

	select {
	case <-f.abs.ReadEntered:
	case <-time.After(time.Second):
		t.Fatal("the draft never reached Audiobookshelf")
	}

	select {
	case err := <-draftDone:
		require.ErrorIs(t, err, context.DeadlineExceeded)
		var upstream *EditionUpstreamError
		require.ErrorAs(t, err, &upstream)
		require.Equal(t, "audiobookshelf", upstream.Service)
		require.Less(t, time.Since(started), time.Second, "the overall draft timeout must cancel a blocked lookup")
	case <-time.After(time.Second):
		t.Fatal("the draft did not stop at its overall timeout")
	}
}

func TestEditionCreateBudgetStartsBeforeAudiobookshelfRefetch(t *testing.T) {
	f := newHeldCreateFixture(t)
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	done := make(chan error, 1)
	go func() {
		_, err := f.service.createEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits(), 50*time.Millisecond)
		done <- err
	}()
	select {
	case <-f.abs.ReadEntered:
	case <-time.After(time.Second):
		t.Fatal("the create never started the ABS refetch")
	}
	select {
	case err := <-done:
		var upstream *EditionUpstreamError
		require.ErrorAs(t, err, &upstream)
		require.Equal(t, "audiobookshelf", upstream.Service)
	case <-time.After(time.Second):
		t.Fatal("the operation deadline did not bound the ABS refetch")
	}
	require.Zero(t, f.hardcover.RequestCount(), "an expired ABS stage must not start Hardcover resolution or insertion")
}

func TestPrepareEditionDraftFallsBackWhenAudnexExceedsItsBudget(t *testing.T) {
	item := editionItem("item-1", "A Title", "An Author")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	metadata["asin"] = "B0SLOWAUDNEX1"
	metadata["publishedDate"] = "2024-06-01"
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)
	stubAudnexTransport(t, func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	started := time.Now()
	built, err := f.service.prepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1", 2*time.Second)
	require.NoError(t, err)
	require.Equal(t, "2024-06-01", built.ReleaseDate, "slow optional enrichment must fall back to the Audiobookshelf date")
	require.Less(t, time.Since(started), 2*time.Second, "optional enrichment must leave time before the overall deadline")
	require.Zero(t, f.hardcover.RequestCount())
}

func TestPrepareEditionDraftCallerCancellationStopsAudnexLookup(t *testing.T) {
	item := editionItem("item-1", "A Title", "An Author")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	metadata["asin"] = "B0CANCELAUDNEX"
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)
	audnexEntered := make(chan struct{})
	stubAudnexTransport(t, func(request *http.Request) (*http.Response, error) {
		close(audnexEntered)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	draftDone := make(chan error, 1)
	go func() {
		_, err := f.service.PrepareEditionDraft(ctx, "profile-1", "run-1", "item-1")
		draftDone <- err
	}()

	select {
	case <-audnexEntered:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("the draft never reached Audnex")
	}
	select {
	case err := <-draftDone:
		require.ErrorIs(t, err, context.Canceled)
		var upstream *EditionUpstreamError
		require.False(t, errors.As(err, &upstream), "caller cancellation must not become an upstream failure")
	case <-time.After(2 * time.Second):
		t.Fatal("the draft did not stop after its caller canceled")
	}
}
