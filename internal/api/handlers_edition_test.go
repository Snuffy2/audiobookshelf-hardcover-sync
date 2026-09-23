package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
			"metadata":  map[string]interface{}{"title": title, "authorName": author, "narratorName": "Narrator", "isbn": "978-0-306-40615-7"},
			"duration":  3600.0,
		},
	}
}

func expandedEditionAPIItem(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("audiobookshelf", "testdata", name))
	require.NoError(t, err)
	var item map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &item))
	return item
}

type editionAPIFixture struct {
	*statusServiceFixture
	routes    http.Handler
	hardcover *editiontest.HardcoverRequestCounter
	abs       *editiontest.AudiobookshelfFake
}

func newEditionAPIFixture(t *testing.T, dryRun bool, records []syncsvc.BookOutcomeRecord, items map[string]map[string]interface{}) *editionAPIFixture {
	t.Helper()
	hardcover := editiontest.NewHardcoverRequestCounter(t)
	abs := editiontest.NewAudiobookshelfFake(t, items)
	for _, item := range items {
		media, _ := item["media"].(map[string]interface{})
		metadata, _ := media["metadata"].(map[string]interface{})
		if name, _ := metadata["authorName"].(string); strings.TrimSpace(name) != "" {
			hardcover.Authors[strings.TrimSpace(name)] = 101
		}
		if authors, ok := metadata["authors"].([]interface{}); ok {
			for _, value := range authors {
				person, _ := value.(map[string]interface{})
				if name, _ := person["name"].(string); strings.TrimSpace(name) != "" {
					hardcover.Authors[strings.TrimSpace(name)] = 101
				}
			}
		}
		if name, _ := metadata["narratorName"].(string); strings.TrimSpace(name) != "" {
			hardcover.Authors[strings.TrimSpace(name)] = 202
		}
		if narrators, ok := metadata["narrators"].([]interface{}); ok {
			for _, value := range narrators {
				if name, _ := value.(string); strings.TrimSpace(name) != "" {
					hardcover.Authors[strings.TrimSpace(name)] = 202
				}
			}
		}
		if name, _ := metadata["publisher"].(string); strings.TrimSpace(name) != "" {
			hardcover.Publishers[strings.TrimSpace(name)] = 303
		}
	}
	fixture := newStatusServiceFixture(t, hardcover.URL)
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
	apiMux.HandleFunc("GET /api/profiles/{id}/edition-capability", handler.GetEditionCapability)
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

const validEditionBody = `{"title":"A Title","isbn_13":"9780306406157","release_date":"2021-02-03","edition_information":"Unabridged"}`

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
		"asin", "audio_seconds", "author_names", "country_id", "dry_run",
		"edition_format", "edition_information", "hardcover_book_id", "isbn_10", "isbn_10_valid", "isbn_13",
		"isbn_13_valid", "language_id", "narrator_names", "publisher_name",
		"reading_format", "release_date", "subtitle", "title", "warnings",
	}, keys)
	require.EqualValues(t, 4242, envelope.Data["hardcover_book_id"])
	require.Equal(t, "Contract Title", envelope.Data["title"])
	require.Equal(t, "Contract Author", envelope.Data["author_names"])
	require.NotContains(t, envelope.Data, "author_ids")
	require.NotContains(t, envelope.Data, "narrator_ids")
	require.NotContains(t, envelope.Data, "publisher_id")
	require.EqualValues(t, 3600, envelope.Data["audio_seconds"])
	require.Equal(t, false, envelope.Data["dry_run"])
	require.IsType(t, []interface{}{}, envelope.Data["warnings"])
	require.NotContains(t, recorder.Body.String(), "abs-secret-token")
	require.Zero(t, f.hardcover.RequestCount(), "a valid audiobook preview must not send any Hardcover request")
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
	require.Zero(t, f.hardcover.RequestCount())
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
	const body = `{"title":"A Title","asin":"B0EXISTING1"}`

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
		`{"title":"A Title","isbn_13":"9781234567897"}`)

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
	require.Zero(t, f.hardcover.RequestCount())

	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusConflict, create.Code, create.Body.String())
	require.Equal(t, want, decodeEnvelope(t, create).Error)
	require.Empty(t, f.hardcover.RecordedMutations())
	require.Zero(t, f.hardcover.RequestCount())
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

	draft := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusOK, draft.Code, draft.Body.String())
	data := decodeEnvelope(t, draft).Data
	require.Equal(t, "ebook", data["reading_format"])
	require.Equal(t, "Ebook", data["edition_format"])
	require.EqualValues(t, 0, data["audio_seconds"])
	require.NotContains(t, data, "author_ids")
	require.NotContains(t, data, "narrator_ids")
	require.NotContains(t, data, "publisher_id")

	require.Zero(t, f.hardcover.RequestCount(), "a valid ebook preview must not send any Hardcover request")

	// The request contains only editable scalars; the server derives ebook metadata.
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

