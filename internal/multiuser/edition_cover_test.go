package multiuser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	stdSync "sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	syncsvc "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const (
	hardcoverUploadHost = "hardcover.app"
	coverStorageHost    = "storage.example.test"
)

type coverRequest struct {
	host          string
	authorization string
}

// editionCoverTransport is the HTTP client transport handed to the edition
// creator in these tests. It serves the Audiobookshelf cover, Hardcover's
// upload-credential endpoint, and the storage bucket from memory, records what
// each request carried, and fails any other host, so no test can reach the
// real network through the creator.
type editionCoverTransport struct {
	absHost    string
	failUpload bool // the Hardcover upload-credential endpoint answers 500

	mu       stdSync.Mutex
	requests []coverRequest
}

func (rt *editionCoverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host // host:port, which identifies the loopback Audiobookshelf fake
	rt.mu.Lock()
	rt.requests = append(rt.requests, coverRequest{host: host, authorization: req.Header.Get("Authorization")})
	rt.mu.Unlock()

	reply := func(status int, contentType, body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{contentType}},
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    req,
		}, nil
	}
	switch host {
	case rt.absHost:
		return reply(http.StatusOK, "image/jpeg", "image-bytes")
	case hardcoverUploadHost:
		if rt.failUpload {
			return reply(http.StatusInternalServerError, "text/plain", "upload unavailable")
		}
		return reply(http.StatusOK, "application/json",
			`{"url":"https://`+coverStorageHost+`/upload","fields":{"key":"editions/777/cover.jpg"}}`)
	case coverStorageHost:
		return reply(http.StatusNoContent, "text/plain", "")
	}
	return nil, errors.New("unexpected request to " + host)
}

func (rt *editionCoverTransport) recorded() []coverRequest {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]coverRequest(nil), rt.requests...)
}

func (rt *editionCoverTransport) reached(host string) bool {
	for _, r := range rt.recorded() {
		if r.host == host {
			return true
		}
	}
	return false
}

// useCoverTransport makes the service build creators around rt. When
// forceRealCreator is set the creator is told it is NOT a dry run, whatever the
// profile says, so only the service's own dry-run handling of the Hardcover
// client stands between the request and Hardcover.
func useCoverTransport(f *editionFixture, rt *editionCoverTransport, forceRealCreator bool) {
	f.service.newEditionCreator = func(client edition.HardcoverClient, dryRun bool, token string) *edition.Creator {
		if forceRealCreator {
			dryRun = false
		}
		return edition.NewCreatorWithHTTPClient(client, logger.Get(), dryRun, token, &http.Client{Transport: rt})
	}
}

func newCoverFixture(t *testing.T, dryRun bool) (*editionFixture, *editionCoverTransport) {
	t.Helper()
	f := newEditionFixture(t, dryRun,
		[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
		map[string]map[string]interface{}{"item-1": editionItemWithCover("item-1", "A Title", "An Author")},
	)
	absURL, err := url.Parse(f.abs.URL)
	require.NoError(t, err)
	return f, &editionCoverTransport{absHost: absURL.Host}
}

func TestCreateEditionFromRunBook_CoverTransfersKeepTheAudiobookshelfTokenScoped(t *testing.T) {
	tests := []struct {
		name         string
		failUpload   bool
		wantWarnings []string
	}{
		{name: "cover uploaded", wantWarnings: []string{}},
		{name: "cover upload fails but the edition is still created", failUpload: true, wantWarnings: []string{editionCoverWarning}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rt := newCoverFixture(t, false)
			rt.failUpload = tt.failUpload
			useCoverTransport(f, rt, false)

			created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())

			require.NoError(t, err, "a cover failure must not fail the request")
			require.Equal(t, &EditionCreated{EditionID: 777, DryRun: false, Warnings: tt.wantWarnings}, created)
			require.Len(t, f.hardcover.recordedMutations(), 1)

			require.True(t, rt.reached(rt.absHost), "the cover must be downloaded from Audiobookshelf")
			require.True(t, rt.reached(hardcoverUploadHost), "the upload must be attempted")
			for _, r := range rt.recorded() {
				if r.host == rt.absHost {
					require.Equal(t, "Bearer abs-token", r.authorization, "Audiobookshelf must receive the profile token")
				} else {
					require.NotContains(t, r.authorization, "abs-token", "the Audiobookshelf token must not reach %s", r.host)
				}
			}
		})
	}
}

