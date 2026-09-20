package multiuser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	stdSync "sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

// editionHardcoverFake stands in for the Hardcover GraphQL endpoint. It answers
// person searches from a name table, records insert_edition variables, and can
// hold that mutation open so tests can overlap two submits.
type editionHardcoverFake struct {
	*httptest.Server

	mu      stdSync.Mutex
	authors map[string]int
	asins   map[string]existingEdition // editions that already carry an ASIN
	isbns   map[string]existingEdition // editions that already carry an ISBN-13
	// insertErrors, when set, is returned as insert_edition's errors list, the way
	// Hardcover reports a duplicate.
	insertErrors []string
	mutations    []map[string]interface{}
	writes       []string // every GraphQL mutation document received, of any kind
	failWith     string   // GraphQL error message returned for insert_edition

	entered chan struct{} // receives once per insert_edition that has started
	release chan struct{} // when non-nil, insert_edition waits for it to close

	holdReads   chan struct{} // when non-nil, every read (non-mutation) waits for it to close
	readEntered chan struct{} // receives once per read that started while holdReads was set
}

// existingEdition is an edition already on Hardcover, found by its ASIN.
type existingEdition struct {
	editionID int
	bookID    int
}

func newEditionHardcoverFake(t *testing.T) *editionHardcoverFake {
	t.Helper()
	fake := &editionHardcoverFake{
		authors: map[string]int{},
		asins:   map[string]existingEdition{},
		isbns:   map[string]existingEdition{},
		entered: make(chan struct{}, 8),

		readEntered: make(chan struct{}, 64),
	}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		respond := func(data map[string]interface{}) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
		}

		if strings.HasPrefix(strings.TrimSpace(request.Query), "mutation") {
			fake.mu.Lock()
			fake.writes = append(fake.writes, request.Query)
			fake.mu.Unlock()
		} else {
			fake.mu.Lock()
			holdReads := fake.holdReads
			fake.mu.Unlock()
			if holdReads != nil {
				fake.readEntered <- struct{}{}
				select {
				case <-holdReads:
				case <-r.Context().Done():
					return
				}
			}
		}

		switch {
		case strings.Contains(request.Query, "insert_edition"):
			fake.mu.Lock()
			fake.mutations = append(fake.mutations, request.Variables)
			release, failWith, insertErrors := fake.release, fake.failWith, fake.insertErrors
			fake.mu.Unlock()
			fake.entered <- struct{}{}
			if release != nil {
				<-release
			}
			if failWith != "" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []map[string]string{{"message": failWith}}})
				return
			}
			if len(insertErrors) > 0 {
				respond(map[string]interface{}{"insert_edition": map[string]interface{}{"id": nil, "errors": insertErrors}})
				return
			}
			respond(map[string]interface{}{"insert_edition": map[string]interface{}{"id": 777, "errors": []string{}}})
		case strings.Contains(request.Query, "insert_image"):
			respond(map[string]interface{}{"insert_image": map[string]interface{}{"id": 55}})
		case strings.Contains(request.Query, "update_edition"):
			respond(map[string]interface{}{"update_edition": map[string]interface{}{"id": 777, "errors": []string{}}})
		case strings.Contains(request.Query, "BookByISBN"):
			isbn, _ := request.Variables["isbn"].(string)
			books := []interface{}{}
			fake.mu.Lock()
			found, ok := fake.isbns[isbn]
			fake.mu.Unlock()
			if ok {
				books = append(books, map[string]interface{}{
					"id": found.bookID, "title": "Existing", "editions": []interface{}{map[string]interface{}{"id": found.editionID, "isbn_13": isbn}},
				})
			}
			respond(map[string]interface{}{"books": books})
		case strings.Contains(request.Query, "BookByASIN"):
			asin, _ := request.Variables["asin"].(string)
			books := []interface{}{}
			fake.mu.Lock()
			found, ok := fake.asins[asin]
			fake.mu.Unlock()
			if ok {
				books = append(books, map[string]interface{}{
					"id": found.bookID, "title": "Existing", "editions": []interface{}{map[string]interface{}{"id": found.editionID, "asin": asin}},
				})
			}
			respond(map[string]interface{}{"books": books})
		case strings.Contains(request.Query, "query GetEdition("):
			editionID, _ := request.Variables["editionId"].(float64)
			editions := []interface{}{}
			fake.mu.Lock()
			for asin, found := range fake.asins {
				if float64(found.editionID) == editionID {
					editions = append(editions, map[string]interface{}{"id": found.editionID, "book_id": found.bookID, "asin": asin})
				}
			}
			fake.mu.Unlock()
			respond(map[string]interface{}{"editions": editions})
		case strings.Contains(request.Query, "SearchPeopleDirect") || strings.Contains(request.Query, "SearchNarrators"):
			name, _ := request.Variables["name"].(string)
			people := []map[string]interface{}{}
			fake.mu.Lock()
			if id, ok := fake.authors[name]; ok {
				people = append(people, map[string]interface{}{"id": id, "name": name, "books_count": 3})
			}
			fake.mu.Unlock()
			respond(map[string]interface{}{"authors": people})
		default:
			// Any other read (candidate search, publisher lookup) finds nothing.
			respond(map[string]interface{}{
				"search":     map[string]interface{}{"error": "", "results": map[string]interface{}{"hits": []interface{}{}}},
				"publishers": []interface{}{},
				"editions":   []interface{}{},
				"books":      []interface{}{},
			})
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (f *editionHardcoverFake) recordedMutations() []map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]interface{}(nil), f.mutations...)
}

