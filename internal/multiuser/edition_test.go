package multiuser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition/editiontest"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

// editionItem builds an Audiobookshelf item with a hyphenated ISBN but no cover
// or ASIN, so no image upload or Audnex lookup is attempted.
func editionItem(id, title, author string) map[string]interface{} {
	return map[string]interface{}{
		"id":        id,
		"libraryId": "library",
		"mediaType": "book",
		"media": map[string]interface{}{
			"metadata": map[string]interface{}{"title": title, "authorName": author, "narratorName": "A Narrator", "isbn": "978-0-306-40615-7"},
			"duration": 3600.0,
		},
	}
}

// editionItemWithCover is editionItem with a cover, so creating its edition
// downloads the cover from Audiobookshelf and uploads it to Hardcover.
func editionItemWithCover(id, title, author string) map[string]interface{} {
	item := editionItem(id, title, author)
	item["media"].(map[string]interface{})["coverPath"] = "/covers/" + id + ".jpg"
	return item
}

type editionFixture struct {
	service   *MultiUserService
	hardcover *editiontest.HardcoverRequestCounter
	abs       *editiontest.AudiobookshelfFake
	db        *gorm.DB
}

type editionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f editionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func stubAudnexTransport(t *testing.T, roundTrip func(*http.Request) (*http.Response, error)) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = editionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.audnex.us" {
			return roundTrip(request)
		}
		return previous.RoundTrip(request)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

// newEditionFixture builds a service with one profile whose run "run-1"
// contains the given outcome records.
func newEditionFixture(t *testing.T, dryRun bool, records []syncsvc.BookOutcomeRecord, items map[string]map[string]interface{}) *editionFixture {
	t.Helper()
	service, db := newStatusLookupService(t)
	hardcover := editiontest.NewHardcoverRequestCounter(t)
	abs := editiontest.NewAudiobookshelfFake(t, items)
	service.globalConfig.Hardcover.BaseURL = hardcover.URL
	service.globalConfig.RateLimit.Rate = time.Nanosecond
	service.globalConfig.RateLimit.MaxConcurrent = 4
	hardcover.Authors["An Author"] = 101
	hardcover.Authors["A Narrator"] = 202
	t.Cleanup(func() { require.NoError(t, service.Shutdown(context.Background())) })

	require.NoError(t, service.repository.CreateProfile(
		"profile-1", "Profile", abs.URL, "abs-token", "hc-token", database.SyncConfigData{DryRun: dryRun},
	))
	service.updateProfileStatus("profile-1", &SyncProfileStatus{
		ProfileID: "profile-1",
		Snapshot: &syncsvc.SyncSnapshot{
			ProfileID:    "profile-1",
			RunID:        "run-1",
			State:        string(syncsvc.RunPhaseCompleted),
			BookOutcomes: records,
		},
	})
	return &editionFixture{service: service, hardcover: hardcover, abs: abs, db: db}
}

func needsReview(bookID, hardcoverBookID string) syncsvc.BookOutcomeRecord {
	return syncsvc.BookOutcomeRecord{BookID: bookID, Outcome: syncsvc.OutcomeNeedsReview, HardcoverBookID: hardcoverBookID}
}

func validEdits() EditionEdits {
	return EditionEdits{
		Title:              "A Title",
		ISBN13:             "9780306406157",
		ReleaseDate:        "2021-02-03",
		EditionInformation: "Unabridged",
	}
}

func TestCreateEditionFromRunBook_TargetsTheRecordedHardcoverBook(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)

	created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.NoError(t, err)
	require.Equal(t, &EditionCreated{EditionID: 777, DryRun: false, Warnings: []string{}}, created)

	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	require.EqualValues(t, 4242, mutations[0]["bookId"], "the edition must attach to the run record's Hardcover book")
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "A Title", dto["title"])
	require.Equal(t, "2021-02-03", dto["release_date"])
	require.Len(t, dto["contributions"], 2)
	require.NotContains(t, dto, "image_id", "an item without a cover must not attach an image")
}

func TestCreateEditionFromRunBookDeduplicatesTrimmedContributorNames(t *testing.T) {
	item := editionItem("item-1", "A Title", "An Author")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	metadata["authors"] = []interface{}{
		map[string]interface{}{"name": "An Author"},
		map[string]interface{}{"name": " An Author "},
	}
	metadata["narrators"] = []interface{}{"A Narrator", " A Narrator "}
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)

	created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.NoError(t, err)
	require.Equal(t, 777, created.EditionID)
	require.Equal(t, 5, f.hardcover.RequestCount(), "one author search, one narrator search, two duplicate lookups and one insert are expected")
	contributions := f.hardcover.RecordedMutations()[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})["contributions"].([]interface{})
	require.Len(t, contributions, 2, "duplicate names should still produce one contribution each")
}

