// Package httpx holds the router, middleware, JSON helpers and error mapping.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

// Body size caps (API_CONTRACT §1).
const (
	DefaultBodyLimit int64 = 1 << 20 // 1 MiB
	UploadBodyLimit  int64 = 8 << 20 // 8 MiB
)

// DecodeJSON reads exactly one JSON object into dst. Unknown fields,
// trailing data, a wrong Content-Type and oversized bodies are errors.
// The body must already be wrapped by the BodyLimit middleware.
func DecodeJSON(r *http.Request, dst any) error {
	ct := r.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || mt != "application/json" {
		return apperr.UnsupportedMedia()
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return apperr.TooLarge()
		}
		return apperr.InvalidJSON("Request body must contain a single JSON object")
	}
	return nil
}

func decodeError(err error) error {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		tooBig    *http.MaxBytesError
	)
	switch {
	case errors.As(err, &tooBig):
		return apperr.TooLarge()
	case errors.Is(err, io.EOF):
		return apperr.InvalidJSON("Request body is empty")
	case errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &syntaxErr):
		return apperr.InvalidJSON("Malformed JSON")
	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			return apperr.InvalidJSON("Request body must be a JSON object")
		}
		return apperr.Validation(map[string]string{field: "has the wrong type"})
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return apperr.Validation(map[string]string{name: "unknown field"})
	default:
		return apperr.InvalidJSON("Malformed JSON")
	}
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		status = http.StatusInternalServerError
		buf.Reset()
		buf.WriteString(`{"error":"Internal server error","code":"INTERNAL"}` + "\n")
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

type errorBody struct {
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields,omitempty"`
}

var statusByKind = map[apperr.Kind]int{
	apperr.KindValidation:       http.StatusBadRequest,
	apperr.KindUnauthenticated:  http.StatusUnauthorized,
	apperr.KindForbidden:        http.StatusForbidden,
	apperr.KindNotFound:         http.StatusNotFound,
	apperr.KindConflict:         http.StatusConflict,
	apperr.KindTooLarge:         http.StatusRequestEntityTooLarge,
	apperr.KindUnsupportedMedia: http.StatusUnsupportedMediaType,
	apperr.KindRateLimited:      http.StatusTooManyRequests,
	apperr.KindUnavailable:      http.StatusServiceUnavailable,
	apperr.KindMisdirected:      http.StatusMisdirectedRequest,
	apperr.KindInternal:         http.StatusInternalServerError,
}

// WriteError maps err to the standard error body. Unknown errors become a
// generic 500; their details are logged with the request ID, never returned.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		e = apperr.Internal(err)
	}
	status, found := statusByKind[e.Kind]
	if !found {
		status = http.StatusInternalServerError
	}
	if status >= 500 && e.Kind == apperr.KindInternal {
		LoggerFrom(r.Context()).ErrorContext(r.Context(), "request failed",
			slog.String("code", e.Code), slog.Any("err", err))
	}
	if e.RetryAfter > 0 {
		secs := int(e.RetryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprint(secs))
	}
	WriteJSON(w, status, errorBody{Error: e.Message, Code: e.Code, Fields: e.Fields})
}
