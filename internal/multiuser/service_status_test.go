package multiuser

import (
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/mismatch"
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

func TestGetProfileStatusCopiesCurrentSnapshotAndLegacyDetails(t *testing.T) {
	lastSync := time.Date(2026, time.September, 12, 22, 0, 0, 0, time.UTC)
	status := &SyncProfileStatus{
		ProfileID:   "profile-a",
		ProfileName: "A",
		Status:      "syncing",
		LastSync:    &lastSync,
		Snapshot: &syncsvc.SyncSnapshot{
			UserID:         "profile-a",
			RunID:          "run-a",
			RunStartedAt:   lastSync,
			State:          "syncing",
			BooksTotal:     20,
			ProcessedSoFar: 2,
			OutcomeCounts: syncsvc.OutcomeCounts{
				Synced:      1,
				NeedsReview: 1,
			},
			BookOutcomes: []syncsvc.BookOutcomeRecord{{
				BookID:  "book-a",
				Title:   "Book A",
				Outcome: syncsvc.OutcomeNeedsReview,
			}},
			AttentionRecords: []syncsvc.BookOutcomeRecord{{
				BookID:  "book-a",
				Title:   "Book A",
				Outcome: syncsvc.OutcomeNeedsReview,
			}},
			Mismatches: []mismatch.BookMismatch{{
				BookID:    "book-a",
				AuthorIDs: []int{7},
			}},
		},
		Mismatches: []mismatch.BookMismatch{{
			BookID:    "book-a",
			AuthorIDs: []int{7},
		}},
	}
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{"profile-a": status},
		syncServices:    make(map[string]*syncsvc.Service),
	}

	got := service.GetProfileStatus("profile-a")
	if got.Snapshot.RunID != "run-a" || got.BooksTotal != 20 {
		t.Fatalf("unexpected snapshot status: %#v", got)
	}

	got.Snapshot.BookOutcomes[0].Title = "changed"
	got.Mismatches[0].AuthorIDs[0] = 99
	got.LastSync = nil

	again := service.GetProfileStatus("profile-a")
	if again.Snapshot.BookOutcomes[0].Title != "Book A" {
		t.Fatalf("snapshot outcome was aliased: %#v", again.Snapshot.BookOutcomes)
	}
	if again.Mismatches[0].AuthorIDs[0] != 7 {
		t.Fatalf("mismatch metadata was aliased: %#v", again.Mismatches)
	}
	if again.LastSync == nil || again.LastSync.Equal(lastSync) == false {
		t.Fatalf("last sync was aliased or lost: %#v", again.LastSync)
	}
}
