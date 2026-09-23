package multiuser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/edition/draft"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/isbn"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

const (
	// EditionDraftTimeout bounds draft preparation to less than Audiobookshelf's
	// per-request timeout, leaving time to serialize and write the response.
	EditionDraftTimeout = audiobookshelf.RequestTimeout - 5*time.Second
	// maxEditionPeople bounds the author and narrator names resolved from ABS.
	maxEditionPeople = 50
	// EditionCreateTimeout bounds one edition creation from the ABS refetch
	// through Hardcover metadata resolution, duplicate checks, and insertion.
	// The HTTP layer sizes its response write deadline from it.
	EditionCreateTimeout = 2 * time.Minute
	// EditionCapabilityCacheTTL keeps definite scope answers briefly while
	// allowing token changes to be observed quickly.
	EditionCapabilityCacheTTL = 3 * time.Minute
	// EditionCapabilityUnverifiedTTL avoids hammering Hardcover during outages.
	EditionCapabilityUnverifiedTTL = 5 * time.Second
	EditionCapabilityTimeout       = 45 * time.Second
	editionLookupLimit             = 10
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

	// ErrEditionConflict indicates that the submitted ASIN or ISBN already
	// belongs to an edition that is not confirmed to be of the run record's
	// Hardcover book: another book's, or one whose book ID is unknown or differs
	// for another reason, such as a merged or canonical book.
	ErrEditionConflict = errors.New("asin or isbn belongs to an edition of a different hardcover book")

	// ErrEditionNoIdentifier indicates that the Audiobookshelf item has neither
	// an ASIN nor a valid ISBN, so an edition created for it could never be
	// matched by a later sync.
	ErrEditionNoIdentifier = errors.New("audiobookshelf item has no asin or isbn")

	// ErrEditionInProgress indicates that an edition submit for the same book is already running.
	ErrEditionInProgress = errors.New("an edition is already being created for this book")

	// ErrEditionAuthorNotFound means none of the Audiobookshelf author names
	// exactly matched an author in Hardcover.
	ErrEditionAuthorNotFound = errors.New("no Audiobookshelf author matched a Hardcover author")

	// ErrEditionInsufficientScope means the profile token cannot append catalog editions.
	ErrEditionInsufficientScope = errors.New("Hardcover token is missing the required edition creation scope")
)

// EditionValidationError reports edition data that cannot be submitted. Its
// message is safe to show to the user.
type EditionValidationError struct {
	Err error
}

func (e *EditionValidationError) Error() string { return e.Err.Error() }
func (e *EditionValidationError) Unwrap() error { return e.Err }

// EditionUpstreamError reports a failed Audiobookshelf or Hardcover call, or
// an overall draft timeout. The wrapped error may contain remote details and
// must not be shown to users.
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
}