func TestCreateEditionFromRunBookAcceptsContributorBudgetBoundaryWithFiveDuplicateLookups(t *testing.T) {
	item := editionItem("item-1", "A Title", "An Author")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	metadata["publisher"] = "A Publisher"
	narratorNames := make([]string, 12)
	narrators := make([]interface{}, len(narratorNames))
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)
	for i := range narratorNames {
		narratorNames[i] = fmt.Sprintf("Narrator %02d", i)
		narrators[i] = narratorNames[i]
		f.hardcover.Authors[narratorNames[i]] = 200 + i
	}
	metadata["narrators"] = narrators
	f.hardcover.Publishers["A Publisher"] = 303
	// At the default limiter this is exactly 25 lookup units: one author query
	// plus 12 narrators, conservatively budgeted for direct and fallback search.
	f.service.globalConfig.RateLimit.Rate = 0
	rateLimit := f.service.hardcoverRateLimit()
	budget := editionContributorLookupBudget(EditionCreateTimeout, rateLimit)
	require.Equal(t, hardcover.DefaultRateLimit, rateLimit)
	require.Equal(t, 25, budget)
	require.Equal(t, budget, editionContributorLookupUnits([]string{"An Author"}, narratorNames))

	// Route the test client's requests through a narrow recording proxy so the
	// test can verify all five proactive identifier probes without expanding
	// the shared edition fake's API.
	type graphQLRequest struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}
	var requestMu sync.Mutex
	var requests []graphQLRequest
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var observed graphQLRequest
		if err := json.Unmarshal(body, &observed); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestMu.Lock()
		requests = append(requests, observed)
		requestMu.Unlock()
		upstreamRequest, err := http.NewRequestWithContext(r.Context(), r.Method, f.hardcover.URL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		upstreamRequest.Header = r.Header.Clone()
		upstreamResponse, err := http.DefaultClient.Do(upstreamRequest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer upstreamResponse.Body.Close()
		for key, values := range upstreamResponse.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(upstreamResponse.StatusCode)
		_, _ = io.Copy(w, upstreamResponse.Body)
	}))
	defer proxy.Close()
	f.service.globalConfig.Hardcover.BaseURL = proxy.URL

	edits := validEdits()
	edits.ASIN = "B0EXISTING1"
	edits.ISBN10 = "0131103628" // Unrelated ISBNs yield five distinct lookup keys.
	created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)
	require.NoError(t, err)
	require.Equal(t, 777, created.EditionID)
	require.Empty(t, created.Warnings)
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.EqualValues(t, 303, dto["publisher_id"])
	require.Len(t, dto["contributions"], 13)

	requestMu.Lock()
	defer requestMu.Unlock()
	var duplicateProbes []graphQLRequest
	for _, request := range requests {
		if strings.Contains(request.Query, "query BookByASIN") || strings.Contains(request.Query, "query BookByISBN") {
			duplicateProbes = append(duplicateProbes, request)
		}
	}
	require.Len(t, duplicateProbes, 5)
	require.Equal(t, "B0EXISTING1", duplicateProbes[0].Variables["asin"])
	require.Equal(t, "9780306406157", duplicateProbes[1].Variables["isbn"])
	require.Equal(t, "0131103628", duplicateProbes[2].Variables["isbn"])
	require.Equal(t, "9780131103627", duplicateProbes[3].Variables["isbn"])
	require.Equal(t, "0306406152", duplicateProbes[4].Variables["isbn"])
}