func (f *editionHardcoverFake) recordedWrites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

// editionAudiobookshelfFake serves /api/items/{id} for a fixed set of items.
type editionAudiobookshelfFake struct {
	*httptest.Server
	status int // non-zero forces every item request to this status
}

func newEditionAudiobookshelfFake(t *testing.T, items map[string]map[string]interface{}) *editionAudiobookshelfFake {
	t.Helper()
	fake := &editionAudiobookshelfFake{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fake.status != 0 {
			http.Error(w, "forced", fake.status)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/items/")
		item, ok := items[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(item)
	}))
	t.Cleanup(fake.Close)
	return fake
}

// editionItem builds an Audiobookshelf item without a cover or ASIN, so no
// image upload or Audnex lookup is attempted.
func editionItem(id, title, author string) map[string]interface{} {
	return map[string]interface{}{
		"id":        id,
		"libraryId": "library",
		"mediaType": "book",
		"media": map[string]interface{}{
			"metadata": map[string]interface{}{"title": title, "authorName": author},
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
	hardcover *editionHardcoverFake
	abs       *editionAudiobookshelfFake
}

// newEditionFixture builds a service with one profile whose run "run-1"
// contains the given outcome records.
func newEditionFixture(t *testing.T, dryRun bool, records []syncsvc.BookOutcomeRecord, items map[string]map[string]interface{}) *editionFixture {
	t.Helper()
	service, _ := newStatusLookupService(t)
	hardcover := newEditionHardcoverFake(t)
	abs := newEditionAudiobookshelfFake(t, items)
	service.globalConfig.Hardcover.BaseURL = hardcover.URL
	service.globalConfig.RateLimit.Rate = time.Nanosecond
	service.globalConfig.RateLimit.MaxConcurrent = 4
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
	return &editionFixture{service: service, hardcover: hardcover, abs: abs}
}

func needsReview(bookID, hardcoverBookID string) syncsvc.BookOutcomeRecord {
	return syncsvc.BookOutcomeRecord{BookID: bookID, Outcome: syncsvc.OutcomeNeedsReview, HardcoverBookID: hardcoverBookID}
}

func validEdits() EditionEdits {
	return EditionEdits{
		Title:              "A Title",
		ReleaseDate:        "2021-02-03",
		EditionInformation: "Unabridged",
		AudioSeconds:       3600,
		LanguageID:         1,
		CountryID:          1,
		AuthorIDs:          []int{101},
		NarratorIDs:        []int{202},
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

	mutations := f.hardcover.recordedMutations()
	require.Len(t, mutations, 1)
	require.EqualValues(t, 4242, mutations[0]["bookId"], "the edition must attach to the run record's Hardcover book")
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "A Title", dto["title"])
	require.Equal(t, "2021-02-03", dto["release_date"])
	require.Len(t, dto["contributions"], 2)
	require.NotContains(t, dto, "image_id", "an item without a cover must not attach an image")
}

func TestCreateEditionFromRunBook_DryRunIssuesNoMutation(t *testing.T) {
	f := newEditionFixture(t, true,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)

	created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
	require.NoError(t, err)
	require.Equal(t, &EditionCreated{EditionID: 0, DryRun: true, Warnings: []string{}}, created)
	require.Empty(t, f.hardcover.recordedMutations())
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
			require.Empty(t, f.hardcover.recordedMutations())
		})
	}
}