// TestCreateEditionFromRunBook_DryRunIsEnforcedAtTheHardcoverClient builds the
// creator as if it were not in dry-run mode, so a Hardcover write can only be
// stopped by the service switching the concrete client into dry-run.
func TestCreateEditionFromRunBook_DryRunIsEnforcedAtTheHardcoverClient(t *testing.T) {
	tests := []struct {
		name       string
		dryRun     bool
		wantWrites bool
	}{
		{name: "dry-run profile writes nothing", dryRun: true, wantWrites: false},
		{name: "control: a normal profile does write", dryRun: false, wantWrites: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rt := newCoverFixture(t, tt.dryRun)
			useCoverTransport(f, rt, true)

			// The outcome differs per row; only the writes matter here.
			_, _ = f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", validEdits())

			if tt.wantWrites {
				require.NotEmpty(t, f.hardcover.recordedWrites())
				return
			}
			require.Empty(t, f.hardcover.recordedWrites(), "no Hardcover mutation may be sent")
			require.False(t, rt.reached(hardcoverUploadHost), "no upload may be started")
			require.False(t, rt.reached(coverStorageHost), "no upload may be started")
		})
	}
}

// TestCreateEditionFromRunBook_ReusesAnEditionByASINWithoutChangingIt covers the
// ASIN of an edition that already exists on Hardcover. The item has a cover, so
// any creation work would show up as a mutation or a cover transfer.
func TestCreateEditionFromRunBook_ReusesAnEditionByASINWithoutChangingIt(t *testing.T) {
	const asin = "B0EXISTING1"
	tests := []struct {
		name        string
		dryRun      bool
		existing    *existingEdition
		wantCreated *EditionCreated
		wantErr     error
		wantWrites  bool
	}{
		{
			name:        "same book returns the existing edition",
			existing:    &existingEdition{editionID: 555, bookID: 4242},
			wantCreated: &EditionCreated{EditionID: 555, Warnings: []string{}},
		},
		{
			name:     "another book's edition is rejected",
			existing: &existingEdition{editionID: 555, bookID: 9999},
			wantErr:  ErrEditionConflict,
		},
		{
			name:        "unknown ASIN creates the edition",
			wantCreated: &EditionCreated{EditionID: 777, Warnings: []string{}},
			wantWrites:  true,
		},
		{
			name:        "dry run does not consult existing editions",
			dryRun:      true,
			existing:    &existingEdition{editionID: 555, bookID: 4242},
			wantCreated: &EditionCreated{EditionID: 0, DryRun: true, Warnings: []string{}},
		},
		{
			name:        "dry run ignores a colliding ASIN on another book",
			dryRun:      true,
			existing:    &existingEdition{editionID: 555, bookID: 9999},
			wantCreated: &EditionCreated{EditionID: 0, DryRun: true, Warnings: []string{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rt := newCoverFixture(t, tt.dryRun)
			useCoverTransport(f, rt, false)
			if tt.existing != nil {
				f.hardcover.asins[asin] = *tt.existing
			}
			edits := validEdits()
			edits.ASIN = asin

			created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.NotContains(t, err.Error(), "9999", "the other book's ID must not leak")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantCreated, created)
			}
			if tt.wantWrites {
				require.NotEmpty(t, f.hardcover.recordedWrites())
				return
			}
			require.Empty(t, f.hardcover.recordedWrites(), "no Hardcover mutation may be sent")
			require.Empty(t, rt.recorded(), "no cover may be downloaded or uploaded")
		})
	}
}

