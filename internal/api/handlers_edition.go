package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/drallgood/audiobookshelf-hardcover-sync/internal/multiuser"
)

// maxEditionRequestBytes caps the create-edition request body.
const maxEditionRequestBytes = 64 << 10

// editionWriteDeadline is how long the response of an edition request may take
// to be written, measured from when the handler starts. The server's default
// write timeout is far shorter than an edition create, so without this a slow
// request would finish its work but the client would see a closed connection
// and retry into a duplicate. The operation deadline includes the ABS refetch,
// Hardcover resolution, duplicate checks and insertion, with a response-write
// margin.
const editionWriteDeadline = multiuser.EditionCreateTimeout + 15*time.Second

// GetEditionCapability handles GET /api/profiles/{id}/edition-capability.
func (h *Handler) GetEditionCapability(w http.ResponseWriter, r *http.Request) {
	profileID := profileIDFromRequest(r)
	if profileID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Profile ID is required")
		return
	}
	if _, authorized := h.authorizeProfileMetadata(w, r, profileID, true); !authorized {
		return
	}
	h.extendEditionWriteDeadline(w)
	capability, err := h.multiUserService.EditionCapabilityForProfile(r.Context(), profileID)
	if err != nil {
		h.writeEditionError(w, "check edition capability", profileID, err)
		return
	}
	h.writeSuccessResponse(w, capability)
}

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

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEditionRequestBytes))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeErrorResponse(w, http.StatusBadRequest, "Request body too large")
			return
		}
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var edits multiuser.EditionEdits
	objectDecoder := json.NewDecoder(bytes.NewReader(trimmed))
	objectDecoder.DisallowUnknownFields()
	if err := objectDecoder.Decode(&edits); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// The body must be exactly one JSON object.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	h.extendEditionWriteDeadline(w)
	created, err := h.multiUserService.CreateEditionFromRunBook(r.Context(), profileID, runID, bookID, edits)
	if err != nil {
		h.writeEditionError(w, "create edition", profileID, err)
		return
	}
	h.writeSuccessResponse(w, created)
}

// extendEditionWriteDeadline lifts the server's write timeout for this response
// to editionWriteDeadline. It is best effort: a ResponseWriter that cannot set a
// deadline keeps the server default, which only matters for requests that run
// longer than that default.
func (h *Handler) extendEditionWriteDeadline(w http.ResponseWriter) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(editionWriteDeadline)); err != nil {
		h.log.Debug(fmt.Sprintf("Could not extend the write deadline for an edition request: %s", err.Error()))
	}
}

// editionRequestIDs validates the path identifiers and authorizes the caller
// for the profile. The edition workflow requires write access even though
// preparing a draft itself is read-only.
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
	errors.As(err, &upstream)
	switch {
	case errors.Is(err, multiuser.ErrProfileNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Sync profile not found")
	case errors.Is(err, multiuser.ErrEditionNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Sync run or book not found")
	case errors.Is(err, multiuser.ErrEditionItemNotFound):
		h.writeErrorResponse(w, http.StatusNotFound, "Audiobookshelf item not found")
	case errors.Is(err, multiuser.ErrEditionNotEligible):
		h.writeErrorResponse(w, http.StatusConflict, "Only needs-review books that matched a Hardcover book can get a new edition")
	case errors.Is(err, multiuser.ErrEditionConflict):
		h.writeErrorResponse(w, http.StatusConflict, "An edition with this ASIN or ISBN already exists on Hardcover and could not be confirmed to belong to this book.")
	case errors.Is(err, multiuser.ErrEditionNoIdentifier):
		h.writeErrorResponse(w, http.StatusConflict, "This book has no ASIN or ISBN in Audiobookshelf, so an edition created for it could not be matched by a sync. Add an ASIN or ISBN in Audiobookshelf first.")
	case errors.Is(err, multiuser.ErrEditionInProgress):
		h.writeErrorResponse(w, http.StatusConflict, "An edition is already being created for this book")
	case errors.Is(err, multiuser.ErrEditionInsufficientScope):
		h.writeErrorResponse(w, http.StatusForbidden, "The profile's Hardcover token needs the write:catalog:append scope to create editions")
	case errors.Is(err, multiuser.ErrProfileDeleting):
		h.writeErrorResponse(w, http.StatusConflict, "Sync profile is being deleted")
	case errors.Is(err, multiuser.ErrServiceShuttingDown):
		h.writeErrorResponse(w, http.StatusServiceUnavailable, "Service is shutting down")
	case errors.As(err, &validation):
		h.writeErrorResponse(w, http.StatusUnprocessableEntity, validation.Error())
	case upstream == nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		// The request context ended; the client is no longer waiting for an
		// upstream failure response, so avoid logging it as one.
		return
	case upstream != nil:
		h.log.Error(fmt.Sprintf("Failed to %s for profile %s: %s", action, profileID, err.Error()))
		h.writeErrorResponse(w, http.StatusBadGateway, fmt.Sprintf("Could not complete the request with %s", upstream.Service))
	default:
		h.log.Error(fmt.Sprintf("Failed to %s for profile %s: %s", action, profileID, err.Error()))
		h.writeErrorResponse(w, http.StatusInternalServerError, "Failed to "+action)
	}
}
