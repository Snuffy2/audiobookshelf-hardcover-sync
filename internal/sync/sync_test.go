package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/config"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/models"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// AudiobookshelfClientInterface defines the interface for audiobookshelf.Client
// to allow for mocking in tests
type AudiobookshelfClientInterface interface {
	GetLibraries(ctx context.Context) ([]audiobookshelf.AudiobookshelfLibrary, error)
	GetLibraryItems(ctx context.Context, libraryID string) ([]models.AudiobookshelfBook, error)
	GetUserProgress(ctx context.Context) (*models.AudiobookshelfUserProgress, error)
	GetListeningSessions(ctx context.Context, since time.Time) ([]models.AudiobookshelfBook, error)
}

// MockAudiobookshelfClient is a mock implementation of the AudiobookshelfClientInterface
type MockAudiobookshelfClient struct {
	mock.Mock
}

// GetLibraries mocks the GetLibraries method
func (m *MockAudiobookshelfClient) GetLibraries(ctx context.Context) ([]audiobookshelf.AudiobookshelfLibrary, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]audiobookshelf.AudiobookshelfLibrary), args.Error(1)
}

// GetLibraryItems mocks the GetLibraryItems method
func (m *MockAudiobookshelfClient) GetLibraryItems(ctx context.Context, libraryID string) ([]models.AudiobookshelfBook, error) {
	args := m.Called(ctx, libraryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.AudiobookshelfBook), args.Error(1)
}

// GetUserProgress mocks the GetUserProgress method
func (m *MockAudiobookshelfClient) GetUserProgress(ctx context.Context) (*models.AudiobookshelfUserProgress, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AudiobookshelfUserProgress), args.Error(1)
}

// GetListeningSessions mocks the GetListeningSessions method
func (m *MockAudiobookshelfClient) GetListeningSessions(ctx context.Context, since time.Time) ([]models.AudiobookshelfBook, error) {
	args := m.Called(ctx, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]models.AudiobookshelfBook), args.Error(1)
}