func TestGetEditionDraftMapsExpandedAudiobookAtHTTPBoundary(t *testing.T) {
	item := expandedEditionAPIItem(t, "expanded-audiobook.json")
	f := newEditionAPIFixture(t, false,
		[]syncsvc.BookOutcomeRecord{reviewRecord("item-audiobook", "4242")},
		map[string]map[string]interface{}{"item-audiobook": item},
	)

	response := f.do(http.MethodGet, editionBasePath+"item-audiobook/edition-draft", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	data := decodeEnvelope(t, response).Data
	require.Equal(t, "Expanded Audiobook", data["title"])
	require.Equal(t, "2020-06-15", data["release_date"])
	require.Equal(t, "Abridged", data["edition_information"])
	require.Equal(t, "audiobook", data["reading_format"])
	require.EqualValues(t, 33855, data["audio_seconds"])
	require.Equal(t, "9780306406158", data["isbn_13"])
	require.Equal(t, "", data["isbn_10"])
	require.Equal(t, false, data["isbn_13_valid"])
	require.Equal(t, "Fixture Author", data["author_names"])
	require.Equal(t, "Fixture Narrator", data["narrator_names"])

	warnings, ok := data["warnings"].([]interface{})
	require.True(t, ok)
	warningText := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warningText = append(warningText, warning.(string))
	}
	require.Contains(t, strings.Join(warningText, "\n"), "incorrect check digit")
	require.Contains(t, strings.Join(warningText, "\n"), "tagged \"German\"")
	require.NotContains(t, strings.Join(warningText, "\n"), "match")
	require.Zero(t, f.hardcover.RequestCount())
}

func TestGetEditionDraftMapsExpandedEbookAtHTTPBoundary(t *testing.T) {
	item := expandedEditionAPIItem(t, "expanded-ebook.json")
	f := newEditionAPIFixture(t, false,
		[]syncsvc.BookOutcomeRecord{reviewRecord("item-ebook", "4242")},
		map[string]map[string]interface{}{"item-ebook": item},
	)

	response := f.do(http.MethodGet, editionBasePath+"item-ebook/edition-draft", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	data := decodeEnvelope(t, response).Data
	require.Equal(t, "Expanded Ebook", data["title"])
	require.Equal(t, "2019-01-01", data["release_date"])
	require.Equal(t, "ebook", data["reading_format"])
	require.Equal(t, "Ebook", data["edition_format"])
	require.EqualValues(t, 0, data["audio_seconds"])
	require.Equal(t, "9780525505143", data["isbn_13"])
	require.Equal(t, "0525505148", data["isbn_10"])
	require.Equal(t, true, data["isbn_13_valid"])
	require.Equal(t, true, data["isbn_10_valid"])
	require.Equal(t, "Ebook Fixture Author", data["author_names"])
	require.Equal(t, "", data["narrator_names"])
	require.Equal(t, []interface{}{}, data["warnings"])
	require.Zero(t, f.hardcover.RequestCount())
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

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"A Title"}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Equal(t, "an ASIN or ISBN is required", decodeEnvelope(t, recorder).Error)

	recorder = f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"A Title","isbn_13":"12345"}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "isbn_13")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionAcceptsAHyphenatedISBNAndSendsItNormalized(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
		`{"title":"A Title","isbn_13":"978-0-306-40615-7"}`)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "9780306406157", dto["isbn_13"])
}