// EditionCapability reports whether the current profile can create editions.
// An unverified result is deliberately false so callers only enable creation
// after a definite positive probe.
type EditionCapability struct {
	CanCreate    bool   `json:"can_create"`
	MissingScope string `json:"missing_scope,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// EditionCreated is the result of a create request. EditionID is 0 for a dry run.
// When the ASIN or ISBN already identifies an edition of the target book,
// that edition is returned untouched and nothing is created.
// Warnings is reserved for user-readable nonfatal creation problems. It is
// never nil so it always serializes as an array.
type EditionCreated struct {
	EditionID int      `json:"edition_id"`
	DryRun    bool     `json:"dry_run"`
	Warnings  []string `json:"warnings"`
}

// editionTarget is a needs-review run record resolved for edition creation.
type editionTarget struct {
	profile         *database.ProfileWithTokens
	hardcoverBookID int
}

// PrepareEditionDraft builds a previewable edition for a needs-review book from
// a retained or live sync run. It reads Audiobookshelf metadata and may enrich
// an audiobook ASIN with Audnex; it does not use Hardcover.
func (s *MultiUserService) PrepareEditionDraft(ctx context.Context, profileID, runID, bookID string) (*draft.Draft, error) {
	return s.prepareEditionDraft(ctx, profileID, runID, bookID, EditionDraftTimeout)
}

// prepareEditionDraft runs draft preparation with an overall budget. Keeping
// the budget as an argument lets tests exercise timeout behavior quickly while
// production callers use the fixed EditionDraftTimeout above.
func (s *MultiUserService) prepareEditionDraft(ctx context.Context, profileID, runID, bookID string, timeout time.Duration) (*draft.Draft, error) {
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(callerCtx, timeout)
	defer cancel()
	if err := callerCtx.Err(); err != nil {
		return nil, err
	}

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
		if callerCtx.Err() != nil {
			return nil, callerCtx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &EditionUpstreamError{Service: "audiobookshelf", Err: err}
		}
		return nil, err
	}
	if !hasEditionIdentifier(item) {
		return nil, ErrEditionNoIdentifier
	}

	built, err := draft.New(ctx, *item, target.hardcoverBookID, target.profile.SyncConfig.AudnexusRegion)
	if err != nil {
		if callerCtx.Err() != nil {
			return nil, callerCtx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &EditionUpstreamError{Service: "audiobookshelf", Err: err}
		}
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
	return s.createEditionFromRunBook(ctx, profileID, runID, bookID, edits, EditionCreateTimeout)
}

// createEditionFromRunBook keeps the operation budget injectable so timeout
// coverage can exercise the same production path without waiting two minutes.
func (s *MultiUserService) createEditionFromRunBook(ctx context.Context, profileID, runID, bookID string, edits EditionEdits, timeout time.Duration) (*EditionCreated, error) {
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

	// Detach from a disconnected HTTP caller, but start the single operation
	// budget before any upstream call so ABS, resolution, duplicate lookup and
	// insert all share the same deadline.
	createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	item, err := s.fetchEditionItem(createCtx, target.profile, bookID)
	if err != nil {
		if createCtx.Err() != nil && ctx.Err() == nil {
			return nil, &EditionUpstreamError{Service: "audiobookshelf", Err: err}
		}
		return nil, err
	}
	if !hasEditionIdentifier(item) {
		return nil, ErrEditionNoIdentifier
	}

	input := &edition.EditionInput{
		BookID:   target.hardcoverBookID,
		Title:    edits.Title,
		Subtitle: edits.Subtitle,
		// Cover upload remains disabled because Hardcover's upload endpoint is
		// undocumented and rejects scoped API tokens. Do not ask the creator to
		// produce a warning for a flow the service intentionally does not attempt.
		ImageURL:    "",
		ISBN10:      edits.ISBN10,
		ISBN13:      edits.ISBN13,
		ASIN:        edits.ASIN,
		ReleaseDate: edits.ReleaseDate,
		EditionInfo: edits.EditionInformation,
		// Edition format, reading format and audio length are server-derived.
		EditionFormat: derivedEditionFormat(item),
		ReadingFormat: item.ReadingFormat(),
		AudioLength:   derivedEditionAudioSeconds(item),
		LanguageID:    1,
		CountryID:     1,
	}
	if err := validateEditionScalars(input); err != nil {
		return nil, &EditionValidationError{Err: err}
	}
	hcClient := s.newHardcoverClient(target.profile.HardcoverToken, profileID)
	metadataWarnings, err := resolveEditionMetadata(createCtx, hcClient, item, input)
	if err != nil {
		return nil, err
	}
	if err := validateEditionInput(input); err != nil {
		return nil, &EditionValidationError{Err: err}
	}

	dryRun := target.profile.SyncConfig.DryRun
	// Dry run is enforced at both the creator and the concrete client boundary.
	hcClient.SetDryRun(dryRun)
	// Cover upload is disabled, so the creator does not need the Audiobookshelf
	// token. Keeping it out of this boundary prevents accidental disclosure if
	// the creator gains another outbound request path.
	creator := s.editionCreator(hcClient, dryRun, "")

	result, err := creator.CreateEdition(createCtx, input)
	if err != nil {
		if errors.Is(err, edition.ErrEditionBelongsToOtherBook) {
			return nil, ErrEditionConflict
		}
		if _, ok := hardcover.InsufficientScope(err); ok {
			return nil, ErrEditionInsufficientScope
		}
		return nil, &EditionUpstreamError{Service: "hardcover", Err: err}
	}
	warnings := append(metadataWarnings, invalidISBNWarnings(input)...)
	created := &EditionCreated{EditionID: result.EditionID, DryRun: dryRun, Warnings: warnings}
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

// resolveEditionMetadata maps exact ABS names to profile-token-scoped Hardcover
// IDs. It deliberately uses no global name cache: a result must be confirmed
// through this profile's rate-limited client at create time.
func resolveEditionMetadata(ctx context.Context, client *hardcover.Client, item *models.AudiobookshelfBook, input *edition.EditionInput) ([]string, error) {
	meta := item.Media.Metadata
	authorNames := expandedAuthorNames(meta.Authors)
	if len(authorNames) == 0 && strings.TrimSpace(meta.AuthorName) != "" {
		// The legacy field is treated as one exact name. Commas may be part of
		// an author's name and are never used as a splitting rule here.
		authorNames = []string{strings.TrimSpace(meta.AuthorName)}
	}
	if len(authorNames) > maxEditionPeople {
		return nil, &EditionValidationError{Err: fmt.Errorf("at most %d authors are allowed", maxEditionPeople)}
	}
	for _, name := range authorNames {
		id, found, err := exactPersonID(ctx, client, name, false)
		if err != nil {
			return nil, &EditionUpstreamError{Service: "hardcover", Err: err}
		}
		if found {
			input.AuthorIDs = appendUniqueID(input.AuthorIDs, id)
		}
	}
	if len(input.AuthorIDs) == 0 {
		return nil, &EditionValidationError{Err: ErrEditionAuthorNotFound}
	}

	warnings := make([]string, 0, 2)
	if item.ReadingFormat() != models.ReadingFormatEbook {
		narratorNames := expandedNarratorNames(meta.Narrators)
		if len(narratorNames) == 0 && strings.TrimSpace(meta.NarratorName) != "" {
			narratorNames = []string{strings.TrimSpace(meta.NarratorName)}
		}
		if len(narratorNames) > maxEditionPeople {
			return nil, &EditionValidationError{Err: fmt.Errorf("at most %d narrators are allowed", maxEditionPeople)}
		}
		missedNarrator := false
		for _, name := range narratorNames {
			id, found, err := exactPersonID(ctx, client, name, true)
			if err != nil {
				return nil, &EditionUpstreamError{Service: "hardcover", Err: err}
			}
			if found {
				input.NarratorIDs = appendUniqueID(input.NarratorIDs, id)
			} else {
				missedNarrator = true
			}
		}
		if missedNarrator {
			warnings = append(warnings, "One or more Audiobookshelf narrators did not match a Hardcover narrator and were omitted.")
		}
	}

	publisherName := strings.TrimSpace(meta.Publisher)
	if publisherName != "" {
		publishers, err := client.SearchPublishers(ctx, publisherName, editionLookupLimit)
		if err != nil {
			return nil, &EditionUpstreamError{Service: "hardcover", Err: err}
		}
		matched := false
		for _, publisher := range publishers {
			if publisher.Name != publisherName {
				continue
			}
			id, parseErr := strconv.Atoi(publisher.ID)
			if parseErr != nil || id <= 0 {
				return nil, &EditionUpstreamError{Service: "hardcover", Err: fmt.Errorf("invalid publisher id for exact match")}
			}
			input.PublisherID = id
			matched = true
			break
		}
		if !matched {
			warnings = append(warnings, "Audiobookshelf publisher did not match a Hardcover publisher and was omitted.")
		}
	}
	return warnings, nil
}

func expandedAuthorNames(authors []models.AudiobookshelfPerson) []string {
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		if name := strings.TrimSpace(author.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func expandedNarratorNames(narrators []string) []string {
	names := make([]string, 0, len(narrators))
	for _, narrator := range narrators {
		if name := strings.TrimSpace(narrator); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func exactPersonID(ctx context.Context, client *hardcover.Client, name string, narrator bool) (int, bool, error) {
	if name == "" {
		return 0, false, nil
	}
	var people []models.Author
	var err error
	if narrator {
		people, err = client.SearchNarratorsByName(ctx, name, editionLookupLimit)
	} else {
		people, err = client.SearchAuthors(ctx, name, editionLookupLimit)
	}
	if err != nil {
		return 0, false, err
	}
	for _, person := range people {
		if person.Name != name {
			continue
		}
		id, parseErr := strconv.Atoi(person.ID)
		if parseErr != nil || id <= 0 {
			return 0, false, errors.New("Hardcover returned an invalid person id")
		}
		return id, true, nil
	}
	return 0, false, nil
}

func appendUniqueID(ids []int, id int) []int {
	for _, current := range ids {
		if current == id {
			return ids
		}
	}
	return append(ids, id)
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

// hasEditionIdentifier reports whether the Audiobookshelf item carries an ASIN
// or a well-formed ISBN. Without one a sync can never match an edition created
// for the book, so no edition is drafted or created.
func hasEditionIdentifier(item *models.AudiobookshelfBook) bool {
	meta := item.Media.Metadata
	if strings.TrimSpace(meta.ASIN) != "" {
		return true
	}
	_, ok := isbn.Parse(meta.ISBN)
	return ok
}

// fetchEditionItem reads the current Audiobookshelf item for bookID.
func (s *MultiUserService) fetchEditionItem(ctx context.Context, profile *database.ProfileWithTokens, bookID string) (*models.AudiobookshelfBook, error) {
	absClient := audiobookshelf.NewClient(profile.AudiobookshelfURL, profile.AudiobookshelfToken)
	item, err := absClient.GetLibraryItem(ctx, bookID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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
	if err := validateEditionScalars(input); err != nil {
		return err
	}
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
	if input.PublisherID < 0 || input.LanguageID < 0 || input.CountryID < 0 || input.AudioLength < 0 {
		return errors.New("publisher, language, country, and audio length must not be negative")
	}
	return nil
}

func validateEditionScalars(input *edition.EditionInput) error {
	input.Title = strings.TrimSpace(input.Title)
	if input.BookID <= 0 {
		return errors.New("book_id is required")
	}
	if input.Title == "" {
		return errors.New("title is required")
	}
	switch strings.ToLower(strings.TrimSpace(input.ReadingFormat)) {
	case models.ReadingFormatAudiobook, models.ReadingFormatEbook:
	default:
		return errors.New("invalid server-derived reading format")
	}
	if input.ReleaseDate != "" {
		if _, err := time.Parse("2006-01-02", input.ReleaseDate); err != nil {
			return errors.New("invalid release_date format, expected YYYY-MM-DD")
		}
	}
	return normalizeEditionIdentifiers(input)
}

func derivedEditionFormat(item *models.AudiobookshelfBook) string {
	if item.ReadingFormat() == models.ReadingFormatEbook {
		return "Ebook"
	}
	if strings.TrimSpace(item.Media.Metadata.ASIN) != "" {
		return "Audible Audio"
	}
	if strings.Contains(strings.ToLower(item.Media.Metadata.Publisher), "libro") {
		return "libro.fm"
	}
	return ""
}

func derivedEditionAudioSeconds(item *models.AudiobookshelfBook) int {
	if item.ReadingFormat() == models.ReadingFormatEbook || item.Media.Duration <= 0 {
		return 0
	}
	return int(item.Media.Duration + 0.5)
}

func invalidISBNWarnings(input *edition.EditionInput) []string {
	warnings := make([]string, 0, 2)
	if input.ISBN10 != "" {
		parsed, _ := isbn.Parse(input.ISBN10)
		if !parsed.Valid {
			warnings = append(warnings, "ISBN-10 has an invalid check digit.")
		}
	}
	if input.ISBN13 != "" {
		parsed, _ := isbn.Parse(input.ISBN13)
		if !parsed.Valid {
			warnings = append(warnings, "ISBN-13 has an invalid check digit.")
		}
	}
	return warnings
}

// normalizeEditionIdentifiers trims the ASIN and rewrites the ISBNs without
// hyphens or spaces, so a caller may send them either way. A value that is not
// the right shape for its field is an error, and at least one identifier is
// required: an edition without one could never be matched by a sync.
func normalizeEditionIdentifiers(input *edition.EditionInput) error {
	input.ASIN = strings.TrimSpace(input.ASIN)
	var err error
	if input.ISBN10, err = normalizeISBN(input.ISBN10, false, "isbn_10 must be a valid 10-character ISBN"); err != nil {
		return err
	}
	if input.ISBN13, err = normalizeISBN(input.ISBN13, true, "isbn_13 must be a valid 13-digit ISBN"); err != nil {
		return err
	}
	if input.ASIN == "" && input.ISBN10 == "" && input.ISBN13 == "" {
		return errors.New("an ASIN or ISBN is required")
	}
	return nil
}

// normalizeISBN returns value without separators, "" for a blank value, or an
// error with message when it is not an ISBN of the wanted length.
func normalizeISBN(value string, want13 bool, message string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	parsed, ok := isbn.Parse(value)
	if !ok || parsed.Is13 != want13 {
		return "", errors.New(message)
	}
	return parsed.Given, nil
}
