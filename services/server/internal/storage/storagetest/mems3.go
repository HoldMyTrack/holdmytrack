// Package storagetest is an in-memory object store for tests of the packages that talk to
// internal/storage.
package storagetest

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemS3 is an S3 server for tests (an http.Handler, served with httptest) that keeps what's put in it, enough for minio-go's single-part
// PUT, GET and DELETE of one object, its bucket-location lookup, and the listing and bulk delete
// storage.RemoveByPrefix makes.
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
	if key == "" {
		m.serveBucket(w, r)
		return
	}
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

// serveBucket answers the bucket-level calls: a listing (ListObjectsV2, by prefix, never
// truncated) and a bulk delete.
func (m *MemS3) serveBucket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	if _, ok := r.URL.Query()["delete"]; ok && r.Method == http.MethodPost {
		var req struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		io.WriteString(w, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
		for _, o := range req.Objects {
			delete(m.objects, o.Key)
			fmt.Fprintf(w, `<Deleted><Key>%s</Key></Deleted>`, html.EscapeString(o.Key))
		}
		io.WriteString(w, `</DeleteResult>`)
		return
	}
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	fmt.Fprintf(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><IsTruncated>false</IsTruncated>`, html.EscapeString(prefix), len(keys))
	for _, k := range keys {
		fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>%d</Size></Contents>`, html.EscapeString(k), len(m.objects[k]))
	}
	io.WriteString(w, `</ListBucketResult>`)
}

// Put stores body under key, as a PUT would — for a test that seeds the store directly.
func (m *MemS3) Put(key string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = body
}

// Has reports whether key is stored.
func (m *MemS3) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}
