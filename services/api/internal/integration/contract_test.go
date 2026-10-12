package integration

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

var (
	specOnce   sync.Once
	specRouter routers.Router
	specErr    error
)

func openAPIRouter(t *testing.T) routers.Router {
	t.Helper()
	specOnce.Do(func() {
		_, here, _, _ := runtime.Caller(0)
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromFile(filepath.Join(filepath.Dir(here), "..", "..", "api", "openapi.yaml"))
		if err != nil {
			specErr = err
			return
		}
		if err := doc.Validate(context.Background()); err != nil {
			specErr = err
			return
		}
		doc.Servers = nil
		specRouter, specErr = gorillamux.NewRouter(doc)
	})
	if specErr != nil {
		t.Fatalf("openapi.yaml: %v", specErr)
	}
	return specRouter
}

// conform checks that a recorded response matches api/openapi.yaml (the
// contract the web client is generated from).
func conform(t *testing.T, method, path string, body []byte, rec *httptest.ResponseRecorder) {
	t.Helper()
	router := openAPIRouter(t)
	req := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s is not in openapi.yaml: %v", method, path, err)
	}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in, Status: rec.Code, Header: rec.Header(),
		Body:    io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
		t.Errorf("%s %s → %d does not match openapi.yaml: %v\nbody: %.400s", method, path, rec.Code, err, rec.Body.String())
	}
}

func TestContract(t *testing.T) {
	a := newAPI(t)
	check := func(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
		rec := a.do(t, method, path, body, cookie)
		var raw []byte
		if body != nil {
			raw = mustJSON(body)
		}
		conform(t, method, path, raw, rec)
		return rec
	}
	check(http.MethodGet, "/v1/regions", nil, nil)
	check(http.MethodGet, "/v1/regions/indore/destinations", nil, nil)
	check(http.MethodGet, "/v1/shortcuts?bbox=75.8,22.6,75.9,22.7&zoom=14", nil, nil)
	check(http.MethodPost, "/v1/route", map[string]any{"from": fxA, "to": map[string]float64{"lat": 19.07, "lng": 72.87}}, nil)
	check(http.MethodGet, "/v1/auth/providers", nil, nil)
	u := a.signIn(t, "contract@example.com")
	check(http.MethodGet, "/v1/me", nil, u)
	check(http.MethodGet, "/v1/me/contributions", nil, u)
	check(http.MethodGet, "/v1/me/places", nil, u)
	check(http.MethodPost, "/v1/shortcuts/snap", map[string]float64{"lat": 23.00001, "lng": 77.00001}, nil)
	if fixtureURL() != "" {
		check(http.MethodPost, "/v1/route", map[string]any{"from": fxA, "to": fxC}, nil)
	}
}

func mustJSON(v any) []byte {
	var buf bytes.Buffer
	if err := jsonEncode(&buf, v); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