// TestSync tests the Sync function
func TestSync(t *testing.T) {
	// Skip this test for now until all struct issues are fixed
	// Previously skipped until struct issues were fixed

	// Setup logger with test config
	logger.Setup(logger.Config{
		Level:  "debug",
		Format: "json",
	})
	log := logger.Get()

	// Setup mocks
	mockABS := new(MockAudiobookshelfClient)
	mockHC := new(MockHardcoverClient)
	
	// Create a temporary state file
	testState := state.NewState()
	
	// Create test config with default values and update sync settings
	testConfig := config.DefaultConfig()
	
	// Configure sync settings - all sync-related settings are now consolidated under Sync
	testConfig.Sync.Incremental = false
	testConfig.Sync.StateFile = "/tmp/sync_state_test.json"
	testConfig.Sync.MinChangeThreshold = 60
	testConfig.Sync.SyncInterval = 1 * time.Hour
	testConfig.Sync.MinimumProgress = 0.05
	testConfig.Sync.SyncWantToRead = true
	testConfig.Sync.SyncOwned = true
	testConfig.Sync.DryRun = true
	
	// Initialize libraries include/exclude
	testConfig.Sync.Libraries.Include = []string{}
	testConfig.Sync.Libraries.Exclude = []string{}
	
	// Set test-specific app settings
	testConfig.App.TestBookFilter = ""
	testConfig.App.TestBookLimit = 0
	
	// Clear deprecated sync fields in App
	testConfig.App.SyncInterval = 0
	testConfig.App.MinimumProgress = 0
	testConfig.App.SyncWantToRead = false
	testConfig.App.SyncOwned = false
	testConfig.App.DryRun = false
	
	// Set test service configurations
	testConfig.Audiobookshelf.URL = "https://abs.example.com"
	testConfig.Audiobookshelf.Token = "test-token"
	testConfig.Hardcover.Token = "test-token"

	// Create the service with mocked clients
	svc := &Service{
		audiobookshelf: nil, // Will be replaced with mock
		hardcover:      mockHC,
		config:         testConfig,
		log:            log,
		state:          testState,
		statePath:      "",
		lastProgressUpdates: make(map[string]progressUpdateInfo),
		asinCache:           make(map[string]*models.HardcoverBook),
		persistentCache:     NewPersistentASINCache("/tmp"),
		userBookCache:       NewPersistentUserBookCache("/tmp"),
	}

	// We need to use a reflection trick to inject our mock into the service
	// since audiobookshelf.Client is a concrete type in the Service struct
	// For testing purposes, we'll create a function to inject the mock
	
	// Create test library for reference
	testLibrary := &audiobookshelf.AudiobookshelfLibrary{
		ID:   "lib1",
		Name: "Test Library",
	}
	
	// Create test user progress
	testUserProgress := &models.AudiobookshelfUserProgress{
		ID: "user1",
		Username: "testuser",
		MediaProgress: []struct {
			ID            string  `json:"id"`
			LibraryItemID string  `json:"libraryItemId"`
			UserID        string  `json:"userId"`
			IsFinished    bool    `json:"isFinished"`
			Progress      float64 `json:"progress"`
			CurrentTime   float64 `json:"currentTime"`
			Duration      float64 `json:"duration"`
			StartedAt     int64   `json:"startedAt"`
			FinishedAt    int64   `json:"finishedAt"`
			LastUpdate    int64   `json:"lastUpdate"`
			TimeListening float64 `json:"timeListening"`
		}{},
		ListeningSessions: []struct {
			ID            string `json:"id"`
			UserID        string `json:"userId"`
			LibraryItemID string `json:"libraryItemId"`
			MediaType     string `json:"mediaType"`
			MediaMetadata struct {
				Title  string `json:"title"`
				Author string `json:"author"`
			} `json:"mediaMetadata"`
			Duration    float64 `json:"duration"`
			CurrentTime float64 `json:"currentTime"`
			Progress    float64 `json:"progress"`
			IsFinished  bool    `json:"isFinished"`
			StartedAt   int64   `json:"startedAt"`
			UpdatedAt   int64   `json:"updatedAt"`
		}{},
	}
	
	// Create empty library items list
	emptyLibraryItems := []models.AudiobookshelfBook{}
	
	// Setup mock expectations - only include what's used in the processLibrary test
	mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return(emptyLibraryItems, nil)
	
	// Replace the audiobookshelf client in the service with our mock
	svc.audiobookshelf = mockABS
	
	// For test purposes, we'll test processLibrary directly
	t.Run("Empty library - should complete without errors", func(t *testing.T) {
		// Create a context with a timeout
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		// Call processLibrary directly
		processed, err := svc.processLibrary(ctx, testLibrary, 0, testUserProgress)
		
		// Verify results
		assert.NoError(t, err)
		assert.Equal(t, 0, processed)
		
		// Verify mock expectations
		mockABS.AssertExpectations(t)
	})
}

