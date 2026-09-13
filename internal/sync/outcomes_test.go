package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/mismatch"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProcessBookRecordsExclusiveNotFoundOutcome(t *testing.T) {
	svc, _ := createTestService()
	svc.config.Sync.ProcessUnreadBooks = true
	book := toAudiobookshelfBook(createTestBook("missing-book", "", "", "", ""))
	book.Progress.CurrentTime = 600

	require.NoError(t, svc.processBook(context.Background(), *book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.NotFound)
	assert.Equal(t, int32(0), snapshot.OutcomeCounts.NeedsReview)
	assert.Equal(t, snapshot.ProcessedSoFar, snapshot.OutcomeCounts.Total())
	require.Len(t, snapshot.AttentionRecords, 1)
	assert.Equal(t, OutcomeNotFound, snapshot.AttentionRecords[0].Outcome)
	assert.Empty(t, snapshot.Mismatches)
}

func TestLiveMismatchStateIsScopedToEachService(t *testing.T) {
	first, _ := createTestService()
	second, _ := createTestService()
	first.beginOutcomeRun()
	second.beginOutcomeRun()

	book := *toAudiobookshelfBook(createTestBook("profile-one-book", "Profile One", "Author", "", ""))
	first.recordBookOutcome(book, OutcomeNeedsReview, "possible match", nil, nil)

	firstSnapshot := first.GetSnapshot()
	secondSnapshot := second.GetSnapshot()
	require.Len(t, firstSnapshot.Mismatches, 1)
	assert.Equal(t, book.ID, firstSnapshot.Mismatches[0].BookID)
	assert.Empty(t, secondSnapshot.Mismatches)
}

