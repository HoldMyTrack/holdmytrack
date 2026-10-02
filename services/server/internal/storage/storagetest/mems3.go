// Package storagetest is an in-memory object store for tests of the packages that talk to
// internal/storage.
package storagetest

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemS3 is an S3 server for tests (an http.Handler, served with httptest) that keeps what's put in it, enough for minio-go's single-part
// PUT, GET and DELETE of one object and its bucket-location lookup.
type MemS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

// New is an empty store.
func New() *MemS3 { return &MemS3{objects: map[string][]byte{}} }

func (m *MemS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["location"]; ok {
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/test/")
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			body = decodeAWSChunked(body)
		}
		m.objects[key] = body
		w.Header().Set("ETag", `"0"`)
	case http.MethodGet, http.MethodHead:
		body, ok := m.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		w.Header().Set("ETag", `"0"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			w.Write(body)
		}
	case http.MethodDelete:
		delete(m.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodeAWSChunked strips the chunk framing minio-go signs a plain-HTTP upload with:
// "<hex size>;chunk-signature=…\r\n<data>\r\n", ending with a zero-size chunk.
func decodeAWSChunked(body []byte) []byte {
	var out []byte
	for len(body) > 0 {
		line, rest, ok := bytes.Cut(body, []byte("\r\n"))
		if !ok {
			break
		}
		sizeHex, _, _ := bytes.Cut(line, []byte(";"))
		size, err := strconv.ParseInt(string(sizeHex), 16, 64)
		if err != nil || size == 0 || int64(len(rest)) < size {
			break
		}
		out = append(out, rest[:size]...)
		body = bytes.TrimPrefix(rest[size:], []byte("\r\n"))
	}
	return out
}

// Has reports whether key is stored.
func (m *MemS3) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}