func TestSyncRecoversBooksTotalAfterPrecountFailure(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.ProcessUnreadBooks = false
	svc.config.Paths.MismatchOutputDir = filepath.Join(t.TempDir(), "mismatches")

	mockABS := new(MockAudiobookshelfClient)
	libraries := []audiobookshelf.AudiobookshelfLibrary{
		{ID: "precounted-library", Name: "Precounted Library"},
		{ID: "retry-library", Name: "Retry Library"},
	}
	precountedBook := *toAudiobookshelfBook(createTestBook("precounted-book", "Precounted Book", "Author", "", ""))
	retriedBook := *toAudiobookshelfBook(createTestBook("retried-book", "Retried Book", "Author", "", ""))
	precountedItems := []models.AudiobookshelfBook{precountedBook}
	retriedItems := []models.AudiobookshelfBook{retriedBook}

	mockABS.On("GetUserProgress", mock.Anything).Return(&models.AudiobookshelfUserProgress{}, nil).Once()
	mockABS.On("GetLibraries", mock.Anything).Return(libraries, nil).Once()
	// The first library succeeds during pre-count and is fetched again for
	// processing; the second library fails transiently, then succeeds during
	// processing. Both totals must be counted exactly once.
	mockABS.On("GetLibraryItems", mock.Anything, "precounted-library").Return(precountedItems, nil).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "retry-library").Return([]models.AudiobookshelfBook(nil), errors.New("temporary library error")).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "precounted-library").Return(precountedItems, nil).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "retry-library").Return(retriedItems, nil).Once()
	mockHC.On("ClearUserBookCache").Return().Once()
	svc.audiobookshelf = mockABS

	require.NoError(t, svc.Sync(context.Background()))

	snapshot := svc.GetSnapshot()
	assert.Equal(t, int32(2), snapshot.BooksTotal)
	assert.Equal(t, int32(2), snapshot.ProcessedSoFar)
	assert.Equal(t, int32(2), snapshot.OutcomeCounts.Skipped)
	assert.Equal(t, snapshot.ProcessedSoFar, snapshot.OutcomeCounts.Total())
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestSyncReturnsLibraryFetchErrorWithoutInventingBookOutcomes(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.ProcessUnreadBooks = false
	svc.config.Paths.MismatchOutputDir = filepath.Join(t.TempDir(), "mismatches")

	mockABS := new(MockAudiobookshelfClient)
	libraries := []audiobookshelf.AudiobookshelfLibrary{
		{ID: "failed-library", Name: "Failed Library"},
		{ID: "healthy-library", Name: "Healthy Library"},
	}
	healthyBook := *toAudiobookshelfBook(createTestBook("healthy-book", "Healthy Book", "Author", "", ""))
	healthyItems := []models.AudiobookshelfBook{healthyBook}
	libraryErr := errors.New("library API unavailable")

	mockABS.On("GetUserProgress", mock.Anything).Return(&models.AudiobookshelfUserProgress{}, nil).Once()
	mockABS.On("GetLibraries", mock.Anything).Return(libraries, nil).Once()
	// The failed library is unavailable during both pre-count and processing.
	// Its unseen candidates must not be fabricated as per-book failures.
	mockABS.On("GetLibraryItems", mock.Anything, "failed-library").Return([]models.AudiobookshelfBook(nil), libraryErr).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "healthy-library").Return(healthyItems, nil).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "failed-library").Return([]models.AudiobookshelfBook(nil), libraryErr).Once()
	mockABS.On("GetLibraryItems", mock.Anything, "healthy-library").Return(healthyItems, nil).Once()
	mockHC.On("ClearUserBookCache").Return().Once()
	svc.audiobookshelf = mockABS

	err := svc.Sync(context.Background())

	require.ErrorIs(t, err, libraryErr)
	snapshot := svc.GetSnapshot()
	assert.Equal(t, "failed", snapshot.State)
	assert.Equal(t, int32(1), snapshot.BooksTotal)
	assert.Equal(t, int32(1), snapshot.ProcessedSoFar)
	assert.Equal(t, int32(1), snapshot.OutcomeCounts.Skipped)
	assert.Len(t, snapshot.BookOutcomes, 1)
	assert.Equal(t, healthyBook.ID, snapshot.BookOutcomes[0].BookID)
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestSyncReturnsUserProgressFetchErrorWithoutSkippingUnseenProgress(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.DryRun = true
	svc.config.Sync.ProcessUnreadBooks = false
	svc.config.Paths.MismatchOutputDir = filepath.Join(t.TempDir(), "mismatches")

	healthyBook := *toAudiobookshelfBook(createTestBook("progress-unavailable-book", "Progress Unavailable", "Author", "", ""))
	var userProgressRequests int
	var libraryItemRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			userProgressRequests++
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "Bearer abs-token", r.Header.Get("Authorization"))
			http.Error(w, "progress endpoint unavailable", http.StatusServiceUnavailable)
		case "/api/libraries":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"libraries": []audiobookshelf.AudiobookshelfLibrary{{ID: "healthy-library", Name: "Healthy Library"}},
			}))
		case "/api/libraries/healthy-library/items":
			libraryItemRequests++
			assert.Equal(t, http.MethodGet, r.Method)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []models.AudiobookshelfBook{healthyBook},
				"total":   1,
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc.audiobookshelf = audiobookshelf.NewClient(server.URL, "abs-token")
	mockHC.On("ClearUserBookCache").Return().Once()

	err := svc.Sync(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch user progress data")
	assert.Equal(t, 1, userProgressRequests)
	assert.Equal(t, 2, libraryItemRequests, "healthy library should still be fetched for pre-count and processing")
	snapshot := svc.GetSnapshot()
	assert.Equal(t, "failed", snapshot.State)
	assert.Equal(t, int32(1), snapshot.BooksTotal)
	assert.Zero(t, snapshot.ProcessedSoFar, "the book has no reliable progress and must not be reported as an unread skip")
	assert.Empty(t, snapshot.BookOutcomes)
	mockHC.AssertExpectations(t)
}