func TestProcessBookFailsBeforeMutationWhenReadStatusLookupFails(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.ProcessUnreadBooks = true
	svc.config.Sync.SyncOwned = false
	book := *toAudiobookshelfBook(createTestBook("read-status-failure", "Read Status Failure", "Author", "READ-ERROR-ASIN", ""))
	book.Progress.CurrentTime = 120
	userBookID := int64(321)
	mockClient.On("SearchBookByASIN", mock.Anything, "READ-ERROR-ASIN").Return(&models.HardcoverBook{
		ID:        "hardcover-book",
		EditionID: "456",
	}, nil).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID: "456", BookID: "123",
	}, nil).Times(3)
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(int(userBookID), nil).Times(3)

	mockClient.On("GetUserBook", mock.Anything, "321").Return(&models.HardcoverBook{
		ID:           "hardcover-book",
		BookStatusID: 2,
	}, nil).Once()
	mockClient.On("GetUserBookReads", mock.Anything, hardcover.GetUserBookReadsInput{
		UserBookID: userBookID,
	}).Return([]hardcover.UserBookRead(nil), errors.New("read API unavailable")).Once()

	err := svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{})

	require.Error(t, err)
	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeFailed, snapshot.BookOutcomes[0].Outcome)
	mockClient.AssertNotCalled(t, "InsertUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookStatus", mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProcessBookRecordsFailedOutcomeWhenFinishedStatusLookupFails(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.ProcessUnreadBooks = true
	svc.config.Sync.SyncOwned = false
	book := *toAudiobookshelfBook(createTestFinishedBook(
		"finished-status-lookup-failure", "Finished Status Lookup Failure", "Author", "FINISHED-STATUS-ERROR", ""))
	userBookID := int64(322)

	mockClient.On("SearchBookByASIN", mock.Anything, "FINISHED-STATUS-ERROR").Return(&models.HardcoverBook{
		ID:        "hardcover-book",
		EditionID: "456",
	}, nil).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID: "456", BookID: "123",
	}, nil).Times(3)
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(int(userBookID), nil).Times(3)
	mockClient.On("GetUserBook", mock.Anything, "322").Return((*models.HardcoverBook)(nil), errors.New("status API unavailable")).Once()

	err := svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get current book status")
	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeFailed, snapshot.BookOutcomes[0].Outcome)
	assert.Contains(t, snapshot.BookOutcomes[0].Error, "status API unavailable")
	_, exists := svc.state.GetBookState(book.ID + ":456")
	assert.False(t, exists, "a failed finished-book lookup must not advance sync state")
	mockClient.AssertNotCalled(t, "GetUserBookReads", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "InsertUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookStatus", mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProcessBookClassifiesLookupFailureSeparatelyFromNotFound(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.ProcessUnreadBooks = true
	book := toAudiobookshelfBook(createTestBook("failed-book", "Failed Book", "Test Author", "FAILED-ASIN", ""))
	book.Progress.CurrentTime = 600
	mockClient.On("SearchBookByASIN", mock.Anything, "FAILED-ASIN").Return(nil, errors.New("request timed out")).Once()
	mockClient.On("SearchBooks", mock.Anything, "Failed Book Test Author", "").Return([]models.HardcoverBook{}, nil).Once()

	require.NoError(t, svc.processBook(context.Background(), *book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.Failed)
	assert.Equal(t, int32(0), snapshot.OutcomeCounts.NotFound)
	assert.Contains(t, snapshot.BookOutcomes[0].Error, "request timed out")
	assert.Empty(t, snapshot.BooksNotFound, "a lookup failure must not appear as a conclusive missing book")
	mockClient.AssertExpectations(t)
}

func TestTitleOnlyMismatchPublishesRichLegacyDetailsWithoutRecount(t *testing.T) {
	svc, _ := createTestService()
	svc.beginOutcomeRun()
	book := *toAudiobookshelfBook(createTestBook("rich-mismatch", "ABS title", "ABS author", "", ""))
	svc.recordBookOutcome(book, OutcomeNeedsReview, "title-only candidate", nil, &models.HardcoverBook{ID: "hc-book"})

	svc.enrichLiveMismatch(mismatch.BookMismatch{
		BookID:                 book.ID,
		Title:                  book.Media.Metadata.Title,
		Author:                 book.Media.Metadata.AuthorName,
		HardcoverBookID:        "hc-book",
		HardcoverTitle:         "Hardcover title",
		HardcoverAuthor:        "Hardcover author",
		HardcoverPublishedYear: "2024",
		HardcoverCoverURL:      "https://example.test/cover",
		HardcoverSlug:          "hardcover-slug",
		Reason:                 "title-only candidate",
	})

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.Mismatches, 1)
	assert.Equal(t, "Hardcover title", snapshot.Mismatches[0].HardcoverTitle)
	assert.Equal(t, "Hardcover author", snapshot.Mismatches[0].HardcoverAuthor)
	assert.Equal(t, "2024", snapshot.Mismatches[0].HardcoverPublishedYear)
	assert.Equal(t, "https://example.test/cover", snapshot.Mismatches[0].HardcoverCoverURL)
	assert.Equal(t, "hardcover-slug", snapshot.Mismatches[0].HardcoverSlug)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.NeedsReview)

	// The deferred processBook finalizer can publish the same outcome again;
	// this must preserve the rich local details and keyed count.
	svc.recordBookOutcome(book, OutcomeNeedsReview, "title-only candidate", nil, nil)
	snapshot = svc.GetSnapshot()
	require.Len(t, snapshot.Mismatches, 1)
	assert.Equal(t, "Hardcover title", snapshot.Mismatches[0].HardcoverTitle)
	assert.Equal(t, "hardcover-slug", snapshot.Mismatches[0].HardcoverSlug)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.NeedsReview)
}

