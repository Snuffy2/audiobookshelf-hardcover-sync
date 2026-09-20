package multiuser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition/draft"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const (
	// maxEditionPeople bounds the author and narrator ID lists a caller may submit.
	maxEditionPeople = 50
	// maxEditionFormatLength bounds the free-text edition format label.
	maxEditionFormatLength = 100
	// EditionCreateTimeout bounds one edition creation, including the cover
	// upload. The HTTP layer sizes its response write deadline from it.
	EditionCreateTimeout = 2 * time.Minute
	// editionImageClientTimeout bounds a single cover download or upload request.
	editionImageClientTimeout = 60 * time.Second
)

var (
	// ErrEditionNotFound indicates that the run, or the book record within it, does not exist.
	ErrEditionNotFound = errors.New("sync run or book record not found")

	// ErrEditionNotEligible indicates that the run record is not a needs-review
	// outcome with a Hardcover book to attach an edition to.
	ErrEditionNotEligible = errors.New("book is not eligible for edition creation")

	// ErrEditionItemNotFound indicates that Audiobookshelf no longer has the item.
	ErrEditionItemNotFound = errors.New("audiobookshelf item not found")

	// ErrEditionConflict indicates that the submitted ASIN or ISBN-13 already
	// belongs to an edition that is not confirmed to be of the run record's
	// Hardcover book: another book's, or one whose book ID is unknown or differs
	// for another reason, such as a merged or canonical book.
	ErrEditionConflict = errors.New("asin or isbn-13 belongs to an edition of a different hardcover book")

	// ErrEditionInProgress indicates that an edition submit for the same book is already running.
	ErrEditionInProgress = errors.New("an edition is already being created for this book")
)

// EditionValidationError reports edition data that cannot be submitted. Its
// message is safe to show to the user.
type EditionValidationError struct {
	Err error
}

func (e *EditionValidationError) Error() string { return e.Err.Error() }
func (e *EditionValidationError) Unwrap() error { return e.Err }

// EditionUpstreamError reports a failed Audiobookshelf or Hardcover call. The
// wrapped error may contain remote details and must not be shown to users.
type EditionUpstreamError struct {
	Service string
	Err     error
}

func (e *EditionUpstreamError) Error() string {
	return fmt.Sprintf("%s request failed: %v", e.Service, e.Err)
}
func (e *EditionUpstreamError) Unwrap() error { return e.Err }

// EditionEdits is the complete set of values a caller may submit when creating
// an edition. The Hardcover book and the cover image URL are deliberately
// absent: the server derives both, so a request can neither retarget the
// edition nor point the Audiobookshelf token at another host.
type EditionEdits struct {
	Title              string `json:"title"`
	Subtitle           string `json:"subtitle"`
	ASIN               string `json:"asin"`
	ISBN10             string `json:"isbn_10"`
	ISBN13             string `json:"isbn_13"`
	ReleaseDate        string `json:"release_date"`
	EditionInformation string `json:"edition_information"`
	EditionFormat      string `json:"edition_format"`
	AudioSeconds       int    `json:"audio_seconds"`
	LanguageID         int    `json:"language_id"`
	CountryID          int    `json:"country_id"`
	AuthorIDs          []int  `json:"author_ids"`
	NarratorIDs        []int  `json:"narrator_ids"`
	PublisherID        int    `json:"publisher_id"`
}

// EditionCreated is the result of a create request. EditionID is 0 for a dry run.
// When the ASIN or ISBN-13 already identifies an edition of the target book,
// that edition is returned untouched and nothing is created.
// Warnings lists user-readable problems that did not stop the edition from
// being created, such as a cover image that could not be uploaded. It is never
// nil so it always serializes as an array.
type EditionCreated struct {
	EditionID int      `json:"edition_id"`
	DryRun    bool     `json:"dry_run"`
	Warnings  []string `json:"warnings"`
}

// editionCoverWarning is shown when the edition exists but its cover does not.
// It deliberately omits the underlying error, which may hold remote details.
const editionCoverWarning = "The edition was created, but its cover image could not be uploaded."

// editionTarget is a needs-review run record resolved for edition creation.
type editionTarget struct {
	profile         *database.ProfileWithTokens
	hardcoverBookID int
}

