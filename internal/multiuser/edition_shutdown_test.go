package multiuser

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestInFlightEditionDraftDoesNotDelayShutdown(t *testing.T) {
	f := newHeldCreateFixture(t)
	holdReads := make(chan struct{})
	f.hardcover.HoldReads(holdReads)
	t.Cleanup(f.hardcover.ReleaseHolds)

	draftDone := make(chan error, 1)
	go func() {
		_, err := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
		draftDone <- err
	}()
	select {
	case <-f.hardcover.ReadEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the draft never reached Hardcover")
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

	close(holdReads)
	f.hardcover.HoldReads(nil)
	select {
	case <-draftDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the draft did not finish after release")
	}
}