func TestProcessBookTitleOnlyMismatchKeepsRichLegacyDetails(t *testing.T) {
	svc, mockClient := createTestService()
	book := *toAudiobookshelfBook(createTestBook("title-only-rich", "ABS title", "ABS author", "", ""))
	candidate := models.HardcoverBook{
		ID:            "hc-rich",
		Title:         "Hardcover title",
		Authors:       []models.Author{{Name: "Hardcover author"}},
		ReleaseDate:   "2024-03-04",
		CoverImageURL: "https://example.test/cover",
		Slug:          "hardcover-slug",
	}

	// The initial lookup and the title-only enrichment lookup both search, and
	// each title search hydrates the selected candidate. The mismatch exporter
	// may fetch the candidate once more, which is harmless to the live store.
	mockClient.On("SearchBooks", mock.Anything, "ABS title ABS author", "").Return([]models.HardcoverBook{candidate}, nil).Twice()
	mockClient.On("SearchBooks", mock.Anything, "ABS title", "ABS author").Return([]models.HardcoverBook{candidate}, nil).Once()
	mockClient.On("GetBookByID", mock.Anything, "hc-rich").Return(&candidate, nil).Maybe()
	mismatch.Clear()
	defer mismatch.Clear()

	require.NoError(t, svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{}))
	snapshot := svc.GetSnapshot()

	require.Len(t, snapshot.Mismatches, 1)
	assert.Equal(t, OutcomeNeedsReview, snapshot.BookOutcomes[0].Outcome)
	assert.Equal(t, "hc-rich", snapshot.Mismatches[0].HardcoverBookID)
	assert.Equal(t, "Hardcover title", snapshot.Mismatches[0].HardcoverTitle)
	assert.Equal(t, "Hardcover author", snapshot.Mismatches[0].HardcoverAuthor)
	assert.Equal(t, "2024", snapshot.Mismatches[0].HardcoverPublishedYear)
	assert.Equal(t, "https://example.test/cover", snapshot.Mismatches[0].HardcoverCoverURL)
	assert.Equal(t, "hardcover-slug", snapshot.Mismatches[0].HardcoverSlug)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.NeedsReview)
	mockClient.AssertExpectations(t)
}

func TestHandleInProgressBookReconcilesStaleStatusWithMatchingFinishedRead(t *testing.T) {
	svc, mockClient := createTestService()
	book := toAudiobookshelfBook(createTestFinishedBook(
		"stale-finished-status", "Stale status", "Test Author", "", ""))
	userBookID := int64(142)
	editionID := int64(472)
	finishedAt := time.Unix(book.Progress.FinishedAt/1000, 0).Format("2006-01-02")
	progressSeconds := int(book.Media.Duration)
	readID := int64(802)

	mockClient.On("GetUserBook", mock.Anything, "142").Return(&models.HardcoverBook{
		ID: "hc-stale-status", EditionID: "472", BookStatusID: 2,
	}, nil).Once()
	mockClient.On("GetUserBookReads", mock.Anything, hardcover.GetUserBookReadsInput{
		UserBookID: userBookID,
	}).Return([]hardcover.UserBookRead{{
		ID: readID, ProgressSeconds: &progressSeconds, EditionID: &editionID,
		FinishedAt: &finishedAt,
	}}, nil).Once()
	mockClient.On("UpdateUserBookStatus", mock.Anything, hardcover.UpdateUserBookStatusInput{
		ID: userBookID, StatusID: 3,
	}).Return(nil).Once()
	// Status reconciliation performs blank-read cleanup using a fresh read list.
	mockClient.On("GetUserBookReads", mock.Anything, hardcover.GetUserBookReadsInput{
		UserBookID: userBookID,
	}).Return([]hardcover.UserBookRead{{
		ID: readID, ProgressSeconds: &progressSeconds, EditionID: &editionID,
		FinishedAt: &finishedAt,
	}}, nil).Once()

	var gotOutcome SyncOutcome
	ctx := context.WithValue(context.Background(), processBookOutcomeReporterKey{}, processBookOutcomeReporter(func(outcome SyncOutcome, _ string) {
		gotOutcome = outcome
	}))
	err := svc.handleInProgressBook(ctx, userBookID, *book, fmt.Sprintf("%s:%d", book.ID, editionID))

	require.NoError(t, err)
	assert.Equal(t, OutcomeSynced, gotOutcome)
	mockClient.AssertNotCalled(t, "UpdateUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestHandleInProgressBookFailsWhenFinishedReadStatusIsUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name     string
		userBook *models.HardcoverBook
		readID   int64
	}{
		{name: "nil user book", userBook: nil, readID: 803},
		{name: "zero status", userBook: &models.HardcoverBook{ID: "hc-zero-status", EditionID: "473", BookStatusID: 0}, readID: 804},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, mockClient := createTestService()
			book := toAudiobookshelfBook(createTestFinishedBook(
				"unavailable-finished-status-"+tt.name, "Unavailable status", "Test Author", "", ""))
			userBookID := int64(143)
			editionID := int64(473)
			finishedAt := time.Unix(book.Progress.FinishedAt/1000, 0).Format("2006-01-02")
			progressSeconds := int(book.Media.Duration)

			mockClient.On("GetUserBook", mock.Anything, "143").Return(tt.userBook, nil).Once()
			mockClient.On("GetUserBookReads", mock.Anything, hardcover.GetUserBookReadsInput{
				UserBookID: userBookID,
			}).Return([]hardcover.UserBookRead{{
				ID: tt.readID, ProgressSeconds: &progressSeconds,
				EditionID: &editionID, FinishedAt: &finishedAt,
			}}, nil).Once()

			var gotOutcome SyncOutcome
			ctx := context.WithValue(context.Background(), processBookOutcomeReporterKey{}, processBookOutcomeReporter(func(outcome SyncOutcome, _ string) {
				gotOutcome = outcome
			}))
			err := svc.handleInProgressBook(ctx, userBookID, *book, fmt.Sprintf("%s:%d", book.ID, editionID))

			require.NoError(t, err)
			assert.Equal(t, OutcomeFailed, gotOutcome)
			assert.True(t, svc.state.NeedsSync(book.ID+":"+fmt.Sprint(editionID), 1, "FINISHED", 0.01))
			mockClient.AssertNotCalled(t, "UpdateUserBookStatus", mock.Anything, mock.Anything)
			mockClient.AssertNotCalled(t, "UpdateUserBookRead", mock.Anything, mock.Anything)
			mockClient.AssertExpectations(t)
		})
	}
}

