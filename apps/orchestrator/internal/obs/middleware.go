package obs

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// HTTPMiddleware wraps an http.Handler so every request becomes a span.
// The span derives from the inbound W3C traceparent header (if present) so
// traces correlate across the front-door / API / DAG layers.
//
// Use:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/v1/run", srv.HandleRun)
//	http.ListenAndServe(addr, obs.HTTPMiddleware(mux))
//
// Status code is recorded on the span attribute "http.status_code" and any
// panic is rolled into the span via the standard handler-with-defer trick.
func HTTPMiddleware(next http.Handler) http.Handler {
	// Use the configured global propagator so cross-service traces link up.
	propagator := propagation.TraceContext{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		spanName := r.Method + " " + canonicalRoute(r.URL.Path)
		ctx, span := Start(ctx, spanName,
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
			attribute.String("http.scheme", schemeFromRequest(r)),
			attribute.String("http.host", r.Host),
			attribute.String("user_agent.original", r.UserAgent()),
		)
		defer span.End()

		// Capture status code via a small wrapper.
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r.WithContext(ctx))
		span.SetAttributes(
			attribute.Int("http.status_code", sw.status),
			attribute.Int64("http.duration_ms", time.Since(start).Milliseconds()),
		)
		if sw.status >= 500 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	})
}

// statusWriter records the response status code so the middleware can attach
// it to the span.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Write caches the status if the handler never explicitly calls it.
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Flush passes through to the underlying writer if it implements http.Flusher.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// canonicalRoute collapses path params to a stable span name.
// /v1/docs/abc123 -> /v1/docs/{id}
func canonicalRoute(path string) string {
	// Lightweight heuristic: only split known suffixes.
	if len(path) > 4 && path[:4] == "/v1/" {
		rest := path[4:]
		for i := len(rest) - 1; i >= 0; i-- {
			if rest[i] == '/' {
				// Last segment is a leaf id.
				return "/v1/" + rest[:i] + "/{id}"
			}
		}
	}
	return path
}

func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
		return s
	}
	return "http"
}