// PrepareEditionDraft builds a previewable edition for a needs-review book from
// a retained or live sync run. It only reads from Audiobookshelf and Hardcover.
func (s *MultiUserService) PrepareEditionDraft(ctx context.Context, profileID, runID, bookID string) (*draft.Draft, error) {
	// A draft is read-only and bound to ctx, so it needs no drain on shutdown or
	// profile deletion; it only refuses to start once either has begun.
	if err := s.checkEditionAdmission(profileID); err != nil {
		return nil, err
	}

	target, err := s.resolveEditionTarget(profileID, runID, bookID)
	if err != nil {
		return nil, err
	}
	item, err := s.fetchEditionItem(ctx, target.profile, bookID)
	if err != nil {
		return nil, err
	}

	hcClient := s.newHardcoverClient(target.profile.HardcoverToken)
	built, err := draft.New(ctx, *item, target.hardcoverBookID, target.profile.AudiobookshelfURL, hcClient, target.profile.SyncConfig.AudnexusRegion)
	if err != nil {
		return nil, fmt.Errorf("build edition draft: %w", err)
	}
	built.DryRun = target.profile.SyncConfig.DryRun
	return built, nil
}

// CreateEditionFromRunBook creates a Hardcover edition for a needs-review book.
// The target Hardcover book always comes from the run record and the cover URL
// from the profile's Audiobookshelf server; edits supplies only editable fields.
// It does not touch sync state and may run alongside a full sync. A profile in
// dry-run mode validates the request but issues no Hardcover mutation. Shutdown
// cancels running syncs first and then waits for in-flight creates to finish.
func (s *MultiUserService) CreateEditionFromRunBook(ctx context.Context, profileID, runID, bookID string, edits EditionEdits) (*EditionCreated, error) {
	gate, err := s.beginEditionWork(profileID)
	if err != nil {
		return nil, err
	}
	defer s.endEditionWork(gate)

	target, err := s.resolveEditionTarget(profileID, runID, bookID)
	if err != nil {
		return nil, err
	}

	release, ok := s.claimEdition(profileID, bookID)
	if !ok {
		return nil, ErrEditionInProgress
	}
	defer release()

	item, err := s.fetchEditionItem(ctx, target.profile, bookID)
	if err != nil {
		return nil, err
	}

	input := &edition.EditionInput{
		BookID:        target.hardcoverBookID,
		Title:         edits.Title,
		Subtitle:      edits.Subtitle,
		ImageURL:      draft.CoverURL(target.profile.AudiobookshelfURL, *item),
		ISBN10:        edits.ISBN10,
		ISBN13:        edits.ISBN13,
		ASIN:          edits.ASIN,
		PublisherID:   edits.PublisherID,
		LanguageID:    edits.LanguageID,
		CountryID:     edits.CountryID,
		AuthorIDs:     edits.AuthorIDs,
		NarratorIDs:   edits.NarratorIDs,
		AudioLength:   edits.AudioSeconds,
		ReleaseDate:   edits.ReleaseDate,
		EditionInfo:   edits.EditionInformation,
		EditionFormat: edits.EditionFormat,
	}
	if err := validateEditionInput(input); err != nil {
		return nil, &EditionValidationError{Err: err}
	}

	dryRun := target.profile.SyncConfig.DryRun
	hcClient := s.newHardcoverClient(target.profile.HardcoverToken)
	// Dry run is enforced at both the creator and the concrete client boundary.
	hcClient.SetDryRun(dryRun)
	creator := s.editionCreator(hcClient, dryRun, target.profile.AudiobookshelfToken)
	creator.SetAudiobookshelfBaseURL(target.profile.AudiobookshelfURL)

	// Finish the creation even if the caller disconnects, so an edition is not
	// left without its cover; the timeout still bounds the work.
	createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), EditionCreateTimeout)
	defer cancel()

	result, err := creator.CreateEdition(createCtx, input)
	if err != nil {
		if errors.Is(err, edition.ErrEditionBelongsToOtherBook) {
			return nil, ErrEditionConflict
		}
		return nil, &EditionUpstreamError{Service: "hardcover", Err: err}
	}
	created := &EditionCreated{EditionID: result.EditionID, DryRun: dryRun, Warnings: []string{}}
	if result.ImageError != "" {
		created.Warnings = append(created.Warnings, editionCoverWarning)
	}
	return created, nil
}

