package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition/editiontest"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const (
	editionProfileID = "edition-profile"
	editionRunID     = "seeded-run"
	editionBasePath  = "/api/profiles/" + editionProfileID + "/runs/" + editionRunID + "/books/"
)

func editionAPIItem(id, title, author, coverPath string) map[string]interface{} {
	return map[string]interface{}{
		"id":        id,
		"libraryId": "library",
		"mediaType": "book",
		"media": map[string]interface{}{
			"coverPath": coverPath,
			"metadata":  map[string]interface{}{"title": title, "authorName": author, "isbn": "978-0-306-40615-7"},
			"duration":  3600.0,
		},
	}
}

type editionAPIFixture struct {
	*statusServiceFixture
	routes    http.Handler
	hardcover *editiontest.HardcoverFake
	abs       *editiontest.AudiobookshelfFake
}

func newEditionAPIFixture(t *testing.T, dryRun bool, records []syncsvc.BookOutcomeRecord, items map[string]map[string]interface{}) *editionAPIFixture {
	t.Helper()
	hardcover := editiontest.NewHardcoverFake(t)
	abs := editiontest.NewAudiobookshelfFake(t, items)
	fixture := newStatusServiceFixture(t, hardcover.URL)
	// Runs before the service shuts down, so a held sync can unwind.
	t.Cleanup(hardcover.ReleaseHolds)

	require.NoError(t, fixture.repo.CreateProfile(
		editionProfileID, "Edition profile", abs.URL, "abs-secret-token", "hc-secret-token",
		database.SyncConfigData{
			StateFile:          filepath.Join(fixture.dataDir, "sync-state.json"),
			ProcessUnreadBooks: true,
			DryRun:             dryRun,
		},
	))
	seedEditionRun(t, fixture.repo, records)

	handler := NewHandler(fixture.multiUser, logger.Get())
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/profiles/{id}/sync", handler.StartSync)
	apiMux.HandleFunc("DELETE /api/profiles/{id}/sync", handler.CancelSync)
	apiMux.HandleFunc("GET /api/profiles/{id}/runs/{runID}/books/{bookID}/edition-draft", handler.GetEditionDraft)
	apiMux.HandleFunc("POST /api/profiles/{id}/runs/{runID}/books/{bookID}/edition", handler.CreateEdition)
	root := http.NewServeMux()
	root.Handle("/api/", apiMux)

	return &editionAPIFixture{statusServiceFixture: fixture, routes: root, hardcover: hardcover, abs: abs}
}

// seedEditionRun stores a completed run report holding the given records.
func seedEditionRun(t *testing.T, repo *database.Repository, records []syncsvc.BookOutcomeRecord) {
	t.Helper()
	now := time.Now().UTC()
	raw, err := json.Marshal(syncsvc.SyncSnapshot{
		ProfileID: editionProfileID, RunID: editionRunID, State: string(syncsvc.RunPhaseCompleted), BookOutcomes: records,
	})
	require.NoError(t, err)
	report, err := repo.AcceptSyncRun(&database.SyncRunReport{
		ProfileID: editionProfileID, RunID: editionRunID, Phase: database.SyncRunPhaseQueued, QueuedAt: &now, SnapshotJSON: "{}",
	})
	require.NoError(t, err)
	report.Phase = database.SyncRunPhaseCompleted
	report.FinishedAt = &now
	report.SnapshotJSON = database.SyncSnapshotJSON(raw)
	require.NoError(t, repo.UpsertSyncRunReportContext(context.Background(), report))
}

func (f *editionAPIFixture) do(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	f.routes.ServeHTTP(recorder, request)
	return recorder
}

type editionEnvelope struct {
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error"`
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) editionEnvelope {
	t.Helper()
	var envelope editionEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	return envelope
}

func reviewRecord(bookID, hardcoverBookID string) syncsvc.BookOutcomeRecord {
	return syncsvc.BookOutcomeRecord{BookID: bookID, Outcome: syncsvc.OutcomeNeedsReview, HardcoverBookID: hardcoverBookID}
}

