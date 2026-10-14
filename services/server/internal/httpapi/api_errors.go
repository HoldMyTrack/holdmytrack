package httpapi

import (
	"net/http"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// Every error the JSON API and the tiles send is one shape (ADR-0045): `error`, a stable code a
// client branches on, and `message`, text for a person. The HTML pages render their own.

type apiErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// writeError sends the error body with status.
func writeError(w http.ResponseWriter, status int, code, message string) {
	// As http.Error does: a Content-Length set for a body that was going to be sent would be
	// wrong for this one.
	w.Header().Del("Content-Length")
	writeJSON(w, status, apiErrorBody{Error: code, Message: message})
}

// writeAPIError is http.Error's counterpart for a message with no translation: the code is the
// status's generic one (statusErrorCode).
func writeAPIError(w http.ResponseWriter, message string, status int) {
	writeError(w, status, statusErrorCode(status), message)
}

// httpErrorT sends the message key in the request's language, the key without its "error."
// prefix as the code: error.photo_no_track is photo_no_track. Renaming a key a client branches
// on renames its code.
func httpErrorT(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeError(w, status, errorCodeFromKey(key, status), i18n.Get(requestLang(r)).T(key, args...))
}

// writeAccountFailure sends an accountError the way httpErrorT sends a key.
func writeAccountFailure(w http.ResponseWriter, r *http.Request, ae *accountError) {
	httpErrorT(w, r, ae.status, ae.key, ae.args...)
}

// errorCodeFromKey is a message key's code: the key without its "error." prefix, or the
// status's generic code for a key outside that namespace.
func errorCodeFromKey(key string, status int) string {
	if code, ok := strings.CutPrefix(key, "error."); ok && code != "" {
		return code
	}
	return statusErrorCode(status)
}

// statusErrorCode is the code of an error that has none of its own.
func statusErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusGone:
		return "gone"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_type"
	case http.StatusUnprocessableEntity:
		return "unprocessable"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	}
	return "internal"
}

// isAPIPath reports a path whose errors take this shape rather than a page's.
func isAPIPath(path string) bool {
	return strings.HasPrefix(path, apiPrefix+"/") || strings.HasPrefix(path, tilesPrefix+"/")
}