func TestCreateEditionByISBN10MatchesAnExistingEdition(t *testing.T) {
	const body = `{"title":"A Title","isbn_10":"0-306-40615-2"}`

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

func TestCreateEditionDerivesNonEditableFieldsFromAudiobookshelf(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "Audiobook", dto["edition_format"])
	require.EqualValues(t, 2, dto["reading_format_id"])
	require.EqualValues(t, 3600, dto["audio_seconds"])
	require.EqualValues(t, 1, dto["language_id"])
	require.EqualValues(t, 1, dto["country_id"])
}

func TestCreateEditionDerivesAudiobookEditionLabelFromAudiobookshelf(t *testing.T) {
	for _, tt := range []struct {
		name      string
		asin      string
		publisher string
		want      string
	}{
		{name: "audible ASIN", asin: "B0AUDIBLE123", want: "Audible Audio"},
		{name: "libro publisher", publisher: "Libro.fm", want: "libro.fm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := editionAPIItem("item-1", "Title", "Author", "")
			metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
			if tt.asin != "" {
				metadata["asin"] = tt.asin
			}
			if tt.publisher != "" {
				metadata["publisher"] = tt.publisher
			}
			f := singleItemFixture(t, false, item)
			recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			mutations := f.hardcover.RecordedMutations()
			require.Len(t, mutations, 1)
			dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
			require.Equal(t, tt.want, dto["edition_format"])
		})
	}
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
		{"client-supplied author ids", withField(`"author_ids":[123]`)},
		{"client-supplied narrator ids", withField(`"narrator_ids":[123]`)},
		{"client-supplied publisher id", withField(`"publisher_id":123`)},
		{"client-supplied edition format", withField(`"edition_format":"Ebook"`)},
		{"client-supplied duration", withField(`"audio_seconds":30`)},
		{"client-supplied language", withField(`"language_id":2`)},
		{"client-supplied country", withField(`"country_id":2`)},
		{"unknown field", withField(`"extra":true`)},
		{"malformed JSON", `{"title":`},
		{"empty body", ``},
		{"null body", `null`},
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
	require.Zero(t, f.abs.ItemRequestCount(), "invalid top-level bodies must be rejected before fetching ABS")
}

func TestCreateEditionRejectsInvalidEditsWithUserReadableError(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))

	f.hardcover.Authors = map[string]int{}
	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Equal(t, "no Audiobookshelf author matched a Hardcover author", decodeEnvelope(t, recorder).Error)
	require.Empty(t, f.hardcover.RecordedMutations())

	f.hardcover.Authors["Author"] = 101
	recorder = f.do(http.MethodPost, editionBasePath+"item-1/edition", `{"title":"   ","isbn_13":"9780306406157"}`)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	require.Contains(t, decodeEnvelope(t, recorder).Error, "title is required")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionResolvesExactExpandedNamesAndReturnsOptionalWarnings(t *testing.T) {
	item := editionAPIItem("item-1", "Title", "Legacy joined name", "")
	metadata := item["media"].(map[string]interface{})["metadata"].(map[string]interface{})
	metadata["authors"] = []interface{}{map[string]interface{}{"name": "Surname, Given"}, map[string]interface{}{"name": "Second Author"}}
	metadata["narrators"] = []interface{}{"Narrator Missing"}
	metadata["publisher"] = "Publisher Missing"
	f := singleItemFixture(t, false, item)
	f.hardcover.Authors["Surname, Given"] = 301
	f.hardcover.Authors["Second Author"] = 302
	delete(f.hardcover.Authors, "Narrator Missing")
	delete(f.hardcover.Publishers, "Publisher Missing")

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, map[string]interface{}{
		"edition_id": float64(777), "dry_run": false,
		"warnings": []interface{}{
			"One or more Audiobookshelf narrators did not match a Hardcover narrator and were omitted.",
			"Audiobookshelf publisher did not match a Hardcover publisher and was omitted.",
		},
	}, decodeEnvelope(t, recorder).Data)
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	contributions := dto["contributions"].([]interface{})
	require.Len(t, contributions, 2)
	require.EqualValues(t, 301, contributions[0].(map[string]interface{})["author_id"])
	require.EqualValues(t, 302, contributions[1].(map[string]interface{})["author_id"])
	require.NotContains(t, dto, "publisher_id")
}