func TestSyncDoesNotProcessUnreliableProgressWithDefaultUnreadSettings(t *testing.T) {
	svc, _ := createTestService()
	testDir := t.TempDir()
	svc.statePath = filepath.Join(testDir, "sync-state.json")
	svc.config.Paths.MismatchOutputDir = filepath.Join(testDir, "mismatches")
	svc.persistentCache = NewPersistentASINCache(filepath.Join(testDir, "cache"))
	_ = svc.persistentCache.Load()
	svc.userBookCache = NewPersistentUserBookCache(filepath.Join(testDir, "cache"))
	_ = svc.userBookCache.Load()
	svc.userBookCache.Clear()
	require.True(t, svc.config.Sync.ProcessUnreadBooks)
	require.True(t, svc.config.Sync.SyncWantToRead)

	unreliableBook := *toAudiobookshelfBook(createTestBook("default-progress-unavailable-book", "Progress Unavailable", "Author", "UNRELIABLE-ASIN", ""))
	var userProgressRequests int
	var libraryItemRequests int
	audiobookshelfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			userProgressRequests++
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "Bearer abs-token", r.Header.Get("Authorization"))
			http.Error(w, "progress endpoint unavailable", http.StatusServiceUnavailable)
		case "/api/libraries":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"libraries": []audiobookshelf.AudiobookshelfLibrary{{ID: "healthy-library", Name: "Healthy Library"}},
			}))
		case "/api/libraries/healthy-library/items":
			libraryItemRequests++
			assert.Equal(t, http.MethodGet, r.Method)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []models.AudiobookshelfBook{unreliableBook},
				"total":   1,
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer audiobookshelfServer.Close()

	var hardcoverMutationRequests atomic.Int32
	hardcoverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Hardcover request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		query := strings.ToLower(request.Query)
		if strings.Contains(query, "mutation") {
			hardcoverMutationRequests.Add(1)
		}

		response := `{"data":{}}`
		switch {
		case strings.Contains(query, "bookbyasin"):
			response = `{"data":{"books":[{"id":123,"title":"Progress Unavailable","book_status_id":1,"editions":[{"id":456,"asin":"UNRELIABLE-ASIN","reading_format_id":2,"audio_seconds":3600}]}]}}`
		case strings.Contains(query, "getedition"):
			response = `{"data":{"editions":[{"id":456,"book_id":789,"title":"Progress Unavailable"}]}}`
		case strings.Contains(query, "getcurrentuserid"):
			response = `{"data":{"me":[{"id":1}]}}`
		case strings.Contains(query, "getuserbookbybookonly"),
			strings.Contains(query, "getuserbookbybook"):
			response = `{"data":{"user_books":[]}}`
		case strings.Contains(query, "getuserbookbyedition"):
			response = `{"data":{"user_books":[{"id":999,"edition_id":456}]}}`
		case strings.Contains(query, "mutation"):
			response = `{"data":{"update_user_book":{"id":999,"error":null}}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer hardcoverServer.Close()

	svc.audiobookshelf = audiobookshelf.NewClient(audiobookshelfServer.URL, "abs-token")
	svc.hardcover = hardcover.NewClientWithConfig(&hardcover.ClientConfig{
		BaseURL:       hardcoverServer.URL,
		Timeout:       time.Second,
		MaxRetries:    0,
		RetryDelay:    time.Nanosecond,
		RateLimit:     time.Nanosecond,
		MaxConcurrent: 1,
	}, "hc-token", logger.Get())

	err := svc.Sync(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch user progress data")
	assert.Equal(t, 1, userProgressRequests)
	assert.Equal(t, 2, libraryItemRequests, "healthy library should still be fetched for pre-count and processing")
	snapshot := svc.GetSnapshot()
	assert.Equal(t, "failed", snapshot.State)
	assert.Equal(t, int32(1), snapshot.BooksTotal)
	assert.Zero(t, snapshot.ProcessedSoFar, "unreliable progress must not trigger a default WANT_TO_READ mutation")
	assert.Empty(t, snapshot.BookOutcomes)
	assert.Zero(t, hardcoverMutationRequests.Load(), "an /api/me failure must not emit any Hardcover mutation request")
}

func TestProcessLibraryRecordsFullCandidateTotalBeforeLimit(t *testing.T) {
	svc, _ := createTestService()
	svc.config.Sync.ProcessUnreadBooks = false
	mockABS := new(MockAudiobookshelfClient)
	books := []models.AudiobookshelfBook{
		*toAudiobookshelfBook(createTestBook("limited-book-1", "Limited Book 1", "Author", "", "")),
		*toAudiobookshelfBook(createTestBook("limited-book-2", "Limited Book 2", "Author", "", "")),
	}
	mockABS.On("GetLibraryItems", mock.Anything, "limited-library").Return(books, nil).Once()
	svc.audiobookshelf = mockABS

	processed, err := svc.processLibrary(
		context.Background(),
		&audiobookshelf.AudiobookshelfLibrary{ID: "limited-library", Name: "Limited Library"},
		1,
		&models.AudiobookshelfUserProgress{},
	)

	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int32(2), svc.GetSnapshot().BooksTotal)
	mockABS.AssertExpectations(t)
}

// TestProcessLibrary tests the processLibrary function
func TestProcessLibrary(t *testing.T) {
	// Setup logger with test config
	logger.Setup(logger.Config{
		Level:  "debug",
		Format: "json",
	})
	log := logger.Get()

	// Setup mocks
	mockABS := new(MockAudiobookshelfClient)
	mockHC := new(MockHardcoverClient)
	
	// Create a temporary state file
	testState := state.NewState()
	
	// Create test config with default values and update sync settings
	testConfig := config.DefaultConfig()
	
	// Configure sync settings - all sync-related settings are now consolidated under Sync
	testConfig.Sync.Incremental = false
	testConfig.Sync.StateFile = "/tmp/sync_state_test.json"
	testConfig.Sync.MinChangeThreshold = 60
	testConfig.Sync.SyncInterval = 1 * time.Hour
	testConfig.Sync.MinimumProgress = 0.05
	testConfig.Sync.SyncWantToRead = true
	testConfig.Sync.SyncOwned = true
	testConfig.Sync.DryRun = true
	
	// Initialize libraries include/exclude
	testConfig.Sync.Libraries.Include = []string{}
	testConfig.Sync.Libraries.Exclude = []string{}
	
	// Set test-specific app settings
	testConfig.App.TestBookFilter = ""
	testConfig.App.TestBookLimit = 0
	
	// Clear deprecated sync fields in App
	testConfig.App.SyncInterval = 0
	testConfig.App.MinimumProgress = 0
	testConfig.App.SyncWantToRead = false
	testConfig.App.SyncOwned = false
	testConfig.App.DryRun = false
	
	// Set test service configurations
	testConfig.Audiobookshelf.URL = "https://abs.example.com"
	testConfig.Audiobookshelf.Token = "test-token"
	testConfig.Hardcover.Token = "test-token"

	// Create the service with mocked clients
	svc := &Service{
		audiobookshelf: nil, // Will be replaced with mock
		hardcover:      mockHC,
		config:         testConfig,
		log:            log,
		state:          testState,
		statePath:      "",
		lastProgressUpdates: make(map[string]progressUpdateInfo),
		asinCache:           make(map[string]*models.HardcoverBook),
		persistentCache:     NewPersistentASINCache("/tmp"),
		userBookCache:       NewPersistentUserBookCache("/tmp"),
		summary:             &SyncSummary{},
	}
	
	// Create test library
	testLibrary := &audiobookshelf.AudiobookshelfLibrary{
		ID:   "lib1",
		Name: "Test Library",
	}
	
	// Create test user progress
	testUserProgress := &models.AudiobookshelfUserProgress{
		ID: "user1",
		Username: "testuser",
		MediaProgress: []struct {
			ID            string  `json:"id"`
			LibraryItemID string  `json:"libraryItemId"`
			UserID        string  `json:"userId"`
			IsFinished    bool    `json:"isFinished"`
			Progress      float64 `json:"progress"`
			CurrentTime   float64 `json:"currentTime"`
			Duration      float64 `json:"duration"`
			StartedAt     int64   `json:"startedAt"`
			FinishedAt    int64   `json:"finishedAt"`
			LastUpdate    int64   `json:"lastUpdate"`
			TimeListening float64 `json:"timeListening"`
		}{},
		ListeningSessions: []struct {
			ID            string `json:"id"`
			UserID        string `json:"userId"`
			LibraryItemID string `json:"libraryItemId"`
			MediaType     string `json:"mediaType"`
			MediaMetadata struct {
				Title  string `json:"title"`
				Author string `json:"author"`
			} `json:"mediaMetadata"`
			Duration    float64 `json:"duration"`
			CurrentTime float64 `json:"currentTime"`
			Progress    float64 `json:"progress"`
			IsFinished  bool    `json:"isFinished"`
			StartedAt   int64   `json:"startedAt"`
			UpdatedAt   int64   `json:"updatedAt"`
		}{},
	}
	
	// Test with an empty library
	t.Run("Empty library", func(t *testing.T) {
		// Create empty library items list
		emptyLibraryItems := []models.AudiobookshelfBook{}
		
		// Setup mock expectations
		mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return(emptyLibraryItems, nil).Once()
		
		// Replace the audiobookshelf client in the service with our mock
		svc.audiobookshelf = mockABS
		
		// Call processLibrary
		processed, err := svc.processLibrary(context.Background(), testLibrary, 0, testUserProgress)
		
		// Verify results
		assert.NoError(t, err)
		assert.Equal(t, 0, processed)
		
		// Verify mock expectations
		mockABS.AssertExpectations(t)
	})
	
	// Test with a non-empty library but with a limit
	t.Run("Library with items and limit", func(t *testing.T) {
		// Create test books
		testBooks := []models.AudiobookshelfBook{
			{
				ID: "book1",
				Media: struct {
					ID        string                               `json:"id"`
					Metadata  models.AudiobookshelfMetadataStruct `json:"metadata"`
					CoverPath string                              `json:"coverPath"`
					Duration  float64                             `json:"duration"`
				}{
					ID: "media1",
					Metadata: models.AudiobookshelfMetadataStruct{
						Title:      "Test Book 1",
						AuthorName: "Test Author 1",
						ASIN:       "B123456789",
						ISBN:       "9781234567890",
					},
					CoverPath: "/covers/test.jpg",
					Duration:  3600,
				},
				Progress: struct {
					CurrentTime float64 `json:"currentTime"`
					IsFinished  bool    `json:"isFinished"`
					StartedAt   int64   `json:"startedAt"`
					FinishedAt  int64   `json:"finishedAt"`
				}{
					CurrentTime: 0,
					IsFinished:  false,
					StartedAt:   0,
					FinishedAt:  0,
				},
			},
		}
		
		// Setup mock expectations
		mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return(testBooks, nil).Once()
		
		// Setup mock for SearchBookByASIN
		testBook := &models.HardcoverBook{
			ID: "test-book-id",
			Title: "Test Book 1",
			Authors: []models.Author{
				{Name: "Test Author 1"},
			},
			ASIN: "B123456789",
		}
		mockHC.On("SearchBookByASIN", mock.Anything, "B123456789").Return(testBook, nil).Once()
		// Some code paths attempt an ISBN13 lookup during enrichment; stub it to return no result
		mockHC.On("SearchBookByISBN13", mock.Anything, "9781234567890").Return((*models.HardcoverBook)(nil), nil).Maybe()
		// Enrichment may fall back to a title/author search; stub it as returning no results
		mockHC.On("SearchBooks", mock.Anything, "Test Book 1", "Test Author 1").Return([]*TestHardcoverBook{}, nil).Maybe()
		
		// Replace the clients in the service with our mocks
		svc.audiobookshelf = mockABS
		svc.hardcover = mockHC
		
		// Set a limit of 1 book
		testConfig.Sync.TestBookLimit = 1
		
		// Call processLibrary
		processed, err := svc.processLibrary(context.Background(), testLibrary, 0, testUserProgress)
		
		// Verify results
		assert.NoError(t, err)
		assert.Equal(t, 1, processed)
		
		// Verify mock expectations
		mockABS.AssertExpectations(t)
	})
}

func TestSyncReturnsErrorForLibraryItemWithoutID(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.DryRun = true

	mockABS := new(MockAudiobookshelfClient)
	libraries := []audiobookshelf.AudiobookshelfLibrary{{ID: "library-id", Name: "Library Name"}}
	invalidBook := *toAudiobookshelfBook(createTestBook("", "Missing ID", "Test Author", "", ""))
	items := []models.AudiobookshelfBook{invalidBook}
	mockABS.On("GetUserProgress", mock.Anything).Return(&models.AudiobookshelfUserProgress{}, nil).Once()
	mockABS.On("GetLibraries", mock.Anything).Return(libraries, nil).Once()
	// Sync fetches library items once for the status total and once for processing.
	mockABS.On("GetLibraryItems", mock.Anything, "library-id").Return(items, nil).Twice()
	mockHC.On("ClearUserBookCache").Return().Once()
	svc.audiobookshelf = mockABS

	err := svc.Sync(context.Background())

	require.ErrorIs(t, err, errInvalidLibraryItemID)
	assert.Contains(t, err.Error(), `library "Library Name" (ID "library-id"), item position 1`)
	snapshot := svc.GetSnapshot()
	assert.Zero(t, snapshot.ProcessedSoFar, "an item without an ID must not produce an outcome")
	assert.Zero(t, snapshot.TotalBooksProcessed, "an item without an ID must not be processed")
	assert.Empty(t, snapshot.BookOutcomes)
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestCheckpointStatePersistsCompletedBook(t *testing.T) {
	svc, _ := createTestService()
	svc.statePath = filepath.Join(t.TempDir(), "sync_state.json")
	svc.state.UpdateBook("book1", 50, "IN_PROGRESS")
	svc.state.SetHasProgressSeconds("book1")

	require.NoError(t, svc.checkpointState("book1"))

	loadedState, err := state.LoadState(svc.statePath)
	require.NoError(t, err)
	bookState, exists := loadedState.GetBookState("book1")
	require.True(t, exists)
	assert.Equal(t, 0.5, bookState.LastProgress)
	assert.Equal(t, "IN_PROGRESS", bookState.Status)
	assert.True(t, bookState.HasProgressSeconds)
}

func TestCheckpointStateSkipsUnchangedState(t *testing.T) {
	svc, _ := createTestService()
	svc.statePath = filepath.Join(t.TempDir(), "sync_state.json")
	svc.state.UpdateBook("book1", 50, "IN_PROGRESS")
	require.NoError(t, svc.checkpointState("book1"))

	checkpointTime := time.Unix(123, 456)
	require.NoError(t, os.Chtimes(svc.statePath, checkpointTime, checkpointTime))
	infoBefore, err := os.Stat(svc.statePath)
	require.NoError(t, err)

	require.NoError(t, svc.checkpointState("book1"))
	infoAfter, err := os.Stat(svc.statePath)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime())

	svc.state.UpdateBook("book1", 75, "IN_PROGRESS")
	require.NoError(t, svc.checkpointState("book1"))
	loadedState, err := state.LoadState(svc.statePath)
	require.NoError(t, err)
	bookState, exists := loadedState.GetBookState("book1")
	require.True(t, exists)
	assert.Equal(t, 0.75, bookState.LastProgress)
}

func TestCheckpointStateSkipsDryRun(t *testing.T) {
	svc, _ := createTestService()
	svc.config.Sync.DryRun = true
	svc.statePath = filepath.Join(t.TempDir(), "sync_state.json")
	svc.state.UpdateBook("book1", 50, "IN_PROGRESS")

	require.NoError(t, svc.checkpointState("book1"))
	_, err := os.Stat(svc.statePath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, exists := svc.state.GetBookState("book1")
	assert.True(t, exists, "dry-run state should remain available in memory")
}

func TestProcessLibraryReturnsCheckpointFailure(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.ProcessUnreadBooks = false
	svc.statePath = t.TempDir() // Renaming a state file over a directory must fail.
	svc.state.UpdateBook("existing-book", 50, "IN_PROGRESS")

	mockABS := new(MockAudiobookshelfClient)
	book := toAudiobookshelfBook(createTestBook("book1", "Unread Book", "Test Author", "", ""))
	book.Progress.CurrentTime = 0
	book.Progress.IsFinished = false
	mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return([]models.AudiobookshelfBook{*book}, nil).Once()
	svc.audiobookshelf = mockABS

	_, err := svc.processLibrary(
		context.Background(),
		&audiobookshelf.AudiobookshelfLibrary{ID: "lib1", Name: "Test Library"},
		0,
		&models.AudiobookshelfUserProgress{},
	)

	assert.ErrorIs(t, err, errStateCheckpoint)
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestProcessLibraryCheckpointsOnceThenReturnsCancellation(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.ProcessUnreadBooks = false
	svc.statePath = filepath.Join(t.TempDir(), "sync_state.json")

	mockABS := new(MockAudiobookshelfClient)
	books := make([]models.AudiobookshelfBook, 2)
	for i := range books {
		book := toAudiobookshelfBook(createTestBook(fmt.Sprintf("book%d", i+1), "Unread Book", "Test Author", "", ""))
		if i == 0 {
			// Give the first book enough progress to reach the deterministic
			// lookup failure path, which records its state before cancellation.
			book.Media.Metadata.ISBN = "ISBN"
			book.Progress.CurrentTime = book.Media.Duration / 2
		} else {
			book.Progress.CurrentTime = 0
		}
		book.Progress.IsFinished = false
		books[i] = *book
	}
	mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return(books, nil).Once()
	ctx, cancel := context.WithCancel(context.Background())
	mockHC.On("SearchBookByISBN13", mock.Anything, "ISBN").Return(&models.HardcoverBook{}, nil).Once()
	mockHC.On("SearchBookByISBN13", mock.Anything, "ISBN").Return((*models.HardcoverBook)(nil), assert.AnError).Once().Run(func(mock.Arguments) {
		// Cancel after the first book reaches its final lookup, before the
		// next loop iteration, so the first book's checkpoint is durable.
		cancel()
	})
	mockHC.On("SearchBookByISBN10", mock.Anything, "ISBN").Return((*models.HardcoverBook)(nil), nil).Once()
	mockHC.On("SearchBooks", mock.Anything, "Unread Book Test Author", "").Return([]models.HardcoverBook{}, nil).Once()
	mockHC.On("SearchBooks", mock.Anything, "Unread Book", "Test Author").Return([]models.HardcoverBook{}, nil).Once()
	svc.audiobookshelf = mockABS

	_, err := svc.processLibrary(
		ctx,
		&audiobookshelf.AudiobookshelfLibrary{ID: "lib1", Name: "Test Library"},
		0,
		&models.AudiobookshelfUserProgress{},
	)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), svc.summary.TotalBooksProcessed, "cancellation must stop before the second book")
	loadedState, loadErr := state.LoadState(svc.statePath)
	require.NoError(t, loadErr)
	bookState, exists := loadedState.GetBookState("book1")
	require.True(t, exists, "the first book state must be durable before cancellation returns")
	assert.Equal(t, 0.5, bookState.LastProgress)
	assert.Equal(t, "SKIPPED", bookState.Status)
	assert.True(t, bookState.HasProgressSeconds)
	_, exists = loadedState.GetBookState("book2")
	assert.False(t, exists, "the second book must not be entered after cancellation")
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestProcessLibraryStopsBeforeBookWhenAlreadyCanceled(t *testing.T) {
	svc, mockHC := createTestService()
	svc.config.Sync.ProcessUnreadBooks = false
	svc.statePath = filepath.Join(t.TempDir(), "sync_state.json")

	mockABS := new(MockAudiobookshelfClient)
	book := toAudiobookshelfBook(createTestBook("book1", "Unread Book", "Test Author", "", ""))
	book.Progress.CurrentTime = book.Media.Duration / 2
	mockABS.On("GetLibraryItems", mock.Anything, "lib1").Return([]models.AudiobookshelfBook{*book}, nil).Once()
	svc.audiobookshelf = mockABS

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.processLibrary(
		ctx,
		&audiobookshelf.AudiobookshelfLibrary{ID: "lib1", Name: "Test Library"},
		0,
		&models.AudiobookshelfUserProgress{},
	)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, svc.summary.TotalBooksProcessed, "an already-canceled run must not enter a book")
	_, statErr := os.Stat(svc.statePath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

func TestSyncReturnsFinalStateSaveFailure(t *testing.T) {
	svc, mockHC := createTestService()
	svc.statePath = t.TempDir() // Renaming a state file over a directory must fail.
	svc.config.Paths.MismatchOutputDir = t.TempDir()

	mockABS := new(MockAudiobookshelfClient)
	mockABS.On("GetUserProgress", mock.Anything).Return(&models.AudiobookshelfUserProgress{}, nil).Once()
	mockABS.On("GetLibraries", mock.Anything).Return([]audiobookshelf.AudiobookshelfLibrary{}, nil).Once()
	mockHC.On("ClearUserBookCache").Return().Once()
	svc.audiobookshelf = mockABS

	err := svc.Sync(context.Background())

	require.Error(t, err)
	mockABS.AssertExpectations(t)
	mockHC.AssertExpectations(t)
}

// TestProcessBook and TestFindBookInHardcover functions would follow the same pattern
// with their test configurations updated similarly to the above examples.
