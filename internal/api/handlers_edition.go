package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/multiuser"
)

// maxEditionRequestBytes caps the create-edition request body.
const maxEditionRequestBytes = 64 << 10

// GetEditionDraft handles
// GET /api/profiles/{id}/runs/{runID}/books/{bookID}/edition-draft.
//
// It returns a previewable Hardcover edition built from the Audiobookshelf item
// behind a needs-review record. The target Hardcover book comes from the run
// record, never from the request.
func (h *Handler) GetEditionDraft(w http.ResponseWriter, r *http.Request) {
	profileID, runID, bookID, ok := h.editionRequestIDs(w, r)
	if !ok {
		return
	}

	draft, err := h.multiUserService.PrepareEditionDraft(r.Context(), profileID, runID, bookID)
	if err != nil {
		h.writeEditionError(w, "prepare edition draft", profileID, err)
		return
	}
	h.writeSuccessResponse(w, draft)
}

// CreateEdition handles
// POST /api/profiles/{id}/runs/{runID}/books/{bookID}/edition.
//
// The body carries only the editable edition fields. Unknown fields, including
// book_id and image_url, are rejected so a request cannot retarget the edition
// or choose the image URL that receives the Audiobookshelf token.
func (h *Handler) CreateEdition(w http.ResponseWriter, r *http.Request) {
	profileID, runID, bookID, ok := h.editionRequestIDs(w, r)
	if !ok {
		return
	}

	var edits multiuser.EditionEdits
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEditionRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&edits); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeErrorResponse(w, http.StatusBadRequest, "Request body too large")
			return
		}
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// The body must be exactly one JSON object.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	created, err := h.multiUserService.CreateEditionFromRunBook(r.Context(), profileID, runID, bookID, edits)
	if err != nil {
		h.writeEditionError(w, "create edition", profileID, err)
		return
	}
	h.writeSuccessResponse(w, created)
}

// editionRequestIDs validates the path identifiers and authorizes the caller
// for the profile. Both edition endpoints count as mutations for authorization
// because they can trigger rate-limited Hardcover work.
func (h *Handler) editionRequestIDs(w http.ResponseWriter, r *http.Request) (profileID, runID, bookID string, ok bool) {
	profileID = profileIDFromRequest(r)
	runID = r.PathValue("runID")
	bookID = r.PathValue("bookID")
	if profileID == "" || runID == "" || bookID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Profile ID, run ID, and book ID are required")
		return "", "", "", false
	}
	if _, authorized := h.authorizeProfileMetadata(w, r, profileID, true); !authorized {
		return "", "", "", false
	}
	return profileID, runID, bookID, true
}

// writeEditionError maps edition service errors to HTTP responses. Upstream and
// internal failures are logged and reported generically so remote details and
// credentials never reach the client.
func (h *Handler) writeEditionError(w http.ResponseWriter, action, profileID string, err error) {
	var validation *multiuser.EditionValidationError
	var upstream *multiuser.EditionUpstreamError
	switch {
	case errors.Is(err, multiuser.ErrProfileNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Sync profile not found")
	case errors.Is(err, multiuser.ErrEditionNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Sync run or book not found")
	case errors.Is(err, multiuser.ErrEditionItemNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Audiobookshelf item not found")
	case errors.Is(err, multiuser.ErrEditionNotEligible):
		h.writeErrorResponse(w, http.StatusConflict, "Only needs-review books that matched a Hardcover book can get a new edition")
	case errors.Is(err, multiuser.ErrEditionInProgress):
		h.writeErrorResponse(w, http.StatusConflict, "An edition is already being created for this book")
	case errors.Is(err, multiuser.ErrProfileDeleting):
		h.writeErrorResponse(w, http.StatusConflict, "Sync profile is being deleted")
	case errors.Is(err, multiuser.ErrServiceShuttingDown):
		h.writeErrorResponse(w, http.StatusServiceUnavailable, "Service is shutting down")
	case errors.As(err, &validation):
		h.writeErrorResponse(w, http.StatusUnprocessableEntity, validation.Error())
	case errors.As(err, &upstream):
		h.log.Error(fmt.Sprintf("Failed to %s for profile %s: %s", action, profileID, err.Error()))
		h.writeErrorResponse(w, http.StatusBadGateway, fmt.Sprintf("Could not complete the request with %s", upstream.Service))
	default:
		h.log.Error(fmt.Sprintf("Failed to %s for profile %s: %s", action, profileID, err.Error()))
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to "+action)
	}
}
