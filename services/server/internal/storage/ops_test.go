package storage

import (
	"net/url"
	"testing"
)

func TestOpClass(t *testing.T) {
	q := func(s string) url.Values { v, _ := url.ParseQuery(s); return v }
	for _, tc := range []struct {
		method, path, query string
		want                int
	}{
		{"PUT", "/app/raw/u/a.gpx", "", classA},
		{"PUT", "/app/raw/u/big.zip", "partNumber=2&uploadId=x", classA},
		{"POST", "/app/raw/u/big.zip", "uploads=", classA},
		{"POST", "/app/raw/u/big.zip", "uploadId=x", classA},
		{"GET", "/app/fog/u/14/1/2.png", "", classB},
		{"HEAD", "/app/fog/u/14/1/2.png", "", classB},
		{"HEAD", "/app", "", classB},
		{"GET", "/app/", "list-type=2&prefix=raw%2F", classA},
		{"GET", "/app", "prefix=raw%2F", classA},
		{"GET", "/app/", "location=", classB},
		{"GET", "/app/raw/u/big.zip", "uploadId=x", classA},
		{"DELETE", "/app/avatars/u", "", classNone},
		{"POST", "/app/", "delete=", classNone},
		{"PATCH", "/app/x", "", classA},
	} {
		if got := opClass(tc.method, tc.path, q(tc.query), "app"); got != tc.want {
			t.Errorf("%s %s?%s: class %d, want %d", tc.method, tc.path, tc.query, got, tc.want)
		}
	}
}

func TestTakeOps(t *testing.T) {
	TakeOps()
	opCounts[classA].Add(3)
	opCounts[classB].Add(5)
	if a, b := TakeOps(); a != 3 || b != 5 {
		t.Fatalf("TakeOps = %d, %d; want 3, 5", a, b)
	}
	ReturnOps(1, 2)
	if a, b := TakeOps(); a != 1 || b != 2 {
		t.Fatalf("after ReturnOps: %d, %d; want 1, 2", a, b)
	}
}
