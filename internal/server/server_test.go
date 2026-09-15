package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/auth"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/config"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/crypto"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/database"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/logger"
	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/multiuser"
	"github.com/stretchr/testify/require"
)

type routeTestFixture struct {
	repo   *database.Repository
	server *Server
}

func newRouteTestFixture(t *testing.T, authEnabled bool) *routeTestFixture {
	t.Helper()
	logger.ForceSetup(logger.Config{Level: "error", Format: logger.FormatJSON, Output: io.Discard})

	dataDir := t.TempDir()
	db, err := database.NewDatabase(&database.DatabaseConfig{
		Type: database.DatabaseTypeSQLite,
		Path: filepath.Join(dataDir, "server-route-test.db"),
	}, logger.Get())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	encryptor, err := crypto.NewEncryptionManagerWithDataDir(dataDir, logger.Get())
	require.NoError(t, err)
	repo := database.NewRepository(db, encryptor, logger.Get())
	cfg := config.DefaultConfig()
	cfg.Paths.DataDir = dataDir
	multiUserService := multiuser.NewMultiUserService(repo, cfg, logger.Get())

	authConfig := auth.DefaultAuthConfig()
	authConfig.Enabled = authEnabled
	authService, err := auth.NewAuthService(db.GetDB(), authConfig, logger.Get())
	require.NoError(t, err)

	return &routeTestFixture{
		repo:   repo,
		server: New("", multiUserService, authService, logger.Get()),
	}
}

func (f *routeTestFixture) request(method, path string, body []byte) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	f.server.server.Handler.ServeHTTP(recorder, request)
	return recorder
}

func TestServerRoutesPreserveEncodedLegacyProfileIDForCRUD(t *testing.T) {
	fixture := newRouteTestFixture(t, false)
	legacyID := "legacy/profile;id"
	require.NoError(t, fixture.repo.CreateProfile(
		legacyID,
		"Legacy profile",
		"http://audiobookshelf.invalid",
		"abs-token",
		"hardcover-token",
		database.SyncConfigData{ProcessUnreadBooks: true, DryRun: true},
	))
	escapedID := url.PathEscape(legacyID)

	profileResponse := fixture.request(http.MethodGet, "/api/profiles/"+escapedID, nil)
	require.Equal(t, http.StatusOK, profileResponse.Code, profileResponse.Body.String())
	var profilePayload struct {
		Success bool `json:"success"`
		Data    struct {
			Profile struct {
				ID string `json:"id"`
			} `json:"profile"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(profileResponse.Body.Bytes(), &profilePayload))
	require.True(t, profilePayload.Success)
	require.Equal(t, legacyID, profilePayload.Data.Profile.ID)

	statusResponse := fixture.request(http.MethodGet, "/api/profiles/"+escapedID+"/status", nil)
	require.Equal(t, http.StatusOK, statusResponse.Code, statusResponse.Body.String())
	var statusPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ProfileID string `json:"profile_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(statusResponse.Body.Bytes(), &statusPayload))
	require.True(t, statusPayload.Success)
	require.Equal(t, legacyID, statusPayload.Data.ProfileID)

	summaryResponse := fixture.request(http.MethodGet, "/api/profiles/"+escapedID+"/summary", nil)
	require.Equal(t, http.StatusOK, summaryResponse.Code, summaryResponse.Body.String())
	detailsResponse := fixture.request(http.MethodGet, "/api/profiles/"+escapedID+"/runs/unknown/details", nil)
	require.Equal(t, http.StatusNotFound, detailsResponse.Code, detailsResponse.Body.String())

	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "profile update",
			method: http.MethodPut,
			path:   "/api/profiles/" + escapedID,
			body:   `{"name":"Updated profile"}`,
		},
		{
			name:   "config update",
			method: http.MethodPut,
			path:   "/api/profiles/" + escapedID + "/config",
			body:   `{"audiobookshelf_url":"http://updated.invalid","sync_config":{"process_unread_books":true}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := fixture.request(test.method, test.path, []byte(test.body))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		})
	}

	deleteResponse := fixture.request(http.MethodDelete, "/api/profiles/"+escapedID, nil)
	require.Equal(t, http.StatusOK, deleteResponse.Code, deleteResponse.Body.String())
	missingResponse := fixture.request(http.MethodGet, "/api/profiles/"+escapedID, nil)
	require.Equal(t, http.StatusNotFound, missingResponse.Code, missingResponse.Body.String())
}

func TestServerProfileRoutesUseAuthMiddleware(t *testing.T) {
	fixture := newRouteTestFixture(t, true)
	response := fixture.request(http.MethodGet, "/api/profiles/example", nil)
	require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
}