const validEditionBody = `{"title":"A Title","isbn_13":"9780306406157","release_date":"2021-02-03","edition_information":"Unabridged",` +
	`"audio_seconds":3600,"language_id":1,"country_id":1,"author_ids":[101],"narrator_ids":[202]}`

func singleItemFixture(t *testing.T, dryRun bool, item map[string]interface{}) *editionAPIFixture {
	t.Helper()
	return newEditionAPIFixture(t, dryRun,
		[]syncsvc.BookOutcomeRecord{reviewRecord("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": item},
	)
}

func TestGetEditionDraftReturnsTheDraftContract(t *testing.T) {
	item := editionAPIItem("item-1", "Contract Title", "Contract Author", "/covers/item-1.jpg")
	f := singleItemFixture(t, false, item)
	f.hardcover.Authors["Contract Author"] = 55

	recorder := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	envelope := decodeEnvelope(t, recorder)
	require.True(t, envelope.Success)

	keys := make([]string, 0, len(envelope.Data))
	for key := range envelope.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	require.Equal(t, []string{
		"asin", "audio_seconds", "author_ids", "author_names", "country_id", "cover_url", "dry_run",
		"edition_format", "edition_information", "hardcover_book_id", "isbn_10", "isbn_13", "language_id",
		"narrator_ids", "narrator_names", "publisher_id", "publisher_name", "reading_format", "release_date", "subtitle",
		"title", "warnings",
	}, keys)
	require.EqualValues(t, 4242, envelope.Data["hardcover_book_id"])
	require.Equal(t, "Contract Title", envelope.Data["title"])
	require.Equal(t, []interface{}{float64(55)}, envelope.Data["author_ids"])
	require.Equal(t, []interface{}{}, envelope.Data["narrator_ids"])
	require.Equal(t, "Contract Author", envelope.Data["author_names"])
	require.EqualValues(t, 3600, envelope.Data["audio_seconds"])
	require.Equal(t, f.abs.URL+"/api/items/item-1/cover", envelope.Data["cover_url"])
	require.Equal(t, false, envelope.Data["dry_run"])
	require.IsType(t, []interface{}{}, envelope.Data["warnings"])
	require.NotContains(t, recorder.Body.String(), "abs-secret-token")
	require.Empty(t, f.hardcover.RecordedMutations(), "previewing must not create anything")
}

func TestEditionEndpointsRejectIneligibleTargets(t *testing.T) {
	records := []syncsvc.BookOutcomeRecord{
		reviewRecord("ok-item", "4242"),
		{BookID: "synced-item", Outcome: syncsvc.OutcomeSynced, HardcoverBookID: "4242"},
		reviewRecord("no-candidate", ""),
		reviewRecord("text-id", "abc"),
	}
	items := map[string]map[string]interface{}{}
	for _, record := range records {
		items[record.BookID] = editionAPIItem(record.BookID, "Title", "Author", "")
	}

	tests := []struct {
		name string
		path string // path after /api/profiles/
		want int
	}{
		{"not needs-review", editionProfileID + "/runs/" + editionRunID + "/books/synced-item", http.StatusConflict},
		{"no Hardcover book", editionProfileID + "/runs/" + editionRunID + "/books/no-candidate", http.StatusConflict},
		{"non-numeric Hardcover book", editionProfileID + "/runs/" + editionRunID + "/books/text-id", http.StatusConflict},
		{"book not in run", editionProfileID + "/runs/" + editionRunID + "/books/absent", http.StatusNotFound},
		{"run not found", editionProfileID + "/runs/other-run/books/ok-item", http.StatusNotFound},
		{"profile not found", "missing-profile/runs/" + editionRunID + "/books/ok-item", http.StatusNotFound},
	}
	f := newEditionAPIFixture(t, false, records, items)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := f.do(http.MethodGet, "/api/profiles/"+tt.path+"/edition-draft", "")
			require.Equal(t, tt.want, draft.Code, draft.Body.String())
			require.False(t, decodeEnvelope(t, draft).Success)

			create := f.do(http.MethodPost, "/api/profiles/"+tt.path+"/edition", validEditionBody)
			require.Equal(t, tt.want, create.Code, create.Body.String())
			require.False(t, decodeEnvelope(t, create).Success)
		})
	}
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestEditionEndpointsRequireAllPathIdentifiers(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	handler := NewHandler(f.multiUser, logger.Get())

	for name, call := range map[string]http.HandlerFunc{"draft": handler.GetEditionDraft, "create": handler.CreateEdition} {
		recorder := httptest.NewRecorder()
		call(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(validEditionBody)))
		require.Equal(t, http.StatusBadRequest, recorder.Code, name)
	}
}

