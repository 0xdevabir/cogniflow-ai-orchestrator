package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/providers"
)

// TestHandleHealthz_OK asserts the health endpoint returns expected JSON shape.
func TestHandleHealthz_OK(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil)}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	srv.HandleHealthz(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	if !strings.Contains(rw.Header().Get("content-type"), "application/json") {
		t.Errorf("content-type = %q", rw.Header().Get("content-type"))
	}
	var body map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if _, ok := body["providers"]; !ok {
		t.Errorf("missing providers array")
	}
}

// TestHandleChat_StreamsUntilDone verifies the SSE flow with the mock provider.
func TestHandleChat_StreamsUntilDone(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil)} // no keys → mock fallback
	body := `{"prompt":"hi","model":"mock"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	rw := httptest.NewRecorder()

	srv.HandleChat(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if ct := rw.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rw.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if xab := rw.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q", xab)
	}

	// Parse the SSE stream and count event types.
	br := bytes.NewReader(rw.Body.Bytes())
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)

	var sawNodeStatus, sawDone bool
	var chunkCount int
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			ev := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			switch ev {
			case "node_status":
				sawNodeStatus = true
			case "chunk":
				chunkCount++
			case "done":
				sawDone = true
			}
		}
	}

	if !sawNodeStatus {
		t.Error("expected at least one `event: node_status` line")
	}
	if chunkCount == 0 {
		t.Error("expected at least one chunk event")
	}
	if !sawDone {
		t.Error("expected terminal `event: done` line")
	}
}

// TestHandleChat_RejectsBadJSON verifies error path for garbage body.
func TestHandleChat_RejectsBadJSON(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil)}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader("{not valid json"))
	rw := httptest.NewRecorder()

	srv.HandleChat(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == nil {
		t.Error("missing error object")
	}
}

// TestHandleChat_RejectsMissingPrompt verifies validation for empty prompt.
func TestHandleChat_RejectsMissingPrompt(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil)}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{"prompt":""}`))
	rw := httptest.NewRecorder()

	srv.HandleChat(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
}

// TestHandleChat_RejectsWrongMethod verifies HTTP method is enforced.
func TestHandleChat_RejectsWrongMethod(t *testing.T) {
	srv := &Server{Registry: providers.NewRegistry(nil)}
	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	rw := httptest.NewRecorder()

	srv.HandleChat(rw, req)

	if rw.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rw.Code)
	}
}

// silence unused import warnings on platforms where bytes is unused
var _ = bytes.NewReader
