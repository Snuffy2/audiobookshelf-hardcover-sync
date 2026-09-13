package multiuser

import (
	"context"
	"testing"
	"time"

	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

func TestRunGenerationKeepsReplacementAfterCanceledRunUnwinds(t *testing.T) {
	oldService := &syncsvc.Service{}
	newService := &syncsvc.Service{}
	oldRun := activeSyncRun{generation: 1, runID: "run-old", startedAt: time.Now().UTC()}
	newRun := activeSyncRun{generation: 2, runID: "run-new", startedAt: time.Now().UTC()}

	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{},
		activeSyncs:     make(map[string]context.CancelFunc),
		activeRuns:      map[string]activeSyncRun{"profile-a": oldRun},
		syncServices:    map[string]*syncsvc.Service{"profile-a": oldService},
		serviceRuns:     map[string]uint64{"profile-a": oldRun.generation},
	}
	_, oldCancel := context.WithCancel(context.Background())
	service.activeSyncs["profile-a"] = oldCancel
	oldDone := make(chan struct{})
	oldFinalStatus := &SyncProfileStatus{
		ProfileID: "profile-a",
		Status:    "completed",
		LastSync:  timePtr(oldRun.startedAt.Add(time.Second)),
		Snapshot: func() *syncsvc.SyncSnapshot {
			snapshot := newRunSnapshot("profile-a", oldRun, "completed")
			return &snapshot
		}(),
	}

	// The old run is canceled and a replacement is registered before the old
	// goroutine finishes its cleanup and final publication.
	oldCancel()
	service.syncMutex.Lock()
	delete(service.activeSyncs, "profile-a")
	delete(service.activeRuns, "profile-a")
	service.syncMutex.Unlock()
	service.syncMutex.Lock()
	service.activeRuns["profile-a"] = newRun
	service.syncMutex.Unlock()
	service.servicesMutex.Lock()
	service.syncServices["profile-a"] = newService
	service.serviceRuns["profile-a"] = newRun.generation
	service.servicesMutex.Unlock()
	newStatus := &SyncProfileStatus{
		ProfileID: "profile-a",
		Status:    "syncing",
		LastSync:  timePtr(newRun.startedAt),
	}
	newSnapshot := newRunSnapshot("profile-a", newRun, "syncing")
	applySnapshotToStatus(newStatus, newSnapshot)
	service.updateProfileStatus("profile-a", newStatus)

	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		<-oldDone
		service.removeSyncService("profile-a", oldRun.generation, oldService)
		if service.publishFinalStatus("profile-a", oldRun.generation, oldFinalStatus) {
			t.Errorf("stale run published final status")
		}
		service.finishActiveRun("profile-a", oldRun.generation)
	}()
	// Signal the blocked old run to finish its guarded cleanup.
	close(oldDone)

	// Wait for the stale cleanup goroutine to make its guarded calls.
	<-cleanupDone

	current, ok := service.GetSyncService("profile-a")
	if !ok || current != newService {
		t.Fatalf("replacement service was removed: service=%p ok=%v", current, ok)
	}
	status := service.GetProfileStatus("profile-a")
	if status.Snapshot == nil || status.Snapshot.RunID != newRun.runID {
		t.Fatalf("replacement status was overwritten: %#v", status)
	}

	service.syncMutex.RLock()
	active, ok := service.activeRuns["profile-a"]
	service.syncMutex.RUnlock()
	if !ok || active.generation != newRun.generation {
		t.Fatalf("replacement run was removed: %#v", active)
	}
}

func TestInitialStatusExposesCoherentRunMetadata(t *testing.T) {
	run := activeSyncRun{generation: 1, runID: "run-a", startedAt: time.Now().UTC()}
	status := &SyncProfileStatus{ProfileID: "profile-a", Status: "syncing"}
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{"profile-a": status},
		activeRuns:      map[string]activeSyncRun{"profile-a": run},
		syncServices:    map[string]*syncsvc.Service{"profile-a": {}},
		serviceRuns:     map[string]uint64{"profile-a": run.generation},
	}

	got := service.GetProfileStatus("profile-a")
	if got.Status != "syncing" || got.Snapshot == nil {
		t.Fatalf("initial status did not expose a snapshot: %#v", got)
	}
	if got.Snapshot.RunID != run.runID || !got.Snapshot.RunStartedAt.Equal(run.startedAt) || got.Snapshot.State != "syncing" {
		t.Fatalf("initial metadata was incoherent: %#v", got.Snapshot)
	}
}
