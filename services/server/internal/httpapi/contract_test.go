package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// The API contract, api/openapi.yaml, is enforced here rather than by generating handlers from
// it: dbTest.do and dbTest.send check every exchange a test makes on a path the file describes,
// so a handler whose real JSON stops matching the contract fails its own tests. A path the file
// doesn't describe yet goes unchecked.

const contractPath = "../../api/openapi.yaml"

var (
	contractOnce   sync.Once
	contractRouter routers.Router
	contractErr    error
)

func loadContract() (routers.Router, error) {
	contractOnce.Do(func() {
		// Lenient on version bits: ids are any hex UUID, not only RFC 4122 ones.
		openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(uuidPattern.String()))
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromFile(contractPath)
		if err != nil {
			contractErr = err
			return
		}
		if err := doc.Validate(loader.Context); err != nil {
			contractErr = err
			return
		}
		// checkContract matches paths with apiPrefix already cut, which the router can't do
		// for a relative servers entry.
		doc.Servers = nil
		contractRouter, contractErr = legacy.NewRouter(doc)
	})
	return contractRouter, contractErr
}

// TestContractIsValid fails on a malformed api/openapi.yaml, with or without a database.
func TestContractIsValid(t *testing.T) {
	if _, err := loadContract(); err != nil {
		t.Fatal(err)
	}
}

// serveChecked runs h on req and checks the exchange against the contract, as dbTest.do does —
// for a test that calls a handler directly, or the server without a dbTest.
func serveChecked(t *testing.T, h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			t.Fatalf("read request body: %v", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	checkContract(t, req, body, rec)
	return rec
}

// checkContract checks one exchange against the contract: the response always, the request
// only when the server accepted it — a test sending a body the server must refuse is not a
// contract violation.
func checkContract(t *testing.T, req *http.Request, reqBody []byte, rec *httptest.ResponseRecorder) {
	t.Helper()
	router, err := loadContract()
	if err != nil {
		t.Fatalf("load %s: %v", contractPath, err)
	}
	path, ok := strings.CutPrefix(req.URL.Path, apiPrefix+"/")
	if !ok {
		return
	}
	probe := req.Clone(context.Background())
	probe.URL.Path = "/" + path
	probe.Body = io.NopCloser(bytes.NewReader(reqBody))
	route, params, err := router.FindRoute(probe)
	var routeErr *routers.RouteError
	if errors.As(err, &routeErr) && (routeErr.Reason == routers.ErrPathNotFound.Error() || routeErr.Reason == routers.ErrMethodNotAllowed.Error()) {
		return
	}
	if err != nil {
		t.Fatalf("contract: route %s %s: %v", req.Method, req.URL.Path, err)
	}
	opts := &openapi3filter.Options{
		AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
		IncludeResponseStatus: true,
		MultiError:            true,
	}
	in := &openapi3filter.RequestValidationInput{Request: probe, PathParams: params, Route: route, Options: opts}
	if rec.Code < 400 {
		if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
			t.Errorf("contract: request %s %s doesn't match %s:\n%v", req.Method, req.URL.Path, contractPath, err)
		}
	}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Options:                opts,
	}
	out.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
		t.Errorf("contract: %d response to %s %s doesn't match %s:\n%v\nbody: %s",
			rec.Code, req.Method, req.URL.Path, contractPath, err, strings.TrimSpace(rec.Body.String()))
	}
}
