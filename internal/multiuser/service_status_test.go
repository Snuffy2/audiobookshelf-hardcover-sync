package multiuser

import (
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/mismatch"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

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
	if got == status || got.Snapshot == status.Snapshot {
		t.Fatal("expected status and snapshot to be copied")
	}
	if got.Snapshot.RunID != "run-a" || got.BooksTotal != 20 {
		t.Fatalf("unexpected snapshot status: %#v", got)
	}

	got.Snapshot.BookOutcomes[0].Title = "changed"
	got.Mismatches[0].AuthorIDs[0] = 99
	got.LastSyncSummary.TotalBooksProcessed = 99
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

func TestGetProfileStatusKeepsProfilesIsolated(t *testing.T) {
	lastSync := time.Now().UTC()
	service := &MultiUserService{
		profileStatuses: map[string]*SyncProfileStatus{
			"profile-a": {
				ProfileID: "profile-a",
				Status:    "completed",
				LastSync:  &lastSync,
				Snapshot:  &syncsvc.SyncSnapshot{UserID: "profile-a", RunID: "run-a", ProcessedSoFar: 1},
			},
			"profile-b": {
				ProfileID: "profile-b",
				Status:    "completed",
				LastSync:  &lastSync,
				Snapshot:  &syncsvc.SyncSnapshot{UserID: "profile-b", RunID: "run-b", ProcessedSoFar: 1},
			},
		},
		syncServices: make(map[string]*syncsvc.Service),
	}

	a := service.GetProfileStatus("profile-a")
	b := service.GetProfileStatus("profile-b")
	if a.Snapshot.UserID != "profile-a" || b.Snapshot.UserID != "profile-b" {
		t.Fatalf("profiles shared snapshot metadata: a=%#v b=%#v", a.Snapshot, b.Snapshot)
	}
	if a.Snapshot.RunID == b.Snapshot.RunID {
		t.Fatalf("profiles shared run IDs: a=%q b=%q", a.Snapshot.RunID, b.Snapshot.RunID)
	}
}
