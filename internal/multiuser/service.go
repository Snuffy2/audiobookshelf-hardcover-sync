package multiuser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	stdSync "sync"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/config"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/mismatch"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync"
)

// SyncProfileStatus represents the sync status for a profile
type SyncProfileStatus struct {
	ProfileID       string                  `json:"profile_id"`
	ProfileName     string                  `json:"profile_name"`
	Status          string                  `json:"status"` // "idle", "syncing", "error", "completed"
	DryRun          bool                    `json:"dry_run,omitempty"`
	LastSync        *time.Time              `json:"last_sync"`
	Error           string                  `json:"error,omitempty"`
	Progress        string                  `json:"progress,omitempty"`
	BooksTotal      int                     `json:"books_total,omitempty"`
	BooksSynced     int                     `json:"books_synced,omitempty"`
	BooksNotFound   []sync.BookNotFoundInfo `json:"books_not_found,omitempty"`
	Mismatches      []mismatch.BookMismatch `json:"mismatches,omitempty"`
	LastSyncSummary *sync.SyncSummary       `json:"last_sync_summary,omitempty"`
	Snapshot        *sync.SyncSnapshot      `json:"snapshot,omitempty"`
}

type activeSyncRun struct {
	generation uint64
	runID      string
	startedAt  time.Time
	canceled   bool
}

type syncStateRepository interface {
	GetSyncState(profileID string) (*database.ProfileSyncState, error)
	UpdateSyncState(state *database.ProfileSyncState) error
}

// MultiUserService manages sync operations for multiple users
type MultiUserService struct {
	repository      *database.Repository
	logger          *logger.Logger
	globalConfig    *config.Config
	profileStatuses map[string]*SyncProfileStatus
	statusMutex     stdSync.RWMutex
	activeSyncs     map[string]context.CancelFunc
	activeRuns      map[string]activeSyncRun
	nextGeneration  uint64
	syncMutex       stdSync.RWMutex
	stateRepository syncStateRepository
	persistenceMu   stdSync.Mutex
	persistedRuns   map[string]uint64
	persistedTimes  map[string]time.Time
	syncServices    map[string]*sync.Service // Maps profile ID to its sync service
	serviceRuns     map[string]uint64
	servicesMutex   stdSync.RWMutex
}

// NewMultiUserService creates a new multi-user service
func NewMultiUserService(repo *database.Repository, globalConfig *config.Config, log *logger.Logger) *MultiUserService {
	var stateRepository syncStateRepository
	if repo != nil {
		stateRepository = repo
	}
	return &MultiUserService{
		repository:      repo,
		logger:          log,
		globalConfig:    globalConfig,
		profileStatuses: make(map[string]*SyncProfileStatus),
		activeSyncs:     make(map[string]context.CancelFunc),
		activeRuns:      make(map[string]activeSyncRun),
		stateRepository: stateRepository,
		persistedRuns:   make(map[string]uint64),
		persistedTimes:  make(map[string]time.Time),
		syncServices:    make(map[string]*sync.Service),
		serviceRuns:     make(map[string]uint64),
	}
}

// ListProfiles returns all active sync profiles
func (s *MultiUserService) ListProfiles() ([]database.SyncProfile, error) {
	return s.repository.ListProfiles()
}

// GetProfile returns a specific profile with decrypted tokens
func (s *MultiUserService) GetProfile(profileID string) (*database.ProfileWithTokens, error) {
	return s.repository.GetProfile(profileID)
}

// CreateProfile creates a new sync profile
func (s *MultiUserService) CreateProfile(profileID, name, audiobookshelfURL, audiobookshelfToken, hardcoverToken string, syncConfig database.SyncConfigData) error {
	return s.repository.CreateProfile(profileID, name, audiobookshelfURL, audiobookshelfToken, hardcoverToken, syncConfig)
}

// UpdateProfile updates profile information
func (s *MultiUserService) UpdateProfile(profileID, name string) error {
	return s.repository.UpdateProfile(profileID, name)
}

