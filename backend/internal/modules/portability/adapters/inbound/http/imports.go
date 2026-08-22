package http

import (
	"errors"
	"io"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// bundleField is the multipart form field that carries the export bundle.
const bundleField = "bundle"

// multipartOverheadSlack gives the body-level size guard room for the
// multipart framing around the bundle part (boundary lines, part headers).
// The exact cap is then enforced on the part's bytes themselves, so a
// bundle at exactly MaxBundleBytes still uploads cleanly.
const multipartOverheadSlack = 4096

// upload accepts a multipart export bundle upload, validates it through the
// application handler, and answers 201 with the new import's id and the
// preview the user confirms against.
//
// The size cap is enforced defensively at both layers: MaxBytesReader aborts
// an oversized body during the multipart parse, and the part's bytes are
// read through a LimitReader at MaxBundleBytes+1 so an oversized file is
// rejected without buffering the excess.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxBundleBytes+multipartOverheadSlack)
	file, _, err := r.FormFile(bundleField)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeBundleTooLarge(w, r)
			return
		}
		writeBundleInvalid(w, r)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.MaxBundleBytes+1))
	if err != nil {
		writeBundleInvalid(w, r)
		return
	}
	if int64(len(data)) > h.MaxBundleBytes {
		writeBundleTooLarge(w, r)
		return
	}
	importID, preview, err := h.Upload.Handle(r.Context(), principal, data)
	if err != nil {
		writePortabilityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"importId": importID,
		"preview":  preview,
	})
}

// get returns one import's view: the preview stored at upload, the report
// once committed, and the lazy TTL expiry.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	view, err := h.Get.Handle(r.Context(), principal.WorkspaceID, r.PathValue("importId"))
	if err != nil {
		writePortabilityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// confirm executes a pending import exactly once and returns the final
// report.
func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	report, err := h.Confirm.Handle(r.Context(), principal, r.PathValue("importId"))
	if err != nil {
		writePortabilityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// writePortabilityError maps the portability domain sentinels to stable
// error envelopes; errors.Is resolves wrapped sentinels. The committed and
// expired conflicts share the import_conflict code — the message names which
// state blocked the operation.
func writePortabilityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrUnsupportedSchemaVersion):
		writeError(w, r, http.StatusUnprocessableEntity, "unsupported_schema_version", "bundle schema version is not supported")
	case errors.Is(err, domain.ErrChecksumMismatch):
		writeError(w, r, http.StatusUnprocessableEntity, "checksum_mismatch", "bundle checksums do not match the manifest")
	case errors.Is(err, domain.ErrBundleStructure), errors.Is(err, domain.ErrRecordInvalid), errors.Is(err, domain.ErrManifestInvalid):
		writeBundleInvalid(w, r)
	case errors.Is(err, domain.ErrImportNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "import not found")
	case errors.Is(err, domain.ErrImportConflict):
		writeError(w, r, http.StatusConflict, "import_conflict", "import is already committed")
	case errors.Is(err, domain.ErrImportExpired):
		writeError(w, r, http.StatusConflict, "import_conflict", "import has expired")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeBundleInvalid(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnprocessableEntity, "bundle_invalid", "export bundle is invalid")
}

func writeBundleTooLarge(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnprocessableEntity, "bundle_too_large", "export bundle exceeds the size limit")
}
