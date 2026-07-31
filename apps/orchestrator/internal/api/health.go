package api

import "net/http"

// HandleHealthz reports the service status + which providers are registered.
//
// GET /healthz → 200 application/json
//
// Response shape:
//
//	{
//	  "status": "ok",
//	  "service": "cogniflow-orchestrator",
//	  "phase": 1,
//	  "providers": ["mock","openai","anthropic"]
//	}
func (s *Server) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	out := `{"status":"ok","service":"cogniflow-orchestrator","phase":1,"providers":[`
	out += joinQuoted(s.Registry.List())
	out += `]}`
	_, _ = w.Write([]byte(out))
}

func joinQuoted(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += `"` + s + `"`
	}
	return out
}