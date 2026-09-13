package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/types"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/config"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/crypto"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/multiuser"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
	"github.com/stretchr/testify/require"
)

func TestPublicStatusAndSummaryRoutesShareCurrentRunSnapshot(t *testing.T) {
	logger.ForceSetup(logger.Config{Level: "error", Format: logger.FormatJSON, Output: io.Discard})

	dataDir := t.TempDir()
	db, err := database.NewDatabase(&database.DatabaseConfig{
		Type: database.DatabaseTypeSQLite,
		Path: filepath.Join(dataDir, "status-test.db"),
	}, logger.Get())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	encryptor, err := crypto.NewEncryptionManagerWithDataDir(dataDir, logger.Get())
	require.NoError(t, err)
	repo := database.NewRepository(db, encryptor, logger.Get())

	absServer := newStatusAudiobookshelfServer()
	t.Cleanup(absServer.Close)
	hardcoverServer := newEmptyHardcoverServer(t)
	t.Cleanup(hardcoverServer.Close)

	cfg := config.DefaultConfig()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.CacheDir = filepath.Join(dataDir, "cache")
	cfg.Paths.MismatchOutputDir = filepath.Join(dataDir, "mismatches")
	cfg.RateLimit.Rate = time.Nanosecond
	cfg.RateLimit.MaxConcurrent = 1
	cfg.Hardcover.BaseURL = hardcoverServer.URL
	multiUser := multiuser.NewMultiUserService(repo, cfg, logger.Get())

	for _, profile := range []struct {
		id     string
		name   string
		title  string
		author string
	}{
		{id: "profile-a", name: "A", title: "Missing A", author: "Author A"},
		{id: "profile-b", name: "B", title: "Missing B", author: "Author B"},
	} {
		require.NoError(t, repo.CreateProfile(
			profile.id,
			profile.name,
			absServer.URL,
			profile.id,
			"hardcover-token",
			database.SyncConfigData{
				StateFile:          filepath.Join(dataDir, "sync-state.json"),
				ProcessUnreadBooks: true,
				DryRun:             true,
			},
		))
		absServer.books[profile.id] = statusBook(profile.id, profile.title, profile.author)
	}

	for _, profileID := range []string{"profile-a", "profile-b"} {
		require.NoError(t, multiUser.StartSync(profileID))
		status := waitForStatusRun(t, multiUser, profileID)
		require.Equal(t, "completed", status.Status)
		require.NotNil(t, status.Snapshot)
		require.Equal(t, int32(1), status.Snapshot.ProcessedSoFar)
		require.Equal(t, int32(1), status.Snapshot.OutcomeCounts.NotFound)
		require.Len(t, status.Snapshot.AttentionRecords, 1)
	}

	handler := NewHandler(multiUser, nil, logger.Get())
	var statusResponse struct {
		Success bool                        `json:"success"`
		Data    multiuser.SyncProfileStatus `json:"data"`
	}
	callJSONHandler(t, handler.GetProfileStatus, "/api/profiles/profile-a/status", &statusResponse)
	require.True(t, statusResponse.Success)
	require.NotNil(t, statusResponse.Data.Snapshot)

	var summaryResponse struct {
		Success bool                 `json:"success"`
		Data    statusSummaryPayload `json:"data"`
	}
	callJSONHandler(t, handler.GetSyncSummary, "/api/profiles/profile-a/summary", &summaryResponse)
	require.True(t, summaryResponse.Success)
	require.NotNil(t, summaryResponse.Data.Snapshot)

	statusSnapshot := statusResponse.Data.Snapshot
	summarySnapshot := summaryResponse.Data.Snapshot
	require.Equal(t, statusSnapshot.RunID, summaryResponse.Data.RunID)
	require.Equal(t, statusSnapshot.RunID, summarySnapshot.RunID)
	require.Equal(t, statusSnapshot.RunStartedAt, summarySnapshot.RunStartedAt)
	require.Equal(t, statusSnapshot.State, summarySnapshot.State)
	require.Equal(t, statusSnapshot.BooksTotal, summaryResponse.Data.BooksTotal)
	require.Equal(t, statusSnapshot.ProcessedSoFar, summaryResponse.Data.ProcessedSoFar)
	require.Equal(t, statusSnapshot.OutcomeCounts, summaryResponse.Data.OutcomeCounts)
	require.Equal(t, statusSnapshot.AttentionRecords, summaryResponse.Data.AttentionRecords)
	require.Equal(t, statusSnapshot.AttentionRecords[0].BookID, "profile-a")

	// Existing consumers can continue using the flattened fields while moving
	// to the run-scoped snapshot contract.
	require.Equal(t, int32(1), summaryResponse.Data.TotalBooksProcessed)
	require.Equal(t, int32(0), summaryResponse.Data.BooksSynced)
	require.Len(t, summaryResponse.Data.BooksNotFound, 1)
	require.Equal(t, "Missing A", summaryResponse.Data.BooksNotFound[0].Title)
	require.Empty(t, summaryResponse.Data.Mismatches)
	require.Equal(t, int32(1), statusResponse.Data.Snapshot.TotalBooksProcessed)
	require.Len(t, statusResponse.Data.BooksNotFound, 1)
	require.Equal(t, "Missing A", statusResponse.Data.BooksNotFound[0].Title)
	require.Empty(t, statusResponse.Data.Mismatches)

	var allStatusesResponse struct {
		Success bool                          `json:"success"`
		Data    []multiuser.SyncProfileStatus `json:"data"`
	}
	callJSONHandler(t, handler.GetAllProfileStatuses, "/api/status", &allStatusesResponse)
	require.True(t, allStatusesResponse.Success)
	require.Len(t, allStatusesResponse.Data, 2)
	byID := make(map[string]multiuser.SyncProfileStatus, len(allStatusesResponse.Data))
	for _, status := range allStatusesResponse.Data {
		byID[status.ProfileID] = status
	}
	require.Contains(t, byID, "profile-a")
	require.Contains(t, byID, "profile-b")
	require.NotNil(t, byID["profile-a"].Snapshot)
	require.NotNil(t, byID["profile-b"].Snapshot)
	require.NotEqual(t, byID["profile-a"].Snapshot.RunID, byID["profile-b"].Snapshot.RunID)
	require.Equal(t, "profile-a", byID["profile-a"].Snapshot.AttentionRecords[0].BookID)
	require.Equal(t, "profile-b", byID["profile-b"].Snapshot.AttentionRecords[0].BookID)
	require.Equal(t, "Missing A", byID["profile-a"].Snapshot.AttentionRecords[0].Title)
	require.Equal(t, "Missing B", byID["profile-b"].Snapshot.AttentionRecords[0].Title)
}