func TestCreateEditionTargetsTheRunRecordAndReturnsTheEdition(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	envelope := decodeEnvelope(t, recorder)
	require.True(t, envelope.Success)
	require.Equal(t, map[string]interface{}{"edition_id": float64(777), "dry_run": false, "warnings": []interface{}{}}, envelope.Data)

	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	require.EqualValues(t, 4242, mutations[0]["bookId"])
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "A Title", dto["title"])
	require.Equal(t, "2021-02-03", dto["release_date"])
}

func TestCreateEditionWithAnExistingASIN(t *testing.T) {
	const body = `{"title":"A Title","asin":"B0EXISTING1","language_id":1,"country_id":1,"author_ids":[101]}`

	t.Run("an edition of the same book is returned untouched", func(t *testing.T) {
		f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", "/cover.jpg"))
		f.hardcover.ASINs["B0EXISTING1"] = editiontest.ExistingEdition{EditionID: 555, BookID: 4242}

		recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", body)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, map[string]interface{}{"edition_id": float64(555), "dry_run": false, "warnings": []interface{}{}}, decodeEnvelope(t, recorder).Data)
		require.Empty(t, f.hardcover.RecordedMutations())
	})

	t.Run("an edition of another book is a conflict that names no other book", func(t *testing.T) {
		f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
		f.hardcover.ASINs["B0EXISTING1"] = editiontest.ExistingEdition{EditionID: 555, BookID: 9999}

		recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", body)
		require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
		require.Equal(t, "An edition with this ASIN or ISBN already exists on Hardcover and could not be confirmed to belong to this book.", decodeEnvelope(t, recorder).Error)
		require.NotContains(t, recorder.Body.String(), "9999")
		require.Empty(t, f.hardcover.RecordedMutations())
	})
}

func TestCreateEditionWithAnISBNOnAnotherBooksEditionIsAConflict(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", "/cover.jpg"))
	f.hardcover.ISBNs["9781234567897"] = editiontest.ExistingEdition{EditionID: 555, BookID: 9999}

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
		`{"title":"A Title","isbn_13":"9781234567897","language_id":1,"country_id":1,"author_ids":[101]}`)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Equal(t, "An edition with this ASIN or ISBN already exists on Hardcover and could not be confirmed to belong to this book.", decodeEnvelope(t, recorder).Error)
	require.NotContains(t, recorder.Body.String(), "9999")
	require.Empty(t, f.hardcover.RecordedMutations(), "the match is found before any insert is attempted")
}

func TestEditionEndpointsRejectABookWithoutAnASINOrISBN(t *testing.T) {
	item := editionAPIItem("item-1", "Title", "Author", "/cover.jpg")
	delete(item["media"].(map[string]interface{})["metadata"].(map[string]interface{}), "isbn")
	f := singleItemFixture(t, false, item)
	const want = "This book has no ASIN or ISBN in Audiobookshelf, so an edition created for it could not be matched by a sync. Add an ASIN or ISBN in Audiobookshelf first."

	draft := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusConflict, draft.Code, draft.Body.String())
	require.Equal(t, want, decodeEnvelope(t, draft).Error)

	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusConflict, create.Code, create.Body.String())
	require.Equal(t, want, decodeEnvelope(t, create).Error)
	require.Empty(t, f.hardcover.RecordedMutations())
}

func ebookAPIItem() map[string]interface{} {
	item := editionAPIItem("item-1", "Ebook Title", "Ebook Author", "/cover.jpg")
	media := item["media"].(map[string]interface{})
	delete(media, "duration")
	media["ebookFormat"] = "epub"
	return item
}