// TestCreateEditionFromRunBook_ISBNMatchNeverTouchesAnotherBooksEdition covers an
// ISBN that already identifies an edition on Hardcover. The caller-chosen ISBN
// must not let a request adopt, or attach a cover to, another book's edition; an
// edition of the target book is returned untouched. The match is found before
// any insert is attempted.
func TestCreateEditionFromRunBook_ISBNMatchNeverTouchesAnotherBooksEdition(t *testing.T) {
	const isbn13 = "9781234567897"
	const isbn10 = "123456789X" // the same book's ISBN-10
	tests := []struct {
		name        string
		isbn        string // the identifier the fake Hardcover knows the edition by
		existing    existingEdition
		wantErr     error
		wantCreated *EditionCreated
	}{
		{name: "another book's ISBN-13 edition is rejected", isbn: isbn13, existing: existingEdition{editionID: 555, bookID: 9999}, wantErr: ErrEditionConflict},
		{name: "another book's ISBN-10 edition is rejected", isbn: isbn10, existing: existingEdition{editionID: 555, bookID: 9999}, wantErr: ErrEditionConflict},
		{name: "the target book's ISBN-13 edition is returned untouched", isbn: isbn13, existing: existingEdition{editionID: 555, bookID: 4242}, wantCreated: &EditionCreated{EditionID: 555, Warnings: []string{}}},
		{name: "the target book's ISBN-10 edition is returned untouched", isbn: isbn10, existing: existingEdition{editionID: 555, bookID: 4242}, wantCreated: &EditionCreated{EditionID: 555, Warnings: []string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rt := newCoverFixture(t, false)
			useCoverTransport(f, rt, false)
			f.hardcover.isbns[tt.isbn] = tt.existing
			edits := validEdits()
			edits.ISBN13 = isbn13

			created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantCreated, created)
			}
			require.Empty(t, f.hardcover.recordedWrites(), "no insert, image, or update mutation may be sent")
			require.Empty(t, rt.recorded(), "no cover may be downloaded or uploaded")
		})
	}
}

// TestCreateEditionFromRunBook_OnlyAnAudiobookEditionCountsAsAMatch pins that the
// existing-edition lookups are audiobook-format only: an edition with the same
// identifier in another format, or with no reading format, is not a match, so the
// edition is created.
func TestCreateEditionFromRunBook_OnlyAnAudiobookEditionCountsAsAMatch(t *testing.T) {
	const asin = "B0EXISTING1"
	const isbn13 = "9781234567897"
	tests := []struct {
		name     string
		existing existingEdition
		wantID   int
		wantMade bool
	}{
		{name: "an audiobook edition is a match", existing: existingEdition{editionID: 555, bookID: 4242}, wantID: 555},
		{name: "an ebook edition is not a match", existing: existingEdition{editionID: 555, bookID: 4242, readingFormat: 4}, wantID: 777, wantMade: true},
		{name: "a physical edition is not a match", existing: existingEdition{editionID: 555, bookID: 4242, readingFormat: 1}, wantID: 777, wantMade: true},
		{name: "an edition with no reading format is not a match", existing: existingEdition{editionID: 555, bookID: 4242, readingFormat: -1}, wantID: 777, wantMade: true},
		{name: "another book's ebook edition does not block creation", existing: existingEdition{editionID: 555, bookID: 9999, readingFormat: 4}, wantID: 777, wantMade: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEditionFixture(t, false,
				[]syncsvc.BookOutcomeRecord{needsReview("item-1", "4242")},
				map[string]map[string]interface{}{"item-1": editionItem("item-1", "A Title", "An Author")},
			)
			f.hardcover.asins[asin] = tt.existing
			f.hardcover.isbns[isbn13] = tt.existing
			edits := validEdits()
			edits.ASIN = asin
			edits.ISBN13 = isbn13

			created, err := f.service.CreateEditionFromRunBook(context.Background(), "profile-1", "run-1", "item-1", edits)

			require.NoError(t, err)
			require.Equal(t, tt.wantID, created.EditionID)
			require.Equal(t, tt.wantMade, len(f.hardcover.recordedMutations()) == 1, "the insert happens only when no audiobook edition matched")
		})
	}
}