func TestProcessBookRecordsDryRunWouldSyncOutcome(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.ProcessUnreadBooks = true
	svc.config.Sync.SyncOwned = false
	book := toAudiobookshelfBook(createTestBook("dry-run-book", "Dry Run Book", "Test Author", "DRYRUN-ASIN", ""))
	book.Progress.CurrentTime = 600
	mockClient.On("SearchBookByASIN", mock.Anything, "DRYRUN-ASIN").Return(&models.HardcoverBook{
		ID:        "123",
		EditionID: "456",
	}, nil).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID:     "456",
		BookID: "123",
	}, nil).Times(3)
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(789, nil).Times(3)
	mockClient.On("GetUserBook", mock.Anything, "789").Return(&models.HardcoverBook{
		ID:           "123",
		EditionID:    "456",
		BookStatusID: 2,
	}, nil).Once()
	mockClient.On("GetUserBookReads", mock.Anything, mock.Anything).Return([]hardcover.UserBookRead{}, nil).Once()

	require.NoError(t, svc.processBook(context.Background(), *book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeWouldSync, snapshot.BookOutcomes[0].Outcome)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.WouldSync)
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	mockClient.AssertExpectations(t)
}

func TestProcessBookDryRunNewUserBookWouldSyncWithoutMutation(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.ProcessUnreadBooks = true
	svc.config.Sync.SyncOwned = false
	book := *toAudiobookshelfBook(createTestFinishedBook(
		"dry-run-new-user-book", "Dry Run New User Book", "Author", "DRYRUN-NEW", ""))

	mockClient.On("SearchBookByASIN", mock.Anything, "DRYRUN-NEW").Return(&models.HardcoverBook{
		ID:        "123",
		EditionID: "456",
	}, nil).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID: "456", BookID: "123",
	}, nil).Maybe()
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(0, nil).Maybe()

	require.NoError(t, svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeWouldSync, snapshot.BookOutcomes[0].Outcome)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.WouldSync)
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	_, exists := svc.state.GetBookState(book.ID + ":456")
	assert.False(t, exists, "dry-run planned creation must not advance sync state")
	mockClient.AssertNotCalled(t, "GetUserBook", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "GetUserBookReads", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "CreateUserBook", mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "MarkEditionAsOwned", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "InsertUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookRead", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateUserBookStatus", mock.Anything, mock.Anything)
	mockClient.AssertNotCalled(t, "UpdateReadingProgress", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProcessBookRecordsAlreadyCurrentForNoChangeDryRun(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.Incremental = true
	svc.config.Sync.ProcessUnreadBooks = true
	book := toAudiobookshelfBook(createTestBook("current-book", "Current Book", "Test Author", "CURRENT-ASIN", ""))
	svc.state.UpdateBook(book.ID, 0, "WANT_TO_READ")
	svc.state.SetHasProgressSeconds(book.ID)

	require.NoError(t, svc.processBook(context.Background(), *book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeAlreadyCurrent, snapshot.BookOutcomes[0].Outcome)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.AlreadyCurrent)
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	mockClient.AssertNotCalled(t, "SearchBookByASIN", mock.Anything, mock.Anything)
}

func TestProcessBookRecordsAlreadyCurrentForUnchangedCompositeState(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.Incremental = true
	svc.config.Sync.ProcessUnreadBooks = true
	book := *toAudiobookshelfBook(createTestBook("composite-current", "Composite Current", "Test Author", "COMPOSITE-ASIN", ""))
	book.Progress.CurrentTime = 720
	svc.state.UpdateBook("composite-current:456", 0.2, "IN_PROGRESS")

	mockClient.On("SearchBookByASIN", mock.Anything, "COMPOSITE-ASIN").Return(&models.HardcoverBook{
		ID:        "123",
		EditionID: "456",
	}, nil).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID: "456", BookID: "123",
	}, nil).Once()
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(321, nil).Once()

	require.NoError(t, svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeAlreadyCurrent, snapshot.BookOutcomes[0].Outcome)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.AlreadyCurrent)
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	mockClient.AssertNotCalled(t, "GetUserBook", mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProcessBookOwnershipMutationAffectsFinalOutcome(t *testing.T) {
	tests := []struct {
		name            string
		dryRun          bool
		incremental     bool
		minimumProgress float64
		ownershipErr    error
		expectedOutcome SyncOutcome
		expectMarkCall  bool
	}{
		{
			name:            "current item becomes synced after ownership write",
			incremental:     true,
			minimumProgress: 0.01,
			expectedOutcome: OutcomeSynced,
			expectMarkCall:  true,
		},
		{
			name:            "threshold skip becomes synced after ownership write",
			minimumProgress: 0.5,
			expectedOutcome: OutcomeSynced,
			expectMarkCall:  true,
		},
		{
			name:            "dry-run threshold plan becomes would sync",
			dryRun:          true,
			minimumProgress: 0.5,
			expectedOutcome: OutcomeWouldSync,
		},
		{
			name:            "ownership failure becomes failed",
			minimumProgress: 0.5,
			ownershipErr:    errors.New("ownership write failed"),
			expectedOutcome: OutcomeFailed,
			expectMarkCall:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mockClient := createTestService()
			svc.config.Sync.SyncOwned = true
			svc.config.Sync.DryRun = tt.dryRun
			svc.config.Sync.Incremental = tt.incremental
			svc.config.Sync.MinimumProgress = tt.minimumProgress
			book := *toAudiobookshelfBook(createTestBook("ownership-"+tt.name, "Ownership Book", "Author", "", "OWNERSHIP-ISBN"))
			book.Progress.CurrentTime = 720

			if tt.incremental {
				svc.state.UpdateBook(book.ID+":456", 0.2, "IN_PROGRESS")
			}
			mockClient.On("SearchBookByISBN13", mock.Anything, "OWNERSHIP-ISBN").Return(&models.HardcoverBook{
				ID: "123", EditionID: "456",
			}, nil).Once()
			mockClient.On("CheckBookOwnership", mock.Anything, 123).Return(false, nil).Once()
			if tt.expectMarkCall {
				mockClient.On("MarkEditionAsOwned", mock.Anything, 456).Return(tt.ownershipErr).Once()
			}
			mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
				ID: "456", BookID: "123",
			}, nil).Once()
			mockClient.On("GetUserBookID", mock.Anything, 456).Return(789, nil).Once()

			err := svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{})
			if tt.ownershipErr != nil {
				assert.NoError(t, err)
			} else {
				assert.NoError(t, err)
			}
			snapshot := svc.GetSnapshot()
			require.Len(t, snapshot.BookOutcomes, 1)
			assert.Equal(t, tt.expectedOutcome, snapshot.BookOutcomes[0].Outcome)
			if tt.dryRun {
				mockClient.AssertNotCalled(t, "MarkEditionAsOwned", mock.Anything, mock.Anything)
			}
			mockClient.AssertExpectations(t)
		})
	}
}