func TestEbookEditionEndpointsCreateAnEbookEdition(t *testing.T) {
	f := singleItemFixture(t, false, ebookAPIItem())
	f.hardcover.Authors["Ebook Author"] = 55

	draft := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusOK, draft.Code, draft.Body.String())
	data := decodeEnvelope(t, draft).Data
	require.Equal(t, "ebook", data["reading_format"])
	require.Equal(t, "Ebook", data["edition_format"])
	require.EqualValues(t, 0, data["audio_seconds"])
	require.Equal(t, []interface{}{}, data["narrator_ids"])

	// The request still carries audiobook-only fields; the server sends none of them.
	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.EqualValues(t, 4, dto["reading_format_id"])
	require.Equal(t, "Ebook", dto["edition_format"])
	require.NotContains(t, dto, "audio_seconds")
	for _, c := range dto["contributions"].([]interface{}) {
		require.NotEqual(t, "Narrator", c.(map[string]interface{})["contribution"])
	}
}

func TestEditionRequestCannotChooseTheReadingFormat(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
		strings.TrimSuffix(validEditionBody, "}")+`,"reading_format":"ebook"}`)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionRequiresAnIdentifierInTheRequest(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"A Title","author_ids":[101]}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Equal(t, "an ASIN or ISBN is required", decodeEnvelope(t, recorder).Error)

	recorder = f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"A Title","isbn_13":"12345","author_ids":[101]}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "isbn_13")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionAcceptsAHyphenatedISBNAndSendsItNormalized(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
		`{"title":"A Title","isbn_13":"978-0-306-40615-7","language_id":1,"country_id":1,"author_ids":[101]}`)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "9780306406157", dto["isbn_13"])
}

func TestCreateEditionByISBN10MatchesAnExistingEdition(t *testing.T) {
	const body = `{"title":"A Title","isbn_10":"0-306-40615-2","language_id":1,"country_id":1,"author_ids":[101]}`

	t.Run("an edition of the same book is returned untouched", func(t *testing.T) {
		f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", "/cover.jpg"))
		f.hardcover.ISBNs["0306406152"] = editiontest.ExistingEdition{EditionID: 555, BookID: 4242}

		recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", body)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, map[string]interface{}{"edition_id": float64(555), "dry_run": false, "warnings": []interface{}{}}, decodeEnvelope(t, recorder).Data)
		require.Empty(t, f.hardcover.RecordedMutations())
	})

	t.Run("an edition of another book is a conflict", func(t *testing.T) {
		f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", "/cover.jpg"))
		f.hardcover.ISBNs["0306406152"] = editiontest.ExistingEdition{EditionID: 555, BookID: 9999}

		recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", body)
		require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
		require.NotContains(t, recorder.Body.String(), "9999")
		require.Empty(t, f.hardcover.RecordedMutations())
	})
}

func TestCreateEditionSendsTheRequestedEditionFormat(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantFormat string
	}{
		{"provided format", strings.TrimSuffix(validEditionBody, "}") + `,"edition_format":"Audible Audio"}`, "Audible Audio"},
		{"format at the length limit", strings.TrimSuffix(validEditionBody, "}") + `,"edition_format":"` + strings.Repeat("f", 100) + `"}`, strings.Repeat("f", 100)},
		{"omitted format", validEditionBody, "Audiobook"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

			recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", tt.body)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

			mutations := f.hardcover.RecordedMutations()
			require.Len(t, mutations, 1)
			dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
			require.Equal(t, tt.wantFormat, dto["edition_format"])
			require.EqualValues(t, 2, dto["reading_format_id"], "the reading format stays Audiobook")
		})
	}
}

