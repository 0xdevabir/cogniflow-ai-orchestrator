package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cogniflow/orchestrator/internal/entity"
	"github.com/cogniflow/orchestrator/internal/rag"
)

func newRAGServer() *Server {
	store := rag.NewMemStore()
	svc := rag.NewService(store, nil)
	return &Server{
		RAG:         svc,
		EntityStore: entity.NoopStore{},
	}
}

func TestDocsUpload_JSON(t *testing.T) {
	srv := newRAGServer()
	body, _ := json.Marshal(map[string]string{
		"text":      "The terminating party shall provide 30 days written notice. " + strings.Repeat("This sentence adds bulk so the chunker fires. ", 30),
		"title":     "NDA",
		"workspace": "test",
		"mime_type": "text/plain",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.HandleDocsUpload(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		DocID      string `json:"doc_id"`
		Title      string `json:"title"`
		ChunkCount int    `json:"chunk_count"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.DocID == "" || out.ChunkCount == 0 || out.Title != "NDA" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestDocsUpload_Multipart(t *testing.T) {
	srv := newRAGServer()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "hello.txt")
	fw.Write([]byte(strings.Repeat("Confidentiality obligations survive for five years. ", 30)))
	mw.WriteField("title", "MSA")
	mw.WriteField("workspace", "ws1")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/docs", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	srv.HandleDocsRoute(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDocsUpload_RejectsEmpty(t *testing.T) {
	srv := newRAGServer()
	body, _ := json.Marshal(map[string]string{"text": "  "})
	req := httptest.NewRequest(http.MethodPost, "/v1/docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.HandleDocsUpload(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDocsRoute_GETReturnsList(t *testing.T) {
	srv := newRAGServer()
	// Seed a doc.
	body, _ := json.Marshal(map[string]string{
		"text":      strings.Repeat("Some content. ", 50),
		"title":     "Doc1",
		"workspace": "wsx",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.HandleDocsUpload(httptest.NewRecorder(), req)

	// Now list.
	listReq := httptest.NewRequest(http.MethodGet, "/v1/docs?workspace=wsx", nil)
	rr := httptest.NewRecorder()
	srv.HandleDocsRoute(rr, listReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Documents []rag.Document `json:"documents"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Documents) == 0 {
		t.Fatalf("expected at least 1 doc, got 0")
	}
	if out.Documents[0].WorkspaceID != "wsx" {
		t.Fatalf("workspace mismatch: %s", out.Documents[0].WorkspaceID)
	}
}

func TestDocsRoute_DELETE(t *testing.T) {
	srv := newRAGServer()
	body, _ := json.Marshal(map[string]string{
		"text":      strings.Repeat("Delete me. ", 30),
		"title":     "ToDelete",
		"workspace": "ws",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.HandleDocsUpload(rr, req)
	var out struct {
		DocID string `json:"doc_id"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&out)

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/docs/"+out.DocID, nil)
	delRR := httptest.NewRecorder()
	srv.HandleDocsRoute(delRR, delReq)
	if delRR.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", delRR.Code, delRR.Body.String())
	}
}

func TestDocsRoute_NoRAGReturns503(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/v1/docs", nil)
	rr := httptest.NewRecorder()
	srv.HandleDocsRoute(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}