type statusSummaryPayload struct {
	Snapshot            *syncsvc.SyncSnapshot       `json:"snapshot"`
	RunID               string                      `json:"run_id"`
	RunStartedAt        time.Time                   `json:"run_started_at"`
	State               string                      `json:"state"`
	BooksTotal          int32                       `json:"books_total"`
	ProcessedSoFar      int32                       `json:"processed_so_far"`
	OutcomeCounts       syncsvc.OutcomeCounts       `json:"outcome_counts"`
	AttentionRecords    []syncsvc.BookOutcomeRecord `json:"attention_records"`
	TotalBooksProcessed int32                       `json:"total_books_processed"`
	BooksSynced         int32                       `json:"books_synced"`
	BooksNotFound       []types.BookNotFoundInfo    `json:"books_not_found"`
	Mismatches          []map[string]interface{}    `json:"mismatches"`
}

type statusAudiobookshelfServer struct {
	*httptest.Server
	books map[string]map[string]interface{}
}

func newStatusAudiobookshelfServer() *statusAudiobookshelfServer {
	fixture := &statusAudiobookshelfServer{books: make(map[string]map[string]interface{})}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profileID := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.URL.Path == "/api/me":
			_, _ = io.WriteString(w, `{"mediaProgress":[],"listeningSessions":[]}`)
		case r.URL.Path == "/api/libraries":
			_, _ = io.WriteString(w, `{"libraries":[{"id":"library","name":"Library"}]}`)
		case r.URL.Path == "/api/libraries/library/items":
			book, ok := fixture.books[profileID]
			if !ok {
				http.Error(w, "unknown profile", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": []map[string]interface{}{book}})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	return fixture
}

func newEmptyHardcoverServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"search": map[string]interface{}{
					"error":   "",
					"results": map[string]interface{}{"hits": []interface{}{}},
				},
			},
		})
	}))
}

func statusBook(id, title, author string) map[string]interface{} {
	return map[string]interface{}{
		"id":        id,
		"libraryId": "library",
		"mediaType": "book",
		"media": map[string]interface{}{
			"metadata": map[string]interface{}{
				"title":      title,
				"authorName": author,
			},
			"duration": 100,
		},
	}
}

func waitForStatusRun(t *testing.T, service *multiuser.MultiUserService, profileID string) *multiuser.SyncProfileStatus {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status := service.GetProfileStatus(profileID)
		if status != nil && status.Status != "syncing" && status.Snapshot != nil && status.Snapshot.RunID != "" {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for profile %q sync status", profileID)
	return nil
}

func callJSONHandler(t *testing.T, handler http.HandlerFunc, path string, target interface{}) {
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), target))
}