func TestProcessBookOwnershipCheckFailureStaysFailedOnCurrentPath(t *testing.T) {
	svc, mockClient := createTestService()
	svc.config.Sync.SyncOwned = true
	svc.config.Sync.Incremental = true
	svc.config.Sync.ProcessUnreadBooks = true
	book := *toAudiobookshelfBook(createTestBook("ownership-check-failure", "Ownership Check Failure", "Author", "", "OWNERSHIP-CHECK-ISBN"))
	book.Progress.CurrentTime = 720
	svc.state.UpdateBook(book.ID+":456", 0.2, "IN_PROGRESS")

	mockClient.On("SearchBookByISBN13", mock.Anything, "OWNERSHIP-CHECK-ISBN").Return(&models.HardcoverBook{
		ID: "123", EditionID: "456",
	}, nil).Once()
	mockClient.On("CheckBookOwnership", mock.Anything, 123).Return(false, errors.New("ownership API unavailable")).Once()
	mockClient.On("GetEdition", mock.Anything, "456").Return(&models.Edition{
		ID: "456", BookID: "123",
	}, nil).Once()
	mockClient.On("GetUserBookID", mock.Anything, 456).Return(789, nil).Once()

	require.NoError(t, svc.processBook(context.Background(), book, &models.AudiobookshelfUserProgress{}))

	snapshot := svc.GetSnapshot()
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, OutcomeFailed, snapshot.BookOutcomes[0].Outcome)
	assert.Contains(t, snapshot.BookOutcomes[0].Error, "ownership API unavailable")
	mockClient.AssertNotCalled(t, "MarkEditionAsOwned", mock.Anything, mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestOutcomeStoreUpsertsEnrichmentWithoutRecount(t *testing.T) {
	svc, _ := createTestService()
	svc.beginOutcomeRun()
	book := *toAudiobookshelfBook(createTestBook("enriched-book", "Original title", "Author", "ASIN", "ISBN"))

	svc.recordBookOutcome(book, OutcomeNeedsReview, "title-only candidate", nil, nil)
	require.True(t, svc.EnrichOutcome(book.ID, BookOutcomeRecord{
		Title:           "Enriched title",
		HardcoverBookID: "123",
		EditionID:       "456",
	}))

	snapshot := svc.GetSnapshot()
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.NeedsReview)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.Total())
	require.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, "Enriched title", snapshot.BookOutcomes[0].Title)
	assert.Equal(t, "123", snapshot.BookOutcomes[0].HardcoverBookID)
	assert.Equal(t, "456", snapshot.BookOutcomes[0].EditionID)
	require.Len(t, snapshot.Mismatches, 1)
	assert.Equal(t, "Enriched title", snapshot.Mismatches[0].HardcoverTitle)
	assert.Equal(t, "123", snapshot.Mismatches[0].HardcoverBookID)
}

func TestGetSnapshotDeepCopiesOutcomeDetails(t *testing.T) {
	svc, _ := createTestService()
	svc.beginOutcomeRun()
	book := *toAudiobookshelfBook(createTestBook("copy-book", "Stable title", "Author", "", ""))
	svc.recordBookOutcome(book, OutcomeSkipped, "filter", nil, nil)

	first := svc.GetSnapshot()
	require.Len(t, first.BookOutcomes, 1)
	first.BookOutcomes[0].Title = "caller mutation"
	first.AttentionRecords = append(first.AttentionRecords, BookOutcomeRecord{BookID: "caller-only"})

	second := svc.GetSnapshot()
	require.Len(t, second.BookOutcomes, 1)
	assert.Equal(t, "Stable title", second.BookOutcomes[0].Title)
	assert.Empty(t, second.AttentionRecords)
	assert.NotEmpty(t, second.RunID)
	assert.False(t, second.RunStartedAt.IsZero())
}
