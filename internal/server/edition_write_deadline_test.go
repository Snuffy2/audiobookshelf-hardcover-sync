package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/auth"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const deadlineTestWriteTimeout = 300 * time.Millisecond

// slowEditionServers holds the Audiobookshelf and Hardcover fakes for the
// write-deadline tests. Hardcover delays every response by delayAll and the
// insert_edition mutation by an additional delayInsert.
type slowEditionServers struct {
	abs, hardcover *httptest.Server
	delayAll       time.Duration
	delayInsert    time.Duration
}

func newSlowEditionServers(t *testing.T) *slowEditionServers {
	t.Helper()
	servers := &slowEditionServers{}
	servers.abs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/items/item-1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":        "item-1",
			"libraryId": "library",
			"mediaType": "book",
			"media": map[string]interface{}{
				"metadata": map[string]interface{}{"title": "A Title", "authorName": "An Author", "isbn": "9780306406157"},
				"duration": 3600.0,
			},
		})
	}))
	t.Cleanup(servers.abs.Close)
	servers.hardcover = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		time.Sleep(servers.delayAll)
		if strings.Contains(request.Query, "insert_edition") {
			time.Sleep(servers.delayInsert)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"insert_edition": map[string]interface{}{"id": 777, "errors": []string{}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{
			"search":     map[string]interface{}{"error": "", "results": map[string]interface{}{"hits": []interface{}{}}},
			"authors":    []interface{}{},
			"publishers": []interface{}{},
			"editions":   []interface{}{},
			"books":      []interface{}{},
		}})
	}))
	t.Cleanup(servers.hardcover.Close)
	return servers
}

// startEditionHTTPServer serves the real handler chain, with authentication,
// from an http.Server whose write timeout is far below the request duration.
// It returns the base URL and the session cookie of a profile owner whose
// profile has a needs-review book ready for an edition.
func startEditionHTTPServer(t *testing.T, servers *slowEditionServers) (baseURL string, cookie *http.Cookie) {
	t.Helper()
	fixture := newRouteTestFixtureWithHardcoverURL(t, servers.hardcover.URL)
	owner := newRouteSession(t, fixture, "edition-owner", auth.RoleUser)

	created := fixture.requestWithCookies(http.MethodPost, "/api/profiles", []byte(`{
		"id": "edition-profile",
		"name": "edition-profile",
		"audiobookshelf_url": "`+servers.abs.URL+`",
		"audiobookshelf_token": "abs-token",
		"hardcover_token": "hc-token",
		"sync_config": {"process_unread_books": true, "dry_run": false}
	}`), []*http.Cookie{owner.cookie})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())

	now := time.Now().UTC()
	raw, err := json.Marshal(syncsvc.SyncSnapshot{
		ProfileID: "edition-profile", RunID: "run-1", State: string(syncsvc.RunPhaseCompleted),
		BookOutcomes: []syncsvc.BookOutcomeRecord{{BookID: "item-1", Outcome: syncsvc.OutcomeNeedsReview, HardcoverBookID: "4242"}},
	})
	require.NoError(t, err)
	report, err := fixture.repo.AcceptSyncRun(&database.SyncRunReport{
		ProfileID: "edition-profile", RunID: "run-1", Phase: database.SyncRunPhaseQueued, QueuedAt: &now, SnapshotJSON: "{}",
	})
	require.NoError(t, err)
	report.Phase = database.SyncRunPhaseCompleted
	report.FinishedAt = &now
	report.SnapshotJSON = database.SyncSnapshotJSON(raw)
	require.NoError(t, fixture.repo.UpsertSyncRunReportContext(context.Background(), report))

	httpServer := httptest.NewUnstartedServer(fixture.server.server.Handler)
	httpServer.Config.WriteTimeout = deadlineTestWriteTimeout
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	return httpServer.URL, owner.cookie
}

func doEditionRequest(t *testing.T, method, url, body string, cookie *http.Cookie) (int, []byte, time.Duration) {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err, "the response must reach the client even though the request outlived the server's write timeout")
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, payload, time.Since(started)
}

func TestCreateEditionResponseSurvivesTheServerWriteTimeout(t *testing.T) {
	servers := newSlowEditionServers(t)
	servers.delayInsert = 3 * deadlineTestWriteTimeout
	baseURL, cookie := startEditionHTTPServer(t, servers)

	status, payload, elapsed := doEditionRequest(t, http.MethodPost,
		baseURL+"/api/profiles/edition-profile/runs/run-1/books/item-1/edition",
		`{"title":"A Title","isbn_13":"9780306406157","release_date":"2021-02-03","audio_seconds":3600,"language_id":1,"country_id":1,"author_ids":[101]}`,
		cookie)

	require.Greater(t, elapsed, deadlineTestWriteTimeout, "the request must outlive the server write timeout for this test to mean anything")
	require.Equal(t, http.StatusOK, status, string(payload))
	var envelope struct {
		Data struct {
			EditionID int `json:"edition_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &envelope))
	require.Equal(t, 777, envelope.Data.EditionID)
}

func TestEditionDraftResponseSurvivesTheServerWriteTimeout(t *testing.T) {
	servers := newSlowEditionServers(t)
	servers.delayAll = deadlineTestWriteTimeout / 2
	baseURL, cookie := startEditionHTTPServer(t, servers)

	status, payload, elapsed := doEditionRequest(t, http.MethodGet,
		baseURL+"/api/profiles/edition-profile/runs/run-1/books/item-1/edition-draft", "", cookie)

	require.Greater(t, elapsed, deadlineTestWriteTimeout, "the request must outlive the server write timeout for this test to mean anything")
	require.Equal(t, http.StatusOK, status, string(payload))
}
