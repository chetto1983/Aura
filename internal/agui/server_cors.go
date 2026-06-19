package agui

import "net/http"

// withCORS wraps the mux when CORSPermissive is enabled and answers preflight
// requests before they reach the route handlers.
func (s *Server) withCORS(next http.Handler) http.Handler {
	if !s.cfg.CORSPermissive {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
