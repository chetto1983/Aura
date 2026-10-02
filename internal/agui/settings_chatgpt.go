package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/chetto1983/aura/internal/chatgptplan"
)

// ChatGPTPlanService is the identity-scoped connection and credential port.
type ChatGPTPlanService interface {
	Status(context.Context, string) (chatgptplan.Status, error)
	AccessToken(context.Context) (string, error)
	Disconnect(context.Context, string) error
}

// ChatGPTBrowserLogin binds a live login window to its authenticated owner.
type ChatGPTBrowserLogin interface {
	Start(context.Context, string, string) (chatgptplan.Flow, error)
	Cancel(context.Context, string, string) error
	Owns(context.Context, string, string) bool
}

var chatGPTLoginRoutePattern = regexp.MustCompile(`^/browser/chatgpt-[a-f0-9]{24}$`)

// SetChatGPTPlan wires the protected account connection.
func (s *Server) SetChatGPTPlan(service ChatGPTPlanService) {
	s.chatGPTPlan = service
}

// SetChatGPTBrowserLogin wires the VM-local browser and callback lifecycle.
func (s *Server) SetChatGPTBrowserLogin(login ChatGPTBrowserLogin) {
	s.chatGPTBrowser = login
}

// ChatGPTPlanRoutes returns the authenticated settings patterns for parent mounts.
func ChatGPTPlanRoutes() []string {
	return []string{
		"POST /api/settings/chatgpt/login",
		"DELETE /api/settings/chatgpt/login",
		"GET /api/settings/chatgpt/status",
		"DELETE /api/settings/chatgpt",
	}
}

func (s *Server) registerChatGPTPlanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/settings/chatgpt/login", s.handleChatGPTLogin)
	mux.HandleFunc("DELETE /api/settings/chatgpt/login", s.handleChatGPTCancel)
	mux.HandleFunc("GET /api/settings/chatgpt/status", s.handleChatGPTStatus)
	mux.HandleFunc("DELETE /api/settings/chatgpt", s.handleChatGPTDisconnect)
}

func (s *Server) chatGPTAvailable(w http.ResponseWriter) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.chatGPTPlan == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT connection unavailable"})
		return false
	}
	return true
}

func (s *Server) handleChatGPTLogin(w http.ResponseWriter, r *http.Request) {
	if !s.chatGPTAvailable(w) {
		return
	}
	if s.chatGPTBrowser == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT sign-in browser unavailable"})
		return
	}
	flow, err := s.chatGPTBrowser.Start(scopedCtx(r.Context()), scopedIdentityID(r.Context()), r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "Unable to start ChatGPT sign-in"})
		return
	}
	writeJSON(w, flow)
}

func (s *Server) handleChatGPTCancel(w http.ResponseWriter, r *http.Request) {
	if !s.chatGPTAvailable(w) {
		return
	}
	var body struct {
		AuthURL string `json:"auth_url"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil || !chatGPTLoginRoutePattern.MatchString(body.AuthURL) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Valid ChatGPT sign-in required"})
		return
	}
	if s.chatGPTBrowser == nil || s.chatGPTBrowser.Cancel(scopedCtx(r.Context()), scopedIdentityID(r.Context()), body.AuthURL) != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "Unable to cancel ChatGPT sign-in"})
		return
	}
	writeJSON(w, map[string]bool{"cancelled": true})
}

func (s *Server) handleChatGPTStatus(w http.ResponseWriter, r *http.Request) {
	if !s.chatGPTAvailable(w) {
		return
	}
	status, err := s.chatGPTPlan.Status(scopedCtx(r.Context()), scopedIdentityID(r.Context()))
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "Unable to read ChatGPT connection"})
		return
	}
	writeJSON(w, status)
}

func (s *Server) handleChatGPTDisconnect(w http.ResponseWriter, r *http.Request) {
	if !s.chatGPTAvailable(w) {
		return
	}
	var cleanupErr error
	if s.chatGPTBrowser != nil {
		cleanupErr = s.chatGPTBrowser.Cancel(scopedCtx(r.Context()), scopedIdentityID(r.Context()), "")
	}
	if err := s.chatGPTPlan.Disconnect(scopedCtx(r.Context()), scopedIdentityID(r.Context())); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "Unable to confirm ChatGPT disconnection; check ChatGPT connections"})
		return
	}
	if cleanupErr != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "ChatGPT disconnected; unable to confirm login browser cleanup"})
		return
	}
	writeJSON(w, map[string]bool{"disconnected": true})
}
