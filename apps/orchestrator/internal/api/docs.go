package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cogniflow/orchestrator/internal/rag"
)

// HandleDocsUpload accepts either:
//
//   - multipart/form-data with field "file" + optional "workspace" + "title";
//   - application/json {"text": "...", "title": "...", "workspace": "...", "mime_type": "text/plain"}.
//
// On success returns {doc_id, title, chunk_count}.
func (s *Server) HandleDocsUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RAG == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "RAG not configured", "no_rag")
		return
	}

	workspaceID := defaultWorkspaceFromRequest(r, "")
	var (
		title    string
		mime     string
		text     string
		size     int
	)

	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		t, ti, sz, m, err := readMultipartUpload(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "bad_upload")
			return
		}
		text = t
		size = sz
		mime = m
		title = ti
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			Text      string `json:"text"`
			Title     string `json:"title"`
			MimeType  string `json:"mime_type"`
			Workspace string `json:"workspace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "bad_json")
			return
		}
		text = body.Text
		title = body.Title
		mime = body.MimeType
		if body.Workspace != "" {
			workspaceID = body.Workspace
		}
	default:
		writeJSONError(w, http.StatusUnsupportedMediaType, "expected multipart/form-data or application/json", "bad_media")
		return
	}

	if strings.TrimSpace(text) == "" {
		writeJSONError(w, http.StatusBadRequest, "document text is empty", "empty_doc")
		return
	}
	if title == "" {
		title = "Untitled"
	}
	if mime == "" {
		mime = "text/plain"
	}

	doc := rag.Document{
		ID:          newDocID(),
		WorkspaceID: workspaceID,
		Title:       title,
		Source:      classifySource(mime),
		MimeType:    mime,
		Size:        size,
		CreatedAt:   time.Now().UTC(),
	}
	res, err := s.RAG.Upload(r.Context(), doc, text)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "upload_failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// HandleDocsList returns all documents in the workspace.
func (s *Server) HandleDocsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RAG == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "RAG not configured", "no_rag")
		return
	}
	workspaceID := r.URL.Query().Get("workspace")
	if workspaceID == "" {
		workspaceID = defaultWorkspaceFromRequest(r, "")
	}
	docs, err := s.RAG.Store.ListDocs(r.Context(), workspaceID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

// HandleDocsDelete deletes a single document (and cascades chunks).
func (s *Server) HandleDocsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RAG == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "RAG not configured", "no_rag")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/docs/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		writeJSONError(w, http.StatusBadRequest, "doc id is required", "missing_id")
		return
	}
	// First id is the doc id; anything after is ignored so trailing slashes
	// don't 404.
	docID := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		docID = rest[:i]
	}
	if err := s.RAG.Store.DeleteDoc(r.Context(), docID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "doc_id": docID})
}

// HandleDocsRoute routes /v1/docs/... to upload / list / delete based on the
// sub-path. It's split out so main.go can mount it with a single line.
func (s *Server) HandleDocsRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/docs")
	switch {
	case path == "" || path == "/":
		switch r.Method {
		case http.MethodGet:
			s.HandleDocsList(w, r)
		case http.MethodPost:
			s.HandleDocsUpload(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		s.HandleDocsDelete(w, r)
	}
}

// --- helpers ---

func readMultipartUpload(r *http.Request) (text, title string, size int, mime string, err error) {
	const maxMemory = 32 << 20 // 32 MiB cap; adjust in Phase 8.
	if err = r.ParseMultipartForm(maxMemory); err != nil {
		return "", "", 0, "", fmt.Errorf("parse multipart: %w", err)
	}
	file, header, ferr := r.FormFile("file")
	if ferr != nil {
		return "", "", 0, "", errors.New(`multipart field "file" is required`)
	}
	defer file.Close()
	body, rerr := io.ReadAll(file)
	if rerr != nil {
		return "", "", 0, "", fmt.Errorf("read file: %w", rerr)
	}
	mime = header.Header.Get("Content-Type")
	if mime == "" {
		mime = "text/plain"
	}
	switch {
	case strings.HasPrefix(mime, "text/"),
		mime == "application/json",
		mime == "application/x-markdown",
		strings.HasSuffix(header.Filename, ".md"),
		strings.HasSuffix(header.Filename, ".txt"):
		text = string(body)
	default:
		// Phase 6: only plaintext mime types are supported. PDFs land in Phase 8
		// when the Python ml-gateway takes over parsing.
		text = string(body)
	}
	title = r.FormValue("title")
	if title == "" {
		title = header.Filename
	}
	return text, title, len(body), mime, nil
}

func classifySource(mime string) string {
	switch {
	case strings.HasPrefix(mime, "multipart/form-data"),
		mime == "text/plain",
		mime == "application/x-markdown":
		return "upload"
	case mime == "application/json":
		return "paste"
	default:
		return "upload"
	}
}

func defaultWorkspaceFromRequest(r *http.Request, override string) string {
	if override != "" {
		return override
	}
	if v := r.URL.Query().Get("workspace"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Workspace-Id"); v != "" {
		return v
	}
	return "default"
}

func newDocID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "doc_" + hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "code": code})
}