func TestCreateEditionRejectsAnOverlongEditionFormat(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	body := strings.TrimSuffix(validEditionBody, "}") + `,"edition_format":"` + strings.Repeat("f", 101) + `"}`

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", body)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "edition format")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionRejectsClientControlledTargetsAndBadBodies(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	withField := func(field string) string {
		return strings.TrimSuffix(validEditionBody, "}") + "," + field + "}"
	}

	tests := []struct {
		name string
		body string
	}{
		{"client-supplied book id", withField(`"book_id":999`)},
		{"client-supplied image url", withField(`"image_url":"https://evil.example/cover.jpg"`)},
		{"unknown field", withField(`"extra":true`)},
		{"malformed JSON", `{"title":`},
		{"empty body", ``},
		{"not an object", `[]`},
		{"trailing JSON value", validEditionBody + `{}`},
		{"body over the size limit", `{"title":"` + strings.Repeat("a", 70<<10) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", tt.body)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionRejectsInvalidEditsWithUserReadableError(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"A Title","author_ids":[]}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "author")
	require.Empty(t, f.hardcover.RecordedMutations())

	recorder = f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"   ","author_ids":[101]}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "title is required")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestEditionDryRunProfilesIssueNoHardcoverMutation(t *testing.T) {
	f := singleItemFixture(t, true, editionAPIItem("item-1", "Title", "Author", ""))

	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	require.Equal(t, map[string]interface{}{"edition_id": float64(0), "dry_run": true, "warnings": []interface{}{}}, decodeEnvelope(t, create).Data)

	draft := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusOK, draft.Code, draft.Body.String())
	require.Equal(t, true, decodeEnvelope(t, draft).Data["dry_run"])

	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestEditionEndpointsReportMissingAndFailingAudiobookshelfItems(t *testing.T) {
	f := newEditionAPIFixture(t, false,
		[]syncsvc.BookOutcomeRecord{reviewRecord("gone-item", "4242")},
		map[string]map[string]interface{}{},
	)
	for _, recorder := range []*httptest.ResponseRecorder{
		f.do(http.MethodGet, editionBasePath+"gone-item/edition-draft", ""),
		f.do(http.MethodPost, editionBasePath+"gone-item/edition", validEditionBody),
	} {
		require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	f.abs.Status = http.StatusInternalServerError
	recorder := f.do(http.MethodGet, editionBasePath+"gone-item/edition-draft", "")
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "abs-secret-token")
	require.NotContains(t, recorder.Body.String(), "forced failure")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionRejectsOverlappingSubmitForTheSameBook(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	release := make(chan struct{})
	f.hardcover.HoldInsert(release)

	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody) }()
	select {
	case <-f.hardcover.Entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first submit never reached Hardcover")
	}

	second := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())

	f.hardcover.HoldInsert(nil)
	close(release)
	firstResponse := <-first
	require.Equal(t, http.StatusOK, firstResponse.Code, firstResponse.Body.String())
	require.Len(t, f.hardcover.RecordedMutations(), 1, "the rejected submit must not reach Hardcover")
}

func TestCreateEditionWorksDuringAFullSyncWithoutChangingSyncControl(t *testing.T) {
	syncBook := editionAPIItem("sync-book", "Sync Book", "Sync Author", "")
	f := newEditionAPIFixture(t, false,
		[]syncsvc.BookOutcomeRecord{reviewRecord("item-1", "4242")},
		map[string]map[string]interface{}{
			"item-1":    editionAPIItem("item-1", "Title", "Author", ""),
			"sync-book": syncBook,
		},
	)
	held := make(chan struct{})
	f.hardcover.HoldSearches(held)

	start := f.do(http.MethodPost, "/api/profiles/"+editionProfileID+"/sync", "")
	require.Equal(t, http.StatusAccepted, start.Code, start.Body.String())
	select {
	case <-f.hardcover.SearchEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("the full sync never reached its Hardcover lookup")
	}

	// The full sync is now active for this profile.
	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	require.Len(t, f.hardcover.RecordedMutations(), 1)

	// Starting another sync is still rejected, and cancelling still works.
	again := f.do(http.MethodPost, "/api/profiles/"+editionProfileID+"/sync", "")
	require.Equal(t, http.StatusConflict, again.Code, again.Body.String())
	cancel := f.do(http.MethodDelete, "/api/profiles/"+editionProfileID+"/sync", "")
	require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())

	status := waitForStatusRun(t, f.multiUser, editionProfileID)
	require.Equal(t, string(syncsvc.RunPhaseCanceled), status.Snapshot.State)

	// Creation still works after the sync ends.
	after := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, after.Code, after.Body.String())
}