func TestCreateEditionFromRunBookRejectsContributorLookupsOverCombinedBudget(t *testing.T) {
	item := editionItem("item-1", "A Title", "An Author")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	narratorNames := make([]string, 13)
	narrators := make([]interface{}, len(narratorNames))
	for i := range narratorNames {
		narratorNames[i] = fmt.Sprintf("Narrator %02d", i)
		narrators[i] = narratorNames[i]
	}
	metadata["narrators"] = narrators
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)
	// With the default two-second limiter, the two-minute create budget leaves
	// room for 25 contributor lookup requests after reserving the worst-case
	// creator work: five proactive identifier probes, publisher, insert, and
	// five duplicate-race probes. One author plus 12 narrators costs exactly
	// 25 requests; the next narrator pushes the input over budget.
	f.service.globalConfig.RateLimit.Rate = 0
	rateLimit := f.service.hardcoverRateLimit()
	budget := editionContributorLookupBudget(EditionCreateTimeout, rateLimit)
	require.Equal(t, hardcover.DefaultRateLimit, rateLimit)
	require.Equal(t, 25, budget)
	require.Equal(t, budget, editionContributorLookupUnits([]string{"An Author"}, narratorNames[:12]))
	require.Greater(t, editionContributorLookupUnits([]string{"An Author"}, narratorNames), budget)
	edits := validEdits()
	edits.ASIN = "B0EXISTING1"
	edits.ISBN10 = "0131103628" // unrelated to the supplied ISBN-13: five distinct creator probes.

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)
	var validation *EditionValidationError
	require.ErrorAs(t, err, &validation)
	require.Contains(t, validation.Error(), "within the edition create time budget")
	require.Zero(t, f.hardcover.RequestCount(), "over-budget metadata must be rejected before contributor lookups")
}

func TestCreateEditionFromRunBook_DryRunIssuesNoMutation(t *testing.T) {
	f := newEditionFixture(t, true,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)

	created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.NoError(t, err)
	require.Equal(t, &EditionCreated{EditionID: 0, DryRun: true, Warnings: []string{}}, created)
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestDeleteProfileInvalidatesEditionCapabilityCache(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	capability, err := f.service.EditionCapabilityForProfile(context.Background(), "profile-1")
	require.NoError(t, err)
	require.True(t, capability.CanCreate)
	require.Equal(t, 1, f.hardcover.ProbeCount())

	require.NoError(t, f.service.DeleteProfile("profile-1"))
	f.service.editionCapabilityMutex.Lock()
	defer f.service.editionCapabilityMutex.Unlock()
	for key := range f.service.editionCapabilities {
		require.NotEqual(t, "profile-1", key.profileID)
	}
}

func TestEditionCapabilityCanceledLeaderDoesNotPoisonConcurrentCaller(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	hold := make(chan struct{})
	f.hardcover.HoldProbes(hold)
	t.Cleanup(func() { f.hardcover.HoldProbes(nil) })

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := f.service.EditionCapabilityForProfile(leaderCtx, "profile-1")
		leaderDone <- err
	}()
	select {
	case <-f.hardcover.ProbeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the capability probe did not reach Hardcover")
	}

	followerDone := make(chan struct {
		capability EditionCapability
		err        error
	}, 1)
	go func() {
		capability, err := f.service.EditionCapabilityForProfile(context.Background(), "profile-1")
		followerDone <- struct {
			capability EditionCapability
			err        error
		}{capability: capability, err: err}
	}()
	cancelLeader()
	close(hold)

	require.ErrorIs(t, <-leaderDone, context.Canceled)
	result := <-followerDone
	require.NoError(t, result.err)
	require.Equal(t, EditionCapability{CanCreate: true}, result.capability)
	require.Equal(t, 1, f.hardcover.ProbeCount(), "the healthy follower must share the completed probe")
}

func TestEditionRequestsRequireAnEligibleRecord(t *testing.T) {
	records := []syncsvc.BookOutcomeRecord{
		needsReview("ok-item", "4242"),
		{BookID: "synced-item", Outcome: syncsvc.OutcomeSynced, HardcoverBookID: "4242"},
		needsReview("no-candidate", ""),
		needsReview("text-id", "not-a-number"),
		needsReview("zero-id", "0"),
		needsReview("negative-id", "-5"),
	}
	items := map[string]map[string]interface{}{}
	for _, r := range records {
		items[r.BookID] = editionItem(r.BookID, "A Title", "An Author")
	}

	tests := []struct {
		name      string
		profileID string
		runID     string
		bookID    string
		want      error
	}{
		{"not a needs-review outcome", "profile-1", "run-1", "synced-item", ErrEditionNotEligible},
		{"no Hardcover book recorded", "profile-1", "run-1", "no-candidate", ErrEditionNotEligible},
		{"non-numeric Hardcover book", "profile-1", "run-1", "text-id", ErrEditionNotEligible},
		{"zero Hardcover book", "profile-1", "run-1", "zero-id", ErrEditionNotEligible},
		{"negative Hardcover book", "profile-1", "run-1", "negative-id", ErrEditionNotEligible},
		{"book absent from the run", "profile-1", "run-1", "missing-item", ErrEditionNotFound},
		{"unknown run", "profile-1", "other-run", "ok-item", ErrEditionNotFound},
		{"unknown profile", "profile-x", "run-1", "ok-item", ErrProfileNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEditionFixture(t, false, records, items)

			_, draftErr := f.service.PrepareEditionDraft(context.Background(), tt.profileID, tt.runID, tt.bookID)
			require.ErrorIs(t, draftErr, tt.want)

			_, createErr := f.service.CreateEditionFromRunBook(context.Background(), tt.profileID, tt.runID, tt.bookID, validEdits())
			require.ErrorIs(t, createErr, tt.want)
			require.Empty(t, f.hardcover.RecordedMutations())
			require.Zero(t, f.hardcover.RequestCount())
		})
	}
}

