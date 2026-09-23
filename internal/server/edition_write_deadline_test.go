package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/auth"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition/editiontest"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const deadlineTestWriteTimeout = 300 * time.Millisecond

// slowEditionServers holds the Audiobookshelf and Hardcover fakes for the
// write-deadline tests. Tests slow Hardcover down with hardcover.SetDelays.
type slowEditionServers struct {
	abs       *editiontest.AudiobookshelfFake
	hardcover *editiontest.HardcoverFake
}

func newSlowEditionServers(t *testing.T) *slowEditionServers {
	t.Helper()
	hardcover := editiontest.NewHardcoverFake(t)
	hardcover.Authors["An Author"] = 101
	return &slowEditionServers{
		abs: editiontest.NewAudiobookshelfFake(t, map[string]map[string]interface{}{
			"item-1": {
				"id":        "item-1",
				"libraryId": "library",
				"mediaType": "book",
				"media": map[string]interface{}{
					"metadata": map[string]interface{}{"title": "A Title", "authorName": "An Author", "isbn": "9780306406157"},
					"duration": 3600.0,
				},
			},
		}),
		hardcover: hardcover,
	}
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
	status, payload, elapsed, err := doEditionRequestResult(method, url, body, cookie)
	require.NoError(t, err, "the response must reach the client even though the request outlived the server's write timeout")
	return status, payload, elapsed
}

func doEditionRequestResult(method, url, body string, cookie *http.Cookie) (int, []byte, time.Duration, error) {
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, nil, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, time.Since(started), err
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, time.Since(started), err
	}
	return response.StatusCode, payload, time.Since(started), nil
}

func TestCreateEditionResponseSurvivesTheServerWriteTimeout(t *testing.T) {
	servers := newSlowEditionServers(t)
	servers.hardcover.SetDelays(0, 3*deadlineTestWriteTimeout)
	baseURL, cookie := startEditionHTTPServer(t, servers)

	status, payload, elapsed := doEditionRequest(t, http.MethodPost,
		baseURL+"/api/profiles/edition-profile/runs/run-1/books/item-1/edition",
		`{"title":"A Title","isbn_13":"9780306406157","release_date":"2021-02-03"}`,
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

func TestDeleteProfileResponseSurvivesEditionDrainPastServerWriteTimeout(t *testing.T) {
	servers := newSlowEditionServers(t)
	hold := make(chan struct{})
	servers.hardcover.HoldInsert(hold)
	baseURL, cookie := startEditionHTTPServer(t, servers)

	createDone := make(chan error, 1)
	go func() {
		status, payload, _, err := doEditionRequestResult(http.MethodPost,
			baseURL+"/api/profiles/edition-profile/runs/run-1/books/item-1/edition",
			`{"title":"A Title","isbn_13":"9780306406157","release_date":"2021-02-03"}`,
			cookie)
		if err != nil {
			createDone <- err
			return
		}
		if status != http.StatusOK {
			createDone <- fmt.Errorf("create returned %d: %s", status, payload)
			return
		}
		createDone <- nil
	}()
	select {
	case <-servers.hardcover.Entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the create never reached Hardcover")
	}

	type deleteResult struct {
		status  int
		payload []byte
		elapsed time.Duration
		err     error
	}
	deleteDone := make(chan deleteResult, 1)
	go func() {
		status, payload, elapsed, err := doEditionRequestResult(http.MethodDelete,
			baseURL+"/api/profiles/edition-profile", "", cookie)
		deleteDone <- deleteResult{status: status, payload: payload, elapsed: elapsed, err: err}
	}()
	time.Sleep(3 * deadlineTestWriteTimeout)
	close(hold)

	require.NoError(t, <-createDone)
	deleted := <-deleteDone
	require.NoError(t, deleted.err)
	require.Greater(t, deleted.elapsed, deadlineTestWriteTimeout, "deletion must outlive the server write timeout for this test to mean anything")
	require.Equal(t, http.StatusOK, deleted.status, string(deleted.payload))
}
