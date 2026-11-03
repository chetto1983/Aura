package agui

import (
	"context"
	"net/http"
)

// SignInImageState reports preparation of the configured image without exposing Docker details.
type SignInImageState struct {
	Present bool   `json:"present"`
	Pulling bool   `json:"pulling"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SignInImages permits an explicit download, never a caller-selected image reference.
type SignInImages interface {
	ImageState(context.Context) (SignInImageState, error)
	PullImage(context.Context) (SignInImageState, error)
}

// SetSignInImages binds image preparation separately from the live desktop transport.
func (s *Server) SetSignInImages(p SignInImages) { s.signInImages = p }

func (s *Server) registerSignInImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/governance/sign-in/image", s.handleSignInImage)
	mux.HandleFunc("POST /api/governance/sign-in/image", s.handleSignInImage)
}

func (s *Server) handleSignInImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := owner(r); !ok {
		http.Error(w, "no identity on this request", http.StatusUnauthorized)
		return
	}
	if s.signInImages == nil {
		http.Error(w, "sign-in image preparation unavailable", http.StatusServiceUnavailable)
		return
	}
	var state SignInImageState
	var err error
	if r.Method == http.MethodPost {
		if !decodeMCPBody(w, r, &struct{}{}) {
			return
		}
		state, err = s.signInImages.PullImage(r.Context())
	} else {
		state, err = s.signInImages.ImageState(r.Context())
	}
	if err != nil {
		http.Error(w, "could not prepare the sign-in image; use your own browser", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, state)
}