// editionCreator builds the creator for one create request, using
// s.newEditionCreator when a test has replaced it.
func (s *MultiUserService) editionCreator(client edition.HardcoverClient, dryRun bool, audiobookshelfToken string) *edition.Creator {
	if s.newEditionCreator != nil {
		return s.newEditionCreator(client, dryRun, audiobookshelfToken)
	}
	return edition.NewCreatorWithHTTPClient(
		client,
		s.logger,
		dryRun,
		audiobookshelfToken,
		&http.Client{Timeout: editionImageClientTimeout},
	)
}

// resolveEditionTarget loads the profile and finds the needs-review record for
// bookID in runID. The Hardcover book ID is taken only from that record.
func (s *MultiUserService) resolveEditionTarget(profileID, runID, bookID string) (*editionTarget, error) {
	if runID == "" || bookID == "" {
		return nil, ErrEditionNotFound
	}
	profile, err := s.GetProfile(profileID)
	if err != nil {
		return nil, fmt.Errorf("load sync profile: %w", err)
	}
	if profile == nil {
		return nil, ErrProfileNotFound
	}

	snapshot, err := s.GetSyncRunSnapshot(profileID, runID)
	if err != nil {
		return nil, fmt.Errorf("load sync run: %w", err)
	}
	if snapshot == nil || snapshot.RunID != runID || (snapshot.ProfileID != "" && snapshot.ProfileID != profileID) {
		return nil, ErrEditionNotFound
	}

	for _, record := range snapshot.BookOutcomes {
		if record.BookID != bookID {
			continue
		}
		if record.Outcome != sync.OutcomeNeedsReview {
			return nil, ErrEditionNotEligible
		}
		hardcoverBookID, err := strconv.Atoi(record.HardcoverBookID)
		if err != nil || hardcoverBookID <= 0 {
			return nil, ErrEditionNotEligible
		}
		return &editionTarget{profile: profile, hardcoverBookID: hardcoverBookID}, nil
	}
	return nil, ErrEditionNotFound
}

// fetchEditionItem reads the current Audiobookshelf item for bookID.
func (s *MultiUserService) fetchEditionItem(ctx context.Context, profile *database.ProfileWithTokens, bookID string) (*models.AudiobookshelfBook, error) {
	absClient := audiobookshelf.NewClient(profile.AudiobookshelfURL, profile.AudiobookshelfToken)
	item, err := absClient.GetLibraryItem(ctx, bookID)
	if err != nil {
		if errors.Is(err, audiobookshelf.ErrItemNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrEditionItemNotFound, bookID)
		}
		return nil, &EditionUpstreamError{Service: "audiobookshelf", Err: err}
	}
	return item, nil
}

// claimEdition marks one profile/book pair as being submitted. It reports false
// when that pair is already in flight; otherwise the returned func releases it.
func (s *MultiUserService) claimEdition(profileID, bookID string) (release func(), ok bool) {
	key := profileID + "\x00" + bookID
	s.editionMutex.Lock()
	defer s.editionMutex.Unlock()
	if _, busy := s.editionsInFlight[key]; busy {
		return nil, false
	}
	if s.editionsInFlight == nil {
		s.editionsInFlight = make(map[string]struct{})
	}
	s.editionsInFlight[key] = struct{}{}
	return func() {
		s.editionMutex.Lock()
		defer s.editionMutex.Unlock()
		delete(s.editionsInFlight, key)
	}, true
}

// validateEditionInput trims the title, then applies the creator's own rules
// plus bounds on the caller-supplied ID lists.
func validateEditionInput(input *edition.EditionInput) error {
	// A blank title is as missing as an empty one, and the stored title is trimmed.
	input.Title = strings.TrimSpace(input.Title)
	if err := input.Validate(); err != nil {
		return err
	}
	if len(input.AuthorIDs) > maxEditionPeople || len(input.NarratorIDs) > maxEditionPeople {
		return fmt.Errorf("at most %d authors and %d narrators are allowed", maxEditionPeople, maxEditionPeople)
	}
	for _, id := range append(append([]int{}, input.AuthorIDs...), input.NarratorIDs...) {
		if id <= 0 {
			return errors.New("author and narrator IDs must be positive")
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(input.EditionFormat)) > maxEditionFormatLength {
		return fmt.Errorf("edition format must be at most %d characters", maxEditionFormatLength)
	}
	if input.PublisherID < 0 || input.LanguageID < 0 || input.CountryID < 0 || input.AudioLength < 0 {
		return errors.New("publisher, language, country, and audio length must not be negative")
	}
	return nil
}