func TestCreateEditionFromRunBook_RejectsInvalidEdits(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*EditionEdits)
		noAuthors bool
	}{
		{"no author match", nil, true},
		{"no ASIN or ISBN", func(e *EditionEdits) { e.ISBN13 = "" }, false},
		{"blank ASIN and ISBNs", func(e *EditionEdits) { e.ASIN, e.ISBN10, e.ISBN13 = "  ", " ", "\t" }, false},
		{"malformed ISBN-13", func(e *EditionEdits) { e.ISBN13 = "978030640615" }, false},
		{"ISBN-10 sent as the ISBN-13", func(e *EditionEdits) { e.ISBN13 = "0306406152" }, false},
		{"malformed ISBN-10", func(e *EditionEdits) { e.ISBN10 = "03064061" }, false},
		{"ISBN-13 sent as the ISBN-10", func(e *EditionEdits) { e.ISBN10 = "9780306406157" }, false},
		{"no title", func(e *EditionEdits) { e.Title = "" }, false},
		{"blank title", func(e *EditionEdits) { e.Title = " \t\n " }, false},
		{"malformed release date", func(e *EditionEdits) { e.ReleaseDate = "03/02/2021" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEditionFixture(t, false,
				[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
				map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
			)
			edits := validEdits()
			if tt.mutate != nil {
				tt.mutate(&edits)
			}
			if tt.noAuthors {
				f.hardcover.Authors = map[string]int{}
			}

			_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)
			var validation *EditionValidationError
			require.ErrorAs(t, err, &validation)
			require.Empty(t, f.hardcover.RecordedMutations())
		})
	}
}

func TestEditionRequests_AudiobookshelfFailures(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{}, // Audiobookshelf no longer has the item
	)
	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.ErrorIs(t, err, ErrEditionItemNotFound)
	_, err = f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
	require.ErrorIs(t, err, ErrEditionItemNotFound)

	f.abs.Status = http.StatusInternalServerError
	var upstream *EditionUpstreamError
	_, err = f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
	require.ErrorAs(t, err, &upstream)
	require.Equal(t, "audiobookshelf", upstream.Service)
	require.Zero(t, f.hardcover.RequestCount())
}

func TestCreateEditionFromRunBook_HardcoverFailureIsUpstream(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	f.hardcover.FailWith = "hardcover rejected the edition"

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	var upstream *EditionUpstreamError
	require.ErrorAs(t, err, &upstream)
	require.Equal(t, "hardcover", upstream.Service)
}

func TestCreateEditionFromRunBook_RejectsOverlappingSubmitForTheSameBook(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242"), needsReview("item-2", "4343")},
		map[string]map[string]interface{}{
			"item-1": editionItem("item-1", "A Title", "An Author"),
			"item-2": editionItem("item-2", "Another", "An Author"),
		},
	)
	release := make(chan struct{})
	f.hardcover.HoldInsert(release)

	firstDone := make(chan error, 1)
	go func() {
		_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
		firstDone <- err
	}()
	select {
	case <-f.hardcover.Entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first submit never reached Hardcover")
	}

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.ErrorIs(t, err, ErrEditionInProgress)

	// A different book is independent of the held one.
	secondDone := make(chan error, 1)
	go func() {
		_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-2", validEdits())
		secondDone <- err
	}()
	select {
	case <-f.hardcover.Entered:
	case <-time.After(10 * time.Second):
		t.Fatal("a different book was blocked by the in-flight submit")
	}

	close(release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	// Once finished, the same book can be submitted again.
	_, err = f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.NoError(t, err)
}

