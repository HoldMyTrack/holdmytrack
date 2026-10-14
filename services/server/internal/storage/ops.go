package storage

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
)

// Op classes as R2 bills them (IMPLEMENTATION.md §5.7): Class A are writes and listings,
// Class B reads, and deletes are free. Counted per request on the wire, so a listing's every
// page, a multipart upload's every part and a resumed download's every range read count, as
// they do on the bill.
const (
	classNone = iota
	classA
	classB
)

var opCounts [3]atomic.Int64

// classLabel is a class's holdmytrack_r2_operations_total label.
var classLabel = [3]string{"", "A", "B"}

// TakeOps returns the Class A and Class B requests this process made since the last call, and
// starts the counts again from zero: what internal/budget adds to the day's total.
func TakeOps() (a, b int64) {
	return opCounts[classA].Swap(0), opCounts[classB].Swap(0)
}

// ReturnOps adds back counts TakeOps handed out that couldn't be stored, for the next try.
func ReturnOps(a, b int64) {
	opCounts[classA].Add(a)
	opCounts[classB].Add(b)
}

// opClass is how R2 bills one S3 request to bucket, from its method and URL (path-style, as
// minio-go addresses an endpoint that isn't AWS's). Anything this doesn't recognize counts as
// Class A, the dearer, so an unknown request is never undercounted.
func opClass(method, path string, query url.Values, bucket string) int {
	key := strings.TrimPrefix(strings.TrimPrefix(path, "/"), bucket)
	key = strings.TrimPrefix(key, "/")
	switch method {
	case http.MethodDelete:
		return classNone // DeleteObject, AbortMultipartUpload
	case http.MethodPost:
		if query.Has("delete") {
			return classNone // DeleteObjects
		}
		return classA // CreateMultipartUpload, CompleteMultipartUpload
	case http.MethodPut:
		return classA // PutObject, UploadPart, PutBucket
	case http.MethodHead:
		return classB // HeadObject, HeadBucket
	case http.MethodGet:
		switch {
		case query.Has("uploadId") || query.Has("uploads"):
			return classA // ListParts, ListMultipartUploads
		case key == "" && (query.Has("location") || query.Has("cors") || query.Has("encryption") || query.Has("lifecycle")):
			return classB // GetBucketLocation and the other bucket reads
		case key == "":
			return classA // ListObjects
		default:
			return classB // GetObject
		}
	}
	return classA
}

// countingTransport counts each request it sends by opClass, for the budget and for Grafana's
// holdmytrack_r2_operations_total.
type countingTransport struct {
	next   http.RoundTripper
	bucket string
}

func (t countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if c := opClass(req.Method, req.URL.Path, req.URL.Query(), t.bucket); c != classNone {
		opCounts[c].Add(1)
		metrics.R2Operations.WithLabelValues(classLabel[c]).Inc()
	}
	return t.next.RoundTrip(req)
}
