package multiuser

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/hardcover"
)

const requiredEditionScope = "write:catalog:append"

type editionCapabilityKey struct {
	profileID string
	tokenHash [32]byte
	epoch     uint64
}

type editionCapabilityCacheEntry struct {
	capability EditionCapability
	expiresAt  time.Time
}

type editionCapabilityCall struct {
	done       chan struct{}
	capability EditionCapability
}

// EditionCapabilityForProfile probes whether the profile's Hardcover key can
// append catalog editions. Definite results live briefly; transient failures
// are marked unverified and cached for only a few seconds.
func (s *MultiUserService) EditionCapabilityForProfile(ctx context.Context, profileID string) (EditionCapability, error) {
	gate, err := s.beginEditionWork(profileID)
	if err != nil {
		return EditionCapability{}, err
	}
	defer s.endEditionWork(gate)

	profile, err := s.GetProfile(profileID)
	if err != nil {
		return EditionCapability{}, err
	}
	if profile == nil {
		return EditionCapability{}, ErrProfileNotFound
	}
	if profile.SyncConfig.DryRun {
		return EditionCapability{CanCreate: true}, nil
	}

	s.editionCapabilityMutex.Lock()
	key := editionCapabilityKey{
		profileID: profileID,
		tokenHash: sha256.Sum256([]byte(profile.HardcoverToken)),
		epoch:     s.editionCapabilityEpoch[profileID],
	}
	now := time.Now()
	if cached, ok := s.editionCapabilities[key]; ok && now.Before(cached.expiresAt) {
		s.editionCapabilityMutex.Unlock()
		return cached.capability, nil
	}
	if call, ok := s.editionCapabilityCalls[key]; ok {
		s.editionCapabilityMutex.Unlock()
		select {
		case <-call.done:
			return call.capability, ctx.Err()
		case <-ctx.Done():
			return EditionCapability{}, ctx.Err()
		}
	}
	call := &editionCapabilityCall{done: make(chan struct{})}
	s.editionCapabilityCalls[key] = call
	s.editionCapabilityMutex.Unlock()

	// A probe is shared by concurrent callers. Keep the bounded upstream work
	// alive if the caller that started it disconnects so a canceled request
	// cannot turn a healthy coalesced request into an unverified result.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), EditionCapabilityTimeout)
	allowed, probeErr := s.newHardcoverClient(profile.HardcoverToken, profileID).ProbeEditionCreateCapability(probeCtx)
	cancel()
	capability := classifyEditionCapability(allowed, probeErr)
	ttl := EditionCapabilityUnverifiedTTL
	if capability.CanCreate || capability.MissingScope != "" {
		ttl = EditionCapabilityCacheTTL
	}

	s.editionCapabilityMutex.Lock()
	call.capability = capability
	if s.editionCapabilityEpoch[profileID] == key.epoch {
		s.editionCapabilities[key] = editionCapabilityCacheEntry{capability: capability, expiresAt: time.Now().Add(ttl)}
	}
	delete(s.editionCapabilityCalls, key)
	close(call.done)
	s.editionCapabilityMutex.Unlock()
	if ctx.Err() != nil {
		return EditionCapability{}, ctx.Err()
	}
	return capability, nil
}

func classifyEditionCapability(allowed bool, err error) EditionCapability {
	if err != nil {
		if scope, ok := hardcover.InsufficientScope(err); ok {
			if isSafeScope(scope) {
				return EditionCapability{CanCreate: false, MissingScope: scope}
			}
			return EditionCapability{CanCreate: false, MissingScope: requiredEditionScope}
		}
		return EditionCapability{CanCreate: false, Reason: "unverified"}
	}
	if allowed {
		return EditionCapability{CanCreate: true}
	}
	return EditionCapability{CanCreate: false, Reason: "unverified"}
}

func isSafeScope(scope string) bool {
	if scope == "" || len(scope) > 64 {
		return false
	}
	for _, r := range scope {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune(":._-", r) {
			continue
		}
		return false
	}
	return true
}

func (s *MultiUserService) invalidateEditionCapability(profileID string) {
	s.editionCapabilityMutex.Lock()
	s.editionCapabilityEpoch[profileID]++
	for key := range s.editionCapabilities {
		if key.profileID == profileID {
			delete(s.editionCapabilities, key)
		}
	}
	s.editionCapabilityMutex.Unlock()
}
