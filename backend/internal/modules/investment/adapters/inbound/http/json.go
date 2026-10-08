package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type errorResponse struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	CorrelationID string `json:"correlationId"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, e := json.Marshal(dto.Public(value))
	if e != nil {
		status = 503
		body = []byte(`{"code":"service_unavailable","message":"response unavailable","correlationId":""}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
func writeError(w http.ResponseWriter, r *http.Request, e error) {
	status, code := 503, "service_unavailable"
	for _, known := range []error{domain.ErrInvalidInput, domain.ErrOverflow, domain.ErrNotFound, domain.ErrDataNotConfigured, domain.ErrDataStale, domain.ErrInsufficientUniverse, domain.ErrFactorUnavailable, domain.ErrCorporateActionIncomplete, domain.ErrInsufficientCash, domain.ErrRiskLimit, domain.ErrAutomationPaused, domain.ErrVersionConflict, domain.ErrIdempotencyConflict, domain.ErrOrderNotCancellable} {
		if errors.Is(e, known) {
			code = known.Error()
			status = 422
			switch known {
			case domain.ErrNotFound:
				status = 404
			case domain.ErrVersionConflict, domain.ErrIdempotencyConflict, domain.ErrOrderNotCancellable:
				status = 409
			}
			break
		}
	}
	if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		status = 503
		code = "service_unavailable"
	}
	writeJSON(w, status, errorResponse{Code: code, Message: "request could not be completed", CorrelationID: observability.CorrelationID(r.Context())})
}
func requestFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for n := 0; n < t.NumField(); n++ {
		f := t.Field(n)
		if f.PkgPath != "" {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" {
			for k, v := range requestFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if tag == "" {
			tag = dto.PublicName(f.Name)
		}
		out[tag] = f.Type
	}
	return out
}
func validateShape(value any, t reflect.Type) bool {
	if value == nil {
		return t.Kind() == reflect.Pointer
	}
	if t.Kind() == reflect.Pointer {
		return validateShape(value, t.Elem())
	}
	if t == reflect.TypeOf(time.Time{}) {
		_, ok := value.(string)
		return ok
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := value.(map[string]any)
		if !ok {
			return false
		}
		fields := requestFields(t)
		for key, v := range obj {
			field, ok := fields[key]
			if !ok || !validateShape(v, field) {
				return false
			}
		}
		return true
	case reflect.Array, reflect.Slice:
		a, ok := value.([]any)
		if !ok || (t.Kind() == reflect.Array && len(a) != t.Len()) {
			return false
		}
		for _, v := range a {
			if !validateShape(v, t.Elem()) {
				return false
			}
		}
		return true
	case reflect.String:
		_, ok := value.(string)
		return ok
	case reflect.Bool:
		_, ok := value.(bool)
		return ok
	case reflect.Int, reflect.Int64, reflect.Float64:
		_, ok := value.(json.Number)
		return ok
	default:
		return false
	}
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	raw, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil || len(raw) > 65536 {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var obj any
	if e = d.Decode(&obj); e != nil || !validateShape(obj, reflect.TypeOf(dst).Elem()) {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	root, ok := obj.(map[string]any)
	if !ok {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	for _, key := range required {
		if _, ok = root[key]; !ok {
			writeError(w, r, domain.ErrInvalidInput)
			return false
		}
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(dst); e != nil {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	return true
}
