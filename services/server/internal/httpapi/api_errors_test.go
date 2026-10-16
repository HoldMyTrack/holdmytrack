package httpapi

// Covers docs/SPEC.md §1.5 (the JSON error shape).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A failure with its own message key gets the key's name as its code; anything else, its
// status's generic code (ADR-0045).
func TestErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		key    string
		status int
		want   string
	}{
		{"error.photo_no_track", http.StatusConflict, "photo_no_track"},
		{"error.demo_read_only", http.StatusForbidden, "demo_read_only"},
		{"signin.google_failed", http.StatusUnauthorized, "unauthenticated"},
		{"error.", http.StatusBadRequest, "invalid_request"},
	} {
		if got := errorCodeFromKey(tc.key, tc.status); got != tc.want {
			t.Errorf("errorCodeFromKey(%q, %d) = %q, want %q", tc.key, tc.status, got, tc.want)
		}
	}
	for status, want := range map[int]string{
		http.StatusBadRequest:          "invalid_request",
		http.StatusNotFound:            "not_found",
		http.StatusTooManyRequests:     "rate_limited",
		http.StatusServiceUnavailable:  "unavailable",
		http.StatusInternalServerError: "internal",
		http.StatusTeapot:              "internal",
	} {
		if got := statusErrorCode(status); got != want {
			t.Errorf("statusErrorCode(%d) = %q, want %q", status, got, want)
		}
	}
}

// An error is JSON with both fields, and drops a Content-Length set for the body it replaces.
func TestWriteAPIError(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Length", "1234")
	writeAPIError(rec, "activity not found", http.StatusNotFound)
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" ||
		rec.Header().Get("Content-Length") != "" || body != (apiErrorBody{Error: "not_found", Message: "activity not found"}) {
		t.Fatalf("got %d %v %+v", rec.Code, rec.Header(), body)
	}
}

// Errors under the API's prefixes take its shape; a page's stay HTML.
func TestIsAPIPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1/activities":      true,
		"/tiles/v1/fog/1/2/3": true,
		"/v1":                 false,
		"/settings":           false,
		"/":                   false,
	} {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}
