package main

import (
	"context"
	"strings"
	"testing"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/api/audiobookshelf"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/config"
)

func TestOneTimeSyncClientEnforcesPublicOnlyTrust(t *testing.T) {
	cfg := &config.Config{}
	cfg.Audiobookshelf.URL = "https://127.0.0.1:443"
	cfg.Audiobookshelf.Token = "test-token"
	cfg.Audiobookshelf.NetworkTrust = audiobookshelf.NetworkTrustPublicOnly

	client, err := newAudiobookshelfClient(cfg)
	if err != nil {
		t.Fatalf("newAudiobookshelfClient() error = %v", err)
	}

	_, err = client.GetLibraries(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not allowed by public_only") {
		t.Fatalf("GetLibraries() error = %v, want public_only destination rejection", err)
	}
}
