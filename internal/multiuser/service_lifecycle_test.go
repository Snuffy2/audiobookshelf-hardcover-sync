package multiuser

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

func TestGetProfileStatusUsesPreloadedSyncState(t *testing.T) {
	lastSync := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{},
		activeRuns:      make(map[string]activeSyncRun),
		syncServices:    make(map[string]*syncsvc.Service),
	}

	status := service.getProfileStatus(
		"profile-a",
		&database.SyncProfile{ID: "profile-a", Name: "A"},
		&database.ProfileSyncState{LastSync: &lastSync},
		true,
	)
	if status.ProfileName != "A" || status.LastSync == nil || !status.LastSync.Equal(lastSync) {
		t.Fatalf("preloaded last sync was not used: %#v", status)
	}
}

func TestPublishFinalStatusDoesNotHoldSyncMutexDuringPersistence(t *testing.T) {
	lastSync := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	persistence := &blockingSyncStateRepository{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		state:   &database.ProfileSyncState{ProfileID: "profile-a"},
	}
	run := activeSyncRun{generation: 1, runID: "run-a", startedAt: lastSync}
	service := &MultiUserService{
		stateRepository: persistence,
		profileStatuses: map[string]*SyncProfileStatus{
			"profile-a": {ProfileID: "profile-a", Status: "syncing"},
		},
		activeRuns:   map[string]activeSyncRun{"profile-a": run},
		syncServices: make(map[string]*syncsvc.Service),
	}
	status := &SyncProfileStatus{ProfileID: "profile-a", Status: "completed", LastSync: &lastSync}
	snapshot := newRunSnapshot("profile-a", run, "completed")
	applySnapshotToStatus(status, snapshot)

	publishDone := make(chan bool, 1)
	go func() { publishDone <- service.publishFinalStatus("profile-a", run.generation, status) }()
	<-persistence.entered

	pollDone := make(chan struct{})
	go func() {
		_ = service.GetProfileStatus("profile-a")
		close(pollDone)
	}()
	select {
	case <-pollDone:
	case <-time.After(time.Second):
		t.Fatal("status polling remained blocked during persistence")
	}
	close(persistence.release)
	if !<-publishDone {
		t.Fatal("final status was not published")
	}
}

func TestCancelSyncRetainsCanceledSnapshotUntilFinalPublication(t *testing.T) {
	started := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	run := activeSyncRun{generation: 1, runID: "run-a", startedAt: started}
	cancelCalled := false
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{},
		activeSyncs: map[string]context.CancelFunc{
			"profile-a": func() { cancelCalled = true },
		},
		activeRuns:   map[string]activeSyncRun{"profile-a": run},
		syncServices: make(map[string]*syncsvc.Service),
	}
	initial := &SyncProfileStatus{ProfileID: "profile-a", Status: "syncing"}
	applySnapshotToStatus(initial, newRunSnapshot("profile-a", run, "syncing"))
	service.updateProfileStatus("profile-a", initial)

	if err := service.CancelSync("profile-a"); err != nil {
		t.Fatalf("CancelSync failed: %v", err)
	}
	if !cancelCalled {
		t.Fatal("cancel function was not called")
	}
	status := service.GetProfileStatus("profile-a")
	if status.Status != "idle" || status.Snapshot == nil || status.Snapshot.State != "canceled" {
		t.Fatalf("canceled snapshot was not retained: %#v", status)
	}
	if status.Snapshot.RunID != run.runID {
		t.Fatalf("canceled run ID was lost: %#v", status.Snapshot)
	}

	final := &SyncProfileStatus{ProfileID: "profile-a", Status: "idle", LastSync: timePtr(started.Add(time.Minute))}
	finalSnapshot := newRunSnapshot("profile-a", run, "canceled")
	finalSnapshot.ProcessedSoFar = 2
	finalSnapshot.TotalBooksProcessed = 2
	applySnapshotToStatus(final, finalSnapshot)
	if !service.publishFinalStatus("profile-a", run.generation, final) {
		t.Fatal("canceled final snapshot was rejected")
	}
	status = service.GetProfileStatus("profile-a")
	if status.Snapshot == nil || status.Snapshot.ProcessedSoFar != 2 {
		t.Fatalf("final canceled snapshot was not published: %#v", status)
	}
}

func TestCanceledRunCannotPublishAfterReplacement(t *testing.T) {
	oldRun := activeSyncRun{generation: 1, runID: "run-old", startedAt: time.Now().UTC()}
	newRun := activeSyncRun{generation: 2, runID: "run-new", startedAt: time.Now().UTC()}
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{},
		activeSyncs:     map[string]context.CancelFunc{"profile-a": func() {}},
		activeRuns:      map[string]activeSyncRun{"profile-a": oldRun},
		syncServices:    make(map[string]*syncsvc.Service),
	}
	initial := &SyncProfileStatus{ProfileID: "profile-a", Status: "syncing"}
	applySnapshotToStatus(initial, newRunSnapshot("profile-a", oldRun, "syncing"))
	service.updateProfileStatus("profile-a", initial)
	if err := service.CancelSync("profile-a"); err != nil {
		t.Fatalf("CancelSync failed: %v", err)
	}

	service.syncMutex.Lock()
	service.activeRuns["profile-a"] = newRun
	service.syncMutex.Unlock()
	oldFinal := &SyncProfileStatus{ProfileID: "profile-a", Status: "idle", LastSync: timePtr(oldRun.startedAt.Add(time.Minute))}
	oldSnapshot := newRunSnapshot("profile-a", oldRun, "canceled")
	applySnapshotToStatus(oldFinal, oldSnapshot)
	if service.publishFinalStatus("profile-a", oldRun.generation, oldFinal) {
		t.Fatal("canceled stale run published after replacement")
	}
}

func TestCanceledSnapshotPollingIsRaceSafe(t *testing.T) {
	run := activeSyncRun{generation: 1, runID: "run-a", startedAt: time.Now().UTC()}
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{},
		activeSyncs:     map[string]context.CancelFunc{"profile-a": func() {}},
		activeRuns:      map[string]activeSyncRun{"profile-a": run},
		syncServices:    make(map[string]*syncsvc.Service),
	}
	initial := &SyncProfileStatus{ProfileID: "profile-a", Status: "syncing"}
	applySnapshotToStatus(initial, newRunSnapshot("profile-a", run, "syncing"))
	service.updateProfileStatus("profile-a", initial)
	if err := service.CancelSync("profile-a"); err != nil {
		t.Fatalf("CancelSync failed: %v", err)
	}

	final := &SyncProfileStatus{ProfileID: "profile-a", Status: "idle", LastSync: timePtr(time.Now())}
	applySnapshotToStatus(final, newRunSnapshot("profile-a", run, "canceled"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = service.GetProfileStatus("profile-a")
			}
		}()
	}
	if !service.publishFinalStatus("profile-a", run.generation, final) {
		t.Fatal("canceled final status was rejected")
	}
	wg.Wait()
}

type blockingSyncStateRepository struct {
	enteredOnce sync.Once
	entered     chan struct{}
	release     chan struct{}
	state       *database.ProfileSyncState
}

func (r *blockingSyncStateRepository) GetSyncState(string) (*database.ProfileSyncState, error) {
	r.enteredOnce.Do(func() { close(r.entered) })
	<-r.release
	return r.state, nil
}

func (r *blockingSyncStateRepository) UpdateSyncState(state *database.ProfileSyncState) error {
	r.state = state
	return nil
}
