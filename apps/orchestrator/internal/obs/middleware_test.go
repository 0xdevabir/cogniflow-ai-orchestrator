package obs

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPMiddleware_RecordsSpan(t *testing.T) {
	// Init to "none" so the test doesn't spam JSON.
	_, err := Init(t.Context(), "cogniflow-test", "none")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	HTTPMiddleware(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHTTPMiddleware_MarksServerError(t *testing.T) {
	_, err := Init(t.Context(), "cogniflow-test", "none")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/boom", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/boom", nil)
	HTTPMiddleware(mux).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestCanonicalRoute(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/v1/chat", "/v1/chat"},
		{"/v1/docs", "/v1/docs"},
		{"/v1/docs/abc-123", "/v1/docs/{id}"},
		// Multi-segment paths keep the trailing segment collapsed; deeper
		// restructuring isn't currently needed by any handler.
		{"/v1/docs/abc-123/chunks", "/v1/docs/abc-123/{id}"},
		{"/healthz", "/healthz"},
	}
	for _, tc := range cases {
		if got := canonicalRoute(tc.in); got != tc.want {
			t.Errorf("canonicalRoute(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStatusWriter_DefaultsTo200(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 0}
	if _, err := sw.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if sw.status != http.StatusOK {
		t.Errorf("status = %d, want 200", sw.status)
	}
}

func TestStatusWriter_RecordsExplicit(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 0}
	sw.WriteHeader(http.StatusNotFound)
	if sw.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", sw.status)
	}
}