func TestCreateEditionFromRunBook_RejectsInvalidEdits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EditionEdits)
	}{
		{"no author", func(e *EditionEdits) { e.AuthorIDs = nil }},
		{"no title", func(e *EditionEdits) { e.Title = "" }},
		{"malformed release date", func(e *EditionEdits) { e.ReleaseDate = "03/02/2021" }},
		{"non-positive author ID", func(e *EditionEdits) { e.AuthorIDs = []int{0} }},
		{"non-positive narrator ID", func(e *EditionEdits) { e.NarratorIDs = []int{-1} }},
		{"negative audio length", func(e *EditionEdits) { e.AudioSeconds = -1 }},
		{"edition format over the length limit", func(e *EditionEdits) { e.EditionFormat = strings.Repeat("f", maxEditionFormatLength+1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEditionFixture(t, false,
				[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
				map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
			)
			edits := validEdits()
			tt.mutate(&edits)

			_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)
			var validation *EditionValidationError
			require.ErrorAs(t, err, &validation)
			require.Empty(t, f.hardcover.recordedMutations())
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

	f.abs.status = http.StatusInternalServerError
	var upstream *EditionUpstreamError
	_, err = f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
	require.ErrorAs(t, err, &upstream)
	require.Equal(t, "audiobookshelf", upstream.Service)
	require.Empty(t, f.hardcover.recordedMutations())
}

func TestCreateEditionFromRunBook_HardcoverFailureIsUpstream(t *testing.T) {
	f := newEditionFixture(t, false,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
	)
	f.hardcover.failWith = "hardcover rejected the edition"

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
	f.hardcover.mu.Lock()
	f.hardcover.release = release
	f.hardcover.mu.Unlock()

	firstDone := make(chan error, 1)
	go func() {
		_, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())
		firstDone <- err
	}()
	select {
	case <-f.hardcover.entered:
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
	case <-f.hardcover.entered:
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
	require.Empty(t, f.hardcover.recordedMutations())
}

func TestPrepareEditionDraft_ResolvesFromTheRunRecord(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		f := newEditionFixture(t, dryRun,
			[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
			map[string]map[string]interface{}{"item-1": editionItem("item-1", "Draft Service Title", "Draft Service Author")},
		)
		f.hardcover.authors["Draft Service Author"] = 55

		built, err := f.service.PrepareEditionDraft(context.Background(), "profile-1", "run-1", "item-1")
		require.NoError(t, err)
		require.Equal(t, 4242, built.HardcoverBookID)
		require.Equal(t, "Draft Service Title", built.Title)
		require.Equal(t, []int{55}, built.AuthorIDs)
		require.Equal(t, dryRun, built.DryRun)
		require.Equal(t, 3600, built.AudioSeconds)
		require.Empty(t, built.CoverURL, "an item without a cover has no cover URL")
		require.Empty(t, f.hardcover.recordedMutations(), "previewing must never mutate Hardcover")
	}
}