// UpdateProfileConfig updates profile configuration
func (s *MultiUserService) UpdateProfileConfig(profileID, audiobookshelfURL, audiobookshelfToken, hardcoverToken string, syncConfig database.SyncConfigData) error {
	return s.repository.UpdateUserConfig(profileID, audiobookshelfURL, audiobookshelfToken, hardcoverToken, syncConfig)
}

// DeleteProfile deletes a sync profile
func (s *MultiUserService) DeleteProfile(profileID string) error {
	// Cancel any active sync for this profile
	if err := s.CancelSync(profileID); err != nil {
		s.logger.Warn("Failed to cancel sync during profile deletion", map[string]interface{}{
			"profileID": profileID,
			"error":     err,
		})
	}
	// The profile is going away, so prevent its canceled goroutine from
	// republishing a status after the status entry is removed.
	s.syncMutex.Lock()
	if run, ok := s.activeRuns[profileID]; ok && run.canceled {
		delete(s.activeRuns, profileID)
	}
	s.syncMutex.Unlock()

	// Remove from status tracking
	s.statusMutex.Lock()
	delete(s.profileStatuses, profileID)
	s.statusMutex.Unlock()

	return s.repository.DeleteProfile(profileID)
}

// GetAllProfileStatuses returns the sync status for all profiles
func (s *MultiUserService) GetAllProfileStatuses() ([]*SyncProfileStatus, error) {
	profiles, err := s.repository.ListProfiles()
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}

	statuses := make([]*SyncProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		status := s.getProfileStatus(profile.ID, &profile, profile.SyncState, true)
		if status == nil {
			status = &SyncProfileStatus{ProfileID: profile.ID, Status: "idle"}
		}
		if status.ProfileName == "" {
			status.ProfileName = profile.Name
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// GetSyncService returns the sync service for a profile, if it exists
func (s *MultiUserService) GetSyncService(profileID string) (*sync.Service, bool) {
	service, _ := s.currentSyncService(profileID)
	return service, service != nil
}

// currentSyncService returns only the service belonging to the active run.
// A canceled run can remain in the map briefly while its goroutine unwinds;
// its generation must not be exposed to a newer run.
func (s *MultiUserService) currentSyncService(profileID string) (*sync.Service, uint64) {
	// Keep the lock order consistent with register/removeSyncService so the
	// service and generation are read as one pair during replacement.
	s.syncMutex.RLock()
	defer s.syncMutex.RUnlock()
	return s.currentSyncServiceLocked(profileID)
}

// currentSyncServiceLocked is currentSyncService with syncMutex already held.
// Callers must retain the lock while using the returned service and generation
// pair so a replacement cannot split the pair from the active run.
func (s *MultiUserService) currentSyncServiceLocked(profileID string) (*sync.Service, uint64) {
	s.servicesMutex.RLock()
	defer s.servicesMutex.RUnlock()
	service, exists := s.syncServices[profileID]
	generation := s.serviceRuns[profileID]
	activeRun, active := s.activeRuns[profileID]
	if !exists || service == nil {
		return nil, 0
	}
	if activeRun.canceled {
		return nil, 0
	}
	// A zero generation is retained as a compatibility path for tests or
	// callers that provide a service directly in the in-memory map.
	if generation == 0 {
		return service, 0
	}
	if !active || activeRun.generation != generation {
		return nil, 0
	}
	return service, generation
}

// GetProfileStatus returns the sync status for a profile
func (s *MultiUserService) GetProfileStatus(profileID string) *SyncProfileStatus {
	return s.getProfileStatus(profileID, nil, nil, false)
}

func (s *MultiUserService) getProfileStatus(profileID string, preloadedProfile *database.SyncProfile, preloadedState *database.ProfileSyncState, hasPreloadedState bool) *SyncProfileStatus {
	// Sync lifecycle writers update activeRuns, profileStatuses, and the
	// service registration under this lock. Hold it while cloning status and
	// reading the current service so a replacement cannot mix generations in a
	// single response.
	s.syncMutex.RLock()
	defer s.syncMutex.RUnlock()

	s.statusMutex.RLock()
	status, exists := s.profileStatuses[profileID]
	status = cloneProfileStatus(status)
	s.statusMutex.RUnlock()

	if !exists || status == nil {
		if preloadedProfile != nil {
			// ListProfiles already established that this profile exists, so use
			// its loaded metadata without issuing another profile query.
			status = &SyncProfileStatus{
				ProfileID:   profileID,
				ProfileName: preloadedProfile.Name,
				Status:      "idle",
				LastSync:    nil,
			}
		} else {
			// Check if profile exists in database
			profile, err := s.GetProfile(profileID)
			if err != nil || profile == nil {
				return &SyncProfileStatus{
					ProfileID: profileID,
					Status:    "error",
					Error:     "Profile not found",
					LastSync:  nil,
				}
			}

			// Create default status for existing profile
			status = &SyncProfileStatus{
				ProfileID:   profileID,
				ProfileName: profile.Profile.Name,
				Status:      "idle",
				LastSync:    nil,
			}
			if !hasPreloadedState {
				preloadedState = profile.Profile.SyncState
				hasPreloadedState = true
			}
		}
	}

	// If we do not have an in-memory LastSync (e.g., after restart), hydrate from DB
	if status.LastSync == nil {
		state := preloadedState
		if !hasPreloadedState && s.repository != nil {
			state, _ = s.repository.GetSyncState(profileID)
		}
		if state != nil && state.LastSync != nil {
			lastSync := *state.LastSync
			status.LastSync = &lastSync
		}
	}
	if status.Snapshot != nil {
		applySnapshotToStatus(status, *status.Snapshot)
	}

	// If there's an active sync service, get the latest status from it
	svc, generation := s.currentSyncServiceLocked(profileID)

	if svc != nil {
		snapshot := svc.GetSnapshot()
		if generation > 0 {
			snapshot = s.normalizeRunSnapshotLocked(profileID, generation, snapshot)
		}
		// Keep the stored status when its run identity differs from the
		// active service. This is defensive for an in-memory replacement
		// observed between lifecycle updates and prevents mixed responses.
		if generation == 0 || status.Snapshot == nil || status.Snapshot.RunID == "" || snapshot.RunID == "" || status.Snapshot.RunID == snapshot.RunID {
			applyLiveSnapshotToStatus(status, snapshot)
			if status.Snapshot.UserID == "" {
				status.Snapshot.UserID = profileID
				status.LastSyncSummary.UserID = profileID
			}
		}
	}

	return status
}

// StartSync starts a sync operation for a specific profile
func (s *MultiUserService) StartSync(profileID string) error {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()

	if _, exists := s.activeSyncs[profileID]; exists {
		return fmt.Errorf("sync already in progress for profile %s", profileID)
	}

	// Get profile config
	profileConfig, err := s.GetProfile(profileID)
	if err != nil {
		return fmt.Errorf("failed to get profile config: %w", err)
	}

	// Create cancellable context and store cancel
	ctx, cancel := context.WithCancel(context.Background())
	s.nextGeneration++
	run := activeSyncRun{
		generation: s.nextGeneration,
		startedAt:  time.Now().UTC(),
	}
	run.runID = fmt.Sprintf("%s-%d-%d", profileID, run.startedAt.UnixNano(), run.generation)
	s.activeSyncs[profileID] = cancel
	s.activeRuns[profileID] = run

	// Update initial status
	initialStatus := &SyncProfileStatus{
		ProfileID:   profileID,
		ProfileName: profileConfig.Profile.Name,
		Status:      "syncing",
		DryRun:      profileConfig.SyncConfig.DryRun,
		LastSync:    nil,
		Progress:    "Starting sync...",
	}
	applySnapshotToStatus(initialStatus, newRunSnapshot(profileID, run, "syncing"))
	s.updateProfileStatus(profileID, initialStatus)

	// Start the sync in background
	go s.performSync(ctx, profileID, profileConfig, run.generation)
	return nil
}

// CancelSync cancels a running sync operation for a profile
func (s *MultiUserService) CancelSync(profileID string) error {
	s.syncMutex.Lock()

	cancel, exists := s.activeSyncs[profileID]
	if !exists {
		s.syncMutex.Unlock()
		return fmt.Errorf("no active sync for profile %s", profileID)
	}
	run, runExists := s.activeRuns[profileID]
	service, _ := s.currentSyncServiceLocked(profileID)
	cancel()
	delete(s.activeSyncs, profileID)
	if runExists {
		run.canceled = true
		s.activeRuns[profileID] = run
	}

	finalStatus := &SyncProfileStatus{
		ProfileID: profileID,
		Status:    "idle",
		LastSync:  timePtr(time.Now()),
		Progress:  "Sync canceled",
	}
	s.statusMutex.RLock()
	storedStatus := cloneProfileStatus(s.profileStatuses[profileID])
	s.statusMutex.RUnlock()
	if storedStatus != nil {
		finalStatus.ProfileName = storedStatus.ProfileName
		finalStatus.DryRun = storedStatus.DryRun
	}
	if service != nil && runExists {
		snapshot := s.normalizeRunSnapshotLocked(profileID, run.generation, service.GetSnapshot())
		snapshot.State = "canceled"
		applySnapshotToStatus(finalStatus, snapshot)
	} else if runExists {
		var snapshot sync.SyncSnapshot
		if storedStatus != nil && storedStatus.Snapshot != nil {
			snapshot = *storedStatus.Snapshot
			snapshot.RunID = run.runID
			snapshot.RunStartedAt = run.startedAt
			snapshot.UserID = profileID
		} else {
			snapshot = newRunSnapshot(profileID, run, "canceled")
		}
		snapshot.State = "canceled"
		applySnapshotToStatus(finalStatus, snapshot)
	}

	s.updateProfileStatus(profileID, finalStatus)
	generation := run.generation
	lastSync := finalStatus.LastSync
	s.syncMutex.Unlock()

	// Persist last_sync without holding syncMutex. Generation and timestamp
	// ordering are enforced by persistLastSync so a delayed stale run cannot
	// overwrite a replacement run's timestamp.
	s.persistLastSync(profileID, generation, lastSync)
	return nil
}

// performSync performs the actual sync operation for a profile
func (s *MultiUserService) performSync(ctx context.Context, profileID string, profileConfig *database.ProfileWithTokens, generation uint64) {
	// Ensure the active sync marker is cleared when this sync finishes
	defer s.finishActiveRun(profileID, generation)
	// Create profile-specific config
	config := s.createProfileSpecificConfig(profileConfig)

	// Create clients
	absClient := audiobookshelf.NewClient(profileConfig.AudiobookshelfURL, profileConfig.AudiobookshelfToken)

	// Build Hardcover client config using global settings (rate limits/base URL)
	hcCfg := hardcover.DefaultClientConfig()
	if s.globalConfig != nil {
		if s.globalConfig.Hardcover.BaseURL != "" {
			hcCfg.BaseURL = s.globalConfig.Hardcover.BaseURL
		}
		if s.globalConfig.RateLimit.Rate > 0 {
			hcCfg.RateLimit = s.globalConfig.RateLimit.Rate
		}
		if s.globalConfig.RateLimit.MaxConcurrent > 0 {
			hcCfg.MaxConcurrent = s.globalConfig.RateLimit.MaxConcurrent
		}
	}

	s.logger.Debug("Initializing Hardcover client (multi-user)", map[string]interface{}{
		"profile_id":     profileID,
		"base_url":       hcCfg.BaseURL,
		"rate_limit":     hcCfg.RateLimit.String(),
		"max_concurrent": hcCfg.MaxConcurrent,
	})

	hcClient := hardcover.NewClientWithConfig(hcCfg, profileConfig.HardcoverToken, s.logger)

	// Create sync service
	syncService, err := sync.NewService(absClient, hcClient, config)
	if err != nil {
		status := &SyncProfileStatus{
			ProfileID:   profileID,
			ProfileName: profileConfig.Profile.Name,
			Status:      "error",
			Error:       fmt.Sprintf("Failed to create sync service: %v", err),
		}
		if run, ok := s.activeRun(profileID, generation); ok {
			applySnapshotToStatus(status, newRunSnapshot(profileID, run, "failed"))
			s.publishFinalStatus(profileID, generation, status)
		}
		return
	}

	// Store the sync service for status access
	if !s.registerSyncService(profileID, generation, syncService) {
		return
	}
	defer s.removeSyncService(profileID, generation, syncService)

	// Run the sync
	err = syncService.Sync(ctx)

	// Obtain summary
	summary := syncService.GetSummary()
	snapshot := syncService.GetSnapshot()
	snapshot = s.normalizeRunSnapshot(profileID, generation, snapshot)

	// Prepare final status
	status := &SyncProfileStatus{
		ProfileID:   profileID,
		ProfileName: profileConfig.Profile.Name,
		DryRun:      profileConfig.SyncConfig.DryRun,
		LastSync:    timePtr(time.Now()),
	}
	applySnapshotToStatus(status, snapshot)
	if status.Snapshot.UserID == "" {
		status.Snapshot.UserID = profileID
		status.LastSyncSummary.UserID = profileID
	}

	if err != nil {
		status.Status = "error"
		status.Error = err.Error()
		if status.Snapshot != nil && status.Snapshot.State == "canceled" {
			status.Status = "idle"
			status.Error = ""
			status.Progress = "Sync canceled"
		}
		s.logger.Error("Sync failed", map[string]interface{}{
			"profileID": profileID,
			"error":     err,
		})
	} else {
		status.Status = "completed"
		status.Progress = "Sync completed successfully"

		s.logger.Debug("Stored full sync summary in profile status", map[string]interface{}{
			"profileID":       profileID,
			"books_processed": summary.TotalBooksProcessed,
			"books_synced":    summary.BooksSynced,
			"books_not_found": len(summary.BooksNotFound),
			"mismatches":      len(summary.Mismatches),
		})
	}

	// Publish only if this run is still current. A canceled run can finish
	// after a replacement run has started and must not overwrite its status or
	// persisted timestamp.
	s.publishFinalStatus(profileID, generation, status)
}

func newRunSnapshot(profileID string, run activeSyncRun, state string) sync.SyncSnapshot {
	return sync.SyncSnapshot{
		UserID:           profileID,
		RunID:            run.runID,
		RunStartedAt:     run.startedAt,
		State:            state,
		BookOutcomes:     make([]sync.BookOutcomeRecord, 0),
		AttentionRecords: make([]sync.BookOutcomeRecord, 0),
		BooksNotFound:    make([]sync.BookNotFoundInfo, 0),
		Mismatches:       make([]mismatch.BookMismatch, 0),
	}
}

func (s *MultiUserService) activeRun(profileID string, generation uint64) (activeSyncRun, bool) {
	s.syncMutex.RLock()
	run, ok := s.activeRuns[profileID]
	s.syncMutex.RUnlock()
	if !ok || run.generation != generation {
		return activeSyncRun{}, false
	}
	return run, true
}

func (s *MultiUserService) normalizeRunSnapshot(profileID string, generation uint64, snapshot sync.SyncSnapshot) sync.SyncSnapshot {
	s.syncMutex.RLock()
	defer s.syncMutex.RUnlock()
	return s.normalizeRunSnapshotLocked(profileID, generation, snapshot)
}

func (s *MultiUserService) normalizeRunSnapshotLocked(profileID string, generation uint64, snapshot sync.SyncSnapshot) sync.SyncSnapshot {
	if run, ok := s.activeRuns[profileID]; ok && run.generation == generation {
		snapshot.UserID = profileID
		snapshot.RunID = run.runID
		snapshot.RunStartedAt = run.startedAt
		if snapshot.State == "" || snapshot.State == "idle" {
			snapshot.State = "syncing"
		}
		return snapshot
	}
	if snapshot.UserID == "" {
		snapshot.UserID = profileID
	}
	return snapshot
}

func (s *MultiUserService) registerSyncService(profileID string, generation uint64, service *sync.Service) bool {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	run, ok := s.activeRuns[profileID]
	if !ok || run.generation != generation {
		return false
	}
	s.servicesMutex.Lock()
	s.syncServices[profileID] = service
	s.serviceRuns[profileID] = generation
	s.servicesMutex.Unlock()
	return true
}

func (s *MultiUserService) removeSyncService(profileID string, generation uint64, service *sync.Service) {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	s.servicesMutex.Lock()
	if s.syncServices[profileID] == service && s.serviceRuns[profileID] == generation {
		delete(s.syncServices, profileID)
		delete(s.serviceRuns, profileID)
	}
	s.servicesMutex.Unlock()
}

func (s *MultiUserService) finishActiveRun(profileID string, generation uint64) {
	s.syncMutex.Lock()
	defer s.syncMutex.Unlock()
	if run, ok := s.activeRuns[profileID]; ok && run.generation == generation {
		delete(s.activeRuns, profileID)
		delete(s.activeSyncs, profileID)
	}
}

func (s *MultiUserService) publishFinalStatus(profileID string, generation uint64, status *SyncProfileStatus) bool {
	s.syncMutex.Lock()
	run, ok := s.activeRuns[profileID]
	if !ok || run.generation != generation || status == nil {
		s.syncMutex.Unlock()
		return false
	}
	if run.canceled && (status.Snapshot == nil || status.Snapshot.State != "canceled") {
		s.syncMutex.Unlock()
		return false
	}
	statusCopy := cloneProfileStatus(status)
	lastSync := statusCopy.LastSync
	s.statusMutex.Lock()
	s.profileStatuses[profileID] = statusCopy
	s.statusMutex.Unlock()
	s.syncMutex.Unlock()

	// Persist after publishing the in-memory status and without holding
	// syncMutex. persistLastSync serializes repository I/O and rejects stale
	// generations/timestamps.
	s.persistLastSync(profileID, generation, lastSync)
	return true
}

func (s *MultiUserService) persistLastSync(profileID string, generation uint64, lastSync *time.Time) {
	if lastSync == nil {
		return
	}
	var repository syncStateRepository
	if s.stateRepository != nil {
		repository = s.stateRepository
	} else if s.repository != nil {
		repository = s.repository
	}
	if repository == nil {
		return
	}

	candidate := *lastSync
	s.persistenceMu.Lock()
	defer s.persistenceMu.Unlock()
	if s.persistedRuns == nil {
		s.persistedRuns = make(map[string]uint64)
	}
	if s.persistedTimes == nil {
		s.persistedTimes = make(map[string]time.Time)
	}
	if previousGeneration, ok := s.persistedRuns[profileID]; ok && generation < previousGeneration {
		return
	}
	if previous, ok := s.persistedTimes[profileID]; ok && candidate.Before(previous) {
		return
	}

	state, err := repository.GetSyncState(profileID)
	if err != nil {
		return
	}
	if state == nil {
		state = &database.ProfileSyncState{ProfileID: profileID, StateData: "{}"}
	}
	if state.LastSync != nil && state.LastSync.After(candidate) {
		if generation > s.persistedRuns[profileID] {
			s.persistedRuns[profileID] = generation
		}
		s.persistedTimes[profileID] = *state.LastSync
		return
	}
	state.LastSync = &candidate
	if err := repository.UpdateSyncState(state); err != nil {
		return
	}
	if generation > s.persistedRuns[profileID] {
		s.persistedRuns[profileID] = generation
	}
	if previous, ok := s.persistedTimes[profileID]; !ok || candidate.After(previous) {
		s.persistedTimes[profileID] = candidate
	}
}

// createProfileSpecificConfig creates a config.Config instance for a specific profile
func (s *MultiUserService) createProfileSpecificConfig(profileConfig *database.ProfileWithTokens) *config.Config {
	// Create a copy of the global config
	config := *s.globalConfig

	// Override with profile-specific settings
	config.Audiobookshelf.URL = profileConfig.AudiobookshelfURL
	config.Audiobookshelf.Token = profileConfig.AudiobookshelfToken
	config.Hardcover.Token = profileConfig.HardcoverToken

	// Apply sync config from profile
	syncConfig := profileConfig.SyncConfig

	// Always apply the sync config from the profile
	// The config in the profile should already have the correct values (either defaults or explicitly set)

	// Parse sync interval if provided
	if syncConfig.SyncInterval != "" {
		duration, err := time.ParseDuration(syncConfig.SyncInterval)
		if err != nil {
			s.logger.Warn("Invalid sync interval, using default", map[string]interface{}{
				"profileID": profileConfig.Profile.ID,
				"interval":  syncConfig.SyncInterval,
				"error":     err,
			})
			duration = 1 * time.Hour // Default to 1 hour if invalid
		}
		config.Sync.SyncInterval = duration
	}

	// Apply all sync config values from the profile
	config.Sync.Incremental = syncConfig.Incremental
	// Make state file path profile-specific to avoid conflicts
	// Resolve state file path using paths.data_dir if not set or relative
	statePath := syncConfig.StateFile
	if statePath == "" {
		// Use paths.data_dir for default state file location
		if s.globalConfig != nil && s.globalConfig.Paths.DataDir != "" {
			statePath = fmt.Sprintf("%s/sync_state.json", strings.TrimSuffix(s.globalConfig.Paths.DataDir, "/"))
		} else {
			statePath = "/data/sync_state.json" // Container-friendly default
		}
	} else if !filepath.IsAbs(statePath) {
		// If relative, resolve it against paths.data_dir (preserving the relative filename).
		if s.globalConfig != nil && s.globalConfig.Paths.DataDir != "" {
			statePath = filepath.Join(s.globalConfig.Paths.DataDir, statePath)
		}
	}
	config.Sync.StateFile = fmt.Sprintf("%s.%s", strings.TrimSuffix(statePath, ".json"), profileConfig.Profile.ID)
	config.Sync.MinChangeThreshold = syncConfig.MinChangeThreshold
	config.Sync.Libraries.Include = syncConfig.Libraries.Include
	config.Sync.Libraries.Exclude = syncConfig.Libraries.Exclude
	config.Sync.MinimumProgress = syncConfig.MinimumProgress
	config.Sync.SyncWantToRead = syncConfig.SyncWantToRead
	config.Sync.ProcessUnreadBooks = syncConfig.ProcessUnreadBooks
	config.Sync.SyncOwned = syncConfig.SyncOwned
	config.Sync.IncludeEbooks = syncConfig.IncludeEbooks
	config.Sync.DryRun = syncConfig.DryRun
	config.Sync.TestBookFilter = syncConfig.TestBookFilter
	config.Sync.TestBookLimit = syncConfig.TestBookLimit
	config.Audiobookshelf.AudnexusRegion = syncConfig.AudnexusRegion

	// Debug logging to verify the config is being applied correctly
	s.logger.Debug("Applied sync config for profile", map[string]interface{}{
		"profileID":            profileConfig.Profile.ID,
		"process_unread_books": config.Sync.ProcessUnreadBooks,
		"incremental":          config.Sync.Incremental,
		"sync_want_to_read":    config.Sync.SyncWantToRead,
	})

	return &config
}

func cloneBookMismatch(m mismatch.BookMismatch) mismatch.BookMismatch {
	copyOf := m
	copyOf.AuthorIDs = append([]int(nil), m.AuthorIDs...)
	copyOf.NarratorIDs = append([]int(nil), m.NarratorIDs...)
	return copyOf
}

func cloneSyncSummary(summary *sync.SyncSummary) *sync.SyncSummary {
	if summary == nil {
		return nil
	}
	copyOf := &sync.SyncSummary{
		UserID:              summary.UserID,
		TotalBooksProcessed: summary.TotalBooksProcessed,
		BooksSynced:         summary.BooksSynced,
		BooksTotal:          summary.BooksTotal,
		BooksNotFound:       append([]sync.BookNotFoundInfo(nil), summary.BooksNotFound...),
		Mismatches:          make([]mismatch.BookMismatch, len(summary.Mismatches)),
	}
	for i, book := range summary.Mismatches {
		copyOf.Mismatches[i] = cloneBookMismatch(book)
	}
	return copyOf
}

func cloneSyncSnapshot(snapshot sync.SyncSnapshot) *sync.SyncSnapshot {
	copyOf := snapshot
	copyOf.BookOutcomes = make([]sync.BookOutcomeRecord, len(snapshot.BookOutcomes))
	copy(copyOf.BookOutcomes, snapshot.BookOutcomes)
	copyOf.AttentionRecords = make([]sync.BookOutcomeRecord, len(snapshot.AttentionRecords))
	copy(copyOf.AttentionRecords, snapshot.AttentionRecords)
	copyOf.BooksNotFound = make([]sync.BookNotFoundInfo, len(snapshot.BooksNotFound))
	copy(copyOf.BooksNotFound, snapshot.BooksNotFound)
	copyOf.Mismatches = make([]mismatch.BookMismatch, len(snapshot.Mismatches))
	for i, book := range snapshot.Mismatches {
		copyOf.Mismatches[i] = cloneBookMismatch(book)
	}
	return &copyOf
}

func applySnapshotToStatus(status *SyncProfileStatus, snapshot sync.SyncSnapshot) {
	if status == nil {
		return
	}
	status.Snapshot = cloneSyncSnapshot(snapshot)
	if snapshot.BooksTotal > 0 {
		status.BooksTotal = int(snapshot.BooksTotal)
	} else {
		status.BooksTotal = int(snapshot.ProcessedSoFar)
	}
	status.BooksSynced = int(snapshot.BooksSynced)
	status.BooksNotFound = append([]sync.BookNotFoundInfo(nil), snapshot.BooksNotFound...)
	status.Mismatches = make([]mismatch.BookMismatch, len(snapshot.Mismatches))
	for i, book := range snapshot.Mismatches {
		status.Mismatches[i] = cloneBookMismatch(book)
	}
	status.LastSyncSummary = &sync.SyncSummary{
		UserID:              snapshot.UserID,
		TotalBooksProcessed: snapshot.TotalBooksProcessed,
		BooksSynced:         snapshot.BooksSynced,
		BooksTotal:          snapshot.BooksTotal,
		BooksNotFound:       []sync.BookNotFoundInfo{},
		Mismatches:          []mismatch.BookMismatch{},
	}
}

// applyLiveSnapshotToStatus keeps the returned status state aligned with a
// terminal snapshot that the sync service has already recorded. Sync.Service
// updates its snapshot state in a deferred cleanup just before performSync
// publishes the profile status, so a read in that small window must not report
// a completed snapshot as still syncing. LastSync remains sourced from the
// stored profile status until publication supplies the new timestamp.
func applyLiveSnapshotToStatus(status *SyncProfileStatus, snapshot sync.SyncSnapshot) {
	if status == nil {
		return
	}
	if status.Status == "syncing" && snapshot.State == "completed" {
		status.Status = "completed"
		status.Progress = "Sync completed successfully"
	}
	applySnapshotToStatus(status, snapshot)
}

func cloneProfileStatus(status *SyncProfileStatus) *SyncProfileStatus {
	if status == nil {
		return nil
	}
	copyOf := *status
	if status.LastSync != nil {
		lastSync := *status.LastSync
		copyOf.LastSync = &lastSync
	}
	copyOf.BooksNotFound = append([]sync.BookNotFoundInfo(nil), status.BooksNotFound...)
	copyOf.Mismatches = make([]mismatch.BookMismatch, len(status.Mismatches))
	for i, book := range status.Mismatches {
		copyOf.Mismatches[i] = cloneBookMismatch(book)
	}
	copyOf.LastSyncSummary = cloneSyncSummary(status.LastSyncSummary)
	if status.Snapshot != nil {
		copyOf.Snapshot = cloneSyncSnapshot(*status.Snapshot)
	}
	return &copyOf
}

// updateProfileStatus updates the status for a profile
func (s *MultiUserService) updateProfileStatus(profileID string, status *SyncProfileStatus) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()
	s.profileStatuses[profileID] = cloneProfileStatus(status)
}

// timePtr returns a pointer to a time.Time value
func timePtr(t time.Time) *time.Time {
	return &t
}

// IsProfileSyncing checks if a profile is currently syncing
func (s *MultiUserService) IsProfileSyncing(profileID string) bool {
	s.syncMutex.RLock()
	defer s.syncMutex.RUnlock()

	_, exists := s.activeSyncs[profileID]
	return exists
}
