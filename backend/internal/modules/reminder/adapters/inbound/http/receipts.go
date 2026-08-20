package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// maxReceiptBodyBytes caps the receipt webhook body at 64 KiB.
const maxReceiptBodyBytes = 64 * 1024

// receiptSignatureHeader carries the hex HMAC-SHA256 of the raw request body
// keyed with the shared secret.
const receiptSignatureHeader = "X-Receipt-Signature"

// ParseGenericReceipt parses the provider-agnostic receipt shape
// {"providerMessageId": string, "delivered": bool, "errorCode"?: string}.
// A receipt without a providerMessageId cannot be matched to a delivery and
// is rejected.
func ParseGenericReceipt(body []byte) (reminderdto.ReceiptPayload, error) {
	var payload struct {
		ProviderMessageID string `json:"providerMessageId"`
		Delivered         bool   `json:"delivered"`
		ErrorCode         string `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return reminderdto.ReceiptPayload{}, err
	}
	if payload.ProviderMessageID == "" {
		return reminderdto.ReceiptPayload{}, errors.New("reminder: receipt missing providerMessageId")
	}
	return reminderdto.ReceiptPayload{
		ProviderMessageID: payload.ProviderMessageID,
		Delivered:         payload.Delivered,
		ErrorCode:         payload.ErrorCode,
	}, nil
}

// recordReceipt serves POST /api/v1/webhooks/receipts/sms. The webhook
// carries no session: the raw body must be signed with the shared secret,
// and receipts are informational — unknown provider ids are acknowledged so
// providers never retry them.
func (h *Handler) recordReceipt(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReceiptBodyBytes))
	if err != nil {
		writeValidationError(w, r)
		return
	}
	if !h.signatureValid(r.Header.Get(receiptSignatureHeader), body) {
		writeError(w, r, http.StatusUnauthorized, "invalid_signature", "receipt signature is invalid")
		return
	}
	parse := h.Parse
	if parse == nil {
		parse = ParseGenericReceipt
	}
	payload, err := parse(body)
	if err != nil {
		writeValidationError(w, r)
		return
	}
	if err := h.Receipt.Handle(r.Context(), remindercommand.ReceiptRequest{
		ProviderMessageID: payload.ProviderMessageID,
		Delivered:         payload.Delivered,
		ErrorCode:         payload.ErrorCode,
	}); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// signatureValid reports whether provided is the hex HMAC-SHA256 of body
// keyed with the handler's shared secret. The comparison is constant-time;
// a missing header or undecodable hex value is simply invalid.
func (h *Handler) signatureValid(provided string, body []byte) bool {
	if provided == "" {
		return false
	}
	providedMAC, err := hex.DecodeString(provided)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.Secret))
	mac.Write(body)
	return hmac.Equal(providedMAC, mac.Sum(nil))
}