func TestCreateEditionFromRunBookRejectsOverlappingItemsForSameHardcoverTarget(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242"), needsReview("item-2", "4242")},
		map[string]map[string]interface{}{
			"item-1": editionItem("item-1", "A Title", "An Author"),
			"item-2": editionItem("item-2", "Another ABS Item", "An Author"),
		},
	)
	release := holdCreatesOpen(t, f)
	firstDone := startHeldCreate(t, f)

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-2", validEdits())
	require.ErrorIs(t, err, ErrEditionInProgress)
	require.Len(t, f.hardcover.RecordedMutations(), 1, "the second ABS item must not insert against the same Hardcover target concurrently")

	release()
	require.NoError(t, <-firstDone)
}

func TestEditionRequestsAreRejectedAfterShutdown(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	require.NoError(t, f.service.Shutdown(context.Background()))

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.ErrorIs(t, err, ErrServiceShuttingDown)
	_, err = f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
	require.ErrorIs(t, err, ErrServiceShuttingDown)
	require.Zero(t, f.hardcover.RequestCount())
}

func TestPrepareEditionDraftUsesABSNamesWithoutHardcoverRequests(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		f := newEditionFixture(t, dryRun,
			[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
			map[string]map[string]interface{}{"item-1": editionItem("item-1", "Draft Service Title", "Draft Service Author")},
		)
		built, err := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
		require.NoError(t, err)
		require.Equal(t, 4242, built.HardcoverBookID)
		require.Equal(t, "Draft Service Title", built.Title)
		require.Equal(t, "Draft Service Author", built.AuthorNames)
		require.Equal(t, dryRun, built.DryRun)
		require.Equal(t, 3600, built.AudioSeconds)
		require.Zero(t, f.hardcover.RequestCount(), "previewing must not send any Hardcover request")
	}
}

func TestEditionRequestsRequireAnIdentifierOnTheAudiobookshelfItem(t *testing.T) {
	item := func(mutate func(meta map[string]interface{})) map[string]map[string]interface{} {
		it := editionItem("item-1", "A Title", "An Author")
		mutate(it["media"].(map[string]interface{})["metadata"].(map[string]interface{}))
		return map[string]map[string]interface{}{"item-1": it}
	}
	tests := []struct {
		name    string
		items   map[string]map[string]interface{}
		allowed bool
	}{
		{name: "neither ASIN nor ISBN", items: item(func(m map[string]interface{}) { delete(m, "isbn") })},
		{name: "blank ASIN and ISBN", items: item(func(m map[string]interface{}) { m["isbn"], m["asin"] = " ", "  " })},
		{name: "an ISBN that is not an ISBN", items: item(func(m map[string]interface{}) { m["isbn"] = "not-an-isbn" })},
		{name: "an ISBN only", items: item(func(m map[string]interface{}) {}), allowed: true},
		{name: "an ASIN only", items: item(func(m map[string]interface{}) { delete(m, "isbn"); m["asin"] = "B0EXISTING1" }), allowed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "an ASIN only" {
				stubAudnexTransport(t, func(request *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"releaseDate":"2024-04-05"}`)),
						Request:    request,
					}, nil
				})
			}
			f := newEditionFixture(t, false, []syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")}, tt.items)

			built, draftErr := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
			require.Zero(t, f.hardcover.RequestCount(), "previewing must not send any Hardcover request")
			_, createErr := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())

			if tt.allowed {
				require.NoError(t, draftErr)
				require.NoError(t, createErr)
				if tt.name == "an ASIN only" {
					require.Equal(t, "B0EXISTING1", built.ASIN)
				}
				return
			}
			require.ErrorIs(t, draftErr, ErrEditionNoIdentifier)
			require.ErrorIs(t, createErr, ErrEditionNoIdentifier)
			require.Zero(t, f.hardcover.RequestCount(), "no Hardcover request may be made for a book without an identifier")
		})
	}
}

func TestCreateEditionFromRunBook_NormalizesSubmittedISBNs(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	edits := validEdits()
	edits.ISBN13 = "978-0-306-40615-7"
	edits.ISBN10 = " 0-306-40615-2 "

	_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)
	require.NoError(t, err)

	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "9780306406157", dto["isbn_13"])
	require.Equal(t, "0306406152", dto["isbn_10"])
}
