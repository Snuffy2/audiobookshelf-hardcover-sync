package multiuser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	statepkg "github.com/drallgood/audiobookshelf-hardcover-sync/internal/sync/state"
)

// ErrEditionAssociationSaveAfterRemoteSuccess distinguishes a verified
// Hardcover result from a failure to persist the local association. Callers
// can safely retry because the Hardcover create path checks for/reuses an
// existing edition before inserting another one.
var ErrEditionAssociationSaveAfterRemoteSuccess = errors.New("Hardcover edition created but local association could not be saved")

// ErrEditionCreateDryRun indicates that edition creation is disabled for a
// profile currently configured for dry run.
var ErrEditionCreateDryRun = errors.New("edition creation is disabled while the profile is in dry run")

// EditionCreateOperation performs a user-confirmed remote edition operation
// while the profile run gate and state-file lock are held. It must return a
// complete association only after Hardcover has returned a verified result.
type EditionCreateOperation func(profile *database.ProfileWithTokens) (statepkg.Association, error)

// CreateEditionWithAssociation runs an edition create and persists its
// profile-local association as one guarded operation. The callback runs after
// confirming no sync is active and after acquiring the state-file lock.
func (s *MultiUserService) CreateEditionWithAssociation(profileID, absItemID string, operation EditionCreateOperation) error {
	if profileID == "" || absItemID == "" {
		return errors.New("profile ID and ABS item ID are required")
	}
	if operation == nil {
		return errors.New("edition create operation is required")
	}

	// Track this profile operation with sync starts so shutdown and profile
	// deletion cannot race an accepted remote mutation.
	s.admissionMutex.Lock()
	if s.shuttingDown {
		s.admissionMutex.Unlock()
		return ErrServiceShuttingDown
	}
	if _, deleting := s.deletingProfiles[profileID]; deleting {
		s.admissionMutex.Unlock()
		return ErrProfileDeleting
	}
	if _, deleted := s.deletedProfiles[profileID]; deleted {
		s.admissionMutex.Unlock()
		return ErrProfileNotFound
	}
	gate := s.profileGate(profileID)
	s.startWaitGroup.Add(1)
	gate.startWaitGroup.Add(1)
	s.admissionMutex.Unlock()
	defer s.endSyncStart(gate)

	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.deleted {
		return ErrProfileNotFound
	}

	s.syncMutex.RLock()
	_, active := s.activeSyncs[profileID]
	s.syncMutex.RUnlock()
	if active {
		return fmt.Errorf("%w for profile %s", ErrSyncAlreadyActive, profileID)
	}

	profile, err := s.GetProfile(profileID)
	if err != nil {
		return fmt.Errorf("failed to load profile %s for edition creation: %w", profileID, err)
	}
	if profile == nil {
		return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
	}
	if profile.SyncConfig.DryRun {
		return ErrEditionCreateDryRun
	}
	if err := s.validatePersistedProfileStateFile(profileID, profile.SyncConfig.StateFile); err != nil {
		return fmt.Errorf("invalid persisted state file for profile %s: %w", profileID, err)
	}

	statePath := s.profileSpecificStatePath(profileID, profile.SyncConfig.StateFile)
	fileLock, err := statepkg.AcquireFileLock(statePath)
	if err != nil {
		if errors.Is(err, statepkg.ErrStateFileLocked) {
			return fmt.Errorf("%w for profile %s: %w", ErrProfileStateBusy, profileID, err)
		}
		return fmt.Errorf("failed to lock state file for profile %s: %w", profileID, err)
	}
	defer func() { _ = fileLock.Close() }()

	_, loadPath, isLegacy, err := s.profileStateSourcePath(profileID, profile.SyncConfig.StateFile)
	if err != nil {
		return fmt.Errorf("failed to locate state file for profile %s: %w", profileID, err)
	}
	if !isLegacy {
		loadPath = fileLock.StatePath()
	}
	state, err := statepkg.LoadState(loadPath)
	if err != nil {
		return fmt.Errorf("failed to load state file for profile %s: %w", profileID, err)
	}

	association, err := operation(profile)
	if err != nil {
		return err
	}
	if association.ABSItemID != absItemID {
		return fmt.Errorf("%w: edition create operation returned an association for a different ABS item", ErrEditionAssociationSaveAfterRemoteSuccess)
	}
	if err := state.SetAssociation(association); err != nil {
		return fmt.Errorf("%w: invalid confirmed edition association: %w", ErrEditionAssociationSaveAfterRemoteSuccess, err)
	}
	if err := state.Save(fileLock.StatePath()); err != nil {
		return fmt.Errorf("%w: %v", ErrEditionAssociationSaveAfterRemoteSuccess, err)
	}
	if isLegacy {
		s.backupMigratedLegacyProfileState(profileID, loadPath)
	}
	return nil
}

// AudiobookshelfNetworkTrust returns the deployment-scoped URL trust mode.
// Profile settings cannot change this policy.
func (s *MultiUserService) AudiobookshelfNetworkTrust() string {
	if s.globalConfig == nil || strings.TrimSpace(s.globalConfig.Audiobookshelf.NetworkTrust) == "" {
		return audiobookshelf.NetworkTrustAllowPrivate
	}
	return strings.TrimSpace(s.globalConfig.Audiobookshelf.NetworkTrust)
}

// NewHardcoverClient constructs a profile-token client with the same global
// endpoint and request pacing used by profile sync workers.
func (s *MultiUserService) NewHardcoverClient(token string) *hardcover.Client {
	clientConfig := hardcover.DefaultClientConfig()
	if s.globalConfig != nil {
		if s.globalConfig.Hardcover.BaseURL != "" {
			clientConfig.BaseURL = s.globalConfig.Hardcover.BaseURL
		}
		if s.globalConfig.RateLimit.Rate > 0 {
			clientConfig.RateLimit = s.globalConfig.RateLimit.Rate
		}
		if s.globalConfig.RateLimit.MaxConcurrent > 0 {
			clientConfig.MaxConcurrent = s.globalConfig.RateLimit.MaxConcurrent
		}
	}
	return hardcover.NewClientWithConfig(clientConfig, token, s.logger)
}