func TestCreateEditionLookupFailureIsSanitizedAndDoesNotInsert(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	f.hardcover.SearchStatus = http.StatusUnauthorized
	f.hardcover.SearchBody = `{"message":"private upstream detail and token"}`

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.Equal(t, "Could not complete the request with hardcover", decodeEnvelope(t, recorder).Error)
	require.NotContains(t, recorder.Body.String(), "private upstream detail")
	require.Empty(t, f.hardcover.RecordedMutations())
}

func TestCreateEditionAcceptsBadISBNChecksumWithFixedWarning(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
		`{"title":"A Title","isbn_13":"9780306406158"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []interface{}{"ISBN-13 has an invalid check digit."}, decodeEnvelope(t, recorder).Data["warnings"])
	mutations := f.hardcover.RecordedMutations()
	require.Len(t, mutations, 1)
	dto := mutations[0]["edition"].(map[string]interface{})["dto"].(map[string]interface{})
	require.Equal(t, "9780306406158", dto["isbn_13"])
}

func TestInvalidISBNWarningDoesNotAssumeInsertForDryRunOrReuse(t *testing.T) {
	for _, tt := range []struct {
		name   string
		dryRun bool
		reuse  bool
		wantID float64
	}{
		{name: "duplicate reuse", reuse: true, wantID: 555},
		{name: "dry run", dryRun: true, wantID: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := singleItemFixture(t, tt.dryRun, editionAPIItem("item-1", "Title", "Author", ""))
			if tt.reuse {
				f.hardcover.ISBNs["9780306406158"] = editiontest.ExistingEdition{EditionID: 555, BookID: 4242}
			}
			recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition",
				`{"title":"A Title","isbn_13":"9780306406158"}`)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			data := decodeEnvelope(t, recorder).Data
			require.Equal(t, tt.wantID, data["edition_id"])
			require.Equal(t, []interface{}{"ISBN-13 has an invalid check digit."}, data["warnings"])
			require.Empty(t, f.hardcover.RecordedWrites())
		})
	}
}

func TestCreateEditionInsufficientScopeIsForbiddenWithoutRetry(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	f.hardcover.InsertStatus = http.StatusForbidden
	f.hardcover.InsertBody = `{"error":"insufficient_scope","scope":"write:catalog:append","message":"private upstream details"}`

	recorder := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Equal(t, "The profile's Hardcover token needs the write:catalog:append scope to create editions", decodeEnvelope(t, recorder).Error)
	require.NotContains(t, recorder.Body.String(), "private upstream details")
	require.Len(t, f.hardcover.RecordedMutations(), 1, "HTTP 403 must not be retried")
}

func TestEditionDryRunProfilesIssueNoHardcoverMutation(t *testing.T) {
	f := singleItemFixture(t, true, editionAPIItem("item-1", "Title", "Author", ""))

	create := f.do(http.MethodPost, editionBasePath+"item-1/edition", validEditionBody)
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	require.Equal(t, map[string]interface{}{"edition_id": float64(0), "dry_run": true, "warnings": []interface{}{}}, decodeEnvelope(t, create).Data)

	draft := f.do(http.MethodGet, editionBasePath+"item-1/edition-draft", "")
	require.Equal(t, http.StatusOK, draft.Code, draft.Body.String())
	require.Equal(t, true, decodeEnvelope(t, draft).Data["dry_run"])

	require.Empty(t, f.hardcover.RecordedWrites(), "dry-run may read metadata but must not send a Hardcover mutation")
}

func TestEditionCapabilityProbeIsImpossibleCachedAndInvalidatedOnTokenChange(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	path := "/api/profiles/" + editionProfileID + "/edition-capability"
	for i := 0; i < 2; i++ {
		recorder := f.do(http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, map[string]interface{}{"can_create": true}, decodeEnvelope(t, recorder).Data)
	}
	require.Equal(t, 1, f.hardcover.ProbeCount(), "the positive result should be cached")
	require.Equal(t, []float64{-1}, f.hardcover.ProbeBookIDs())

	require.NoError(t, f.multiUser.UpdateProfileConfig(
		editionProfileID, f.abs.URL, "abs-secret-token", "rotated-hc-token", database.SyncConfigData{DryRun: false},
	))
	recorder := f.do(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 2, f.hardcover.ProbeCount(), "a token change invalidates the prior capability")
}

func TestEditionCapabilityDistinguishesDeniedAndUnverified(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     int
		body       string
		want       map[string]interface{}
		wantProbes int
	}{
		{
			name:       "missing append scope",
			status:     http.StatusForbidden,
			body:       `{"error":"insufficient_scope","scope":"write:catalog:append"}`,
			want:       map[string]interface{}{"can_create": false, "missing_scope": "write:catalog:append"},
			wantProbes: 1,
		},
		{
			name:       "other authentication failure is unverified",
			status:     http.StatusUnauthorized,
			body:       `{"error":"invalid_token","message":"secret remote detail"}`,
			want:       map[string]interface{}{"can_create": false, "reason": "unverified"},
			wantProbes: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
			f.hardcover.ProbeStatus = tt.status
			f.hardcover.ProbeBody = tt.body
			recorder := f.do(http.MethodGet, "/api/profiles/"+editionProfileID+"/edition-capability", "")
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, tt.want, decodeEnvelope(t, recorder).Data)
			require.Equal(t, tt.wantProbes, f.hardcover.ProbeCount())
			require.NotContains(t, recorder.Body.String(), "secret remote detail")
		})
	}
}

func TestDryRunEditionCapabilityDoesNotProbe(t *testing.T) {
	f := singleItemFixture(t, true, editionAPIItem("item-1", "Title", "Author", ""))
	recorder := f.do(http.MethodGet, "/api/profiles/"+editionProfileID+"/edition-capability", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, map[string]interface{}{"can_create": true}, decodeEnvelope(t, recorder).Data)
	require.Zero(t, f.hardcover.ProbeCount())
	require.Empty(t, f.hardcover.RecordedWrites())
}

func TestEditionCapabilityConcurrentCallsShareOneProbe(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	hold := make(chan struct{})
	f.hardcover.HoldProbes(hold)
	path := "/api/profiles/" + editionProfileID + "/edition-capability"
	first := make(chan *httptest.ResponseRecorder, 1)
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.do(http.MethodGet, path, "") }()
	select {
	case <-f.hardcover.ProbeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the capability probe did not reach Hardcover")
	}
	go func() { second <- f.do(http.MethodGet, path, "") }()
	close(hold)
	for _, response := range []<-chan *httptest.ResponseRecorder{first, second} {
		select {
		case recorder := <-response:
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		case <-time.After(5 * time.Second):
			t.Fatal("a capability request did not finish")
		}
	}
	require.Equal(t, 1, f.hardcover.ProbeCount())
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
	require.Zero(t, f.hardcover.RequestCount())
}

func TestGetEditionDraftDoesNotReportCallerCancellationAsUpstreamFailure(t *testing.T) {
	f := singleItemFixture(t, false, editionAPIItem("item-1", "Title", "Author", ""))
	holdReads := make(chan struct{})
	f.abs.HoldReads(holdReads)
	t.Cleanup(f.abs.ReleaseReads)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, editionBasePath+"item-1/edition-draft", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		f.routes.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case <-f.abs.ReadEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the request never reached Audiobookshelf")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the handler did not stop after its caller canceled")
	}

	require.NotEqual(t, http.StatusBadGateway, recorder.Code)
	require.Empty(t, recorder.Body.String(), "a canceled caller should not receive an upstream failure response")
	require.Zero(t, f.hardcover.RequestCount())
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
