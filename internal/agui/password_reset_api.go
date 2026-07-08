package agui

import (
	"encoding/json"
	"errors"
	"net/http"
)

const passwordResetBodyMaxBytes = 16 << 10

// SetPasswordResetService wires the optional self-service password reset API.
func (s *Server) SetPasswordResetService(service *PasswordResetService) {
	s.passwordReset = service
}

func (s *Server) registerPasswordResetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/password-reset/start", s.handlePasswordResetStart)
	mux.HandleFunc("POST /api/auth/password-reset/question", s.handlePasswordResetQuestion)
	mux.HandleFunc("POST /api/auth/password-reset/verify", s.handlePasswordResetVerify)
	mux.HandleFunc("POST /api/auth/password-reset/complete", s.handlePasswordResetComplete)
}

func (s *Server) handlePasswordResetStart(w http.ResponseWriter, r *http.Request) {
	if s.passwordReset == nil {
		http.Error(w, "password reset service not configured", http.StatusServiceUnavailable)
		return
	}
	var req PasswordResetStartRequest
	if !decodePasswordResetRequest(w, r, &req) {
		return
	}
	stampPasswordResetRequest(r, &req.RequestIP, &req.UserAgent)
	resp, err := s.passwordReset.Start(r.Context(), req)
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, resp)
}

func (s *Server) handlePasswordResetQuestion(w http.ResponseWriter, r *http.Request) {
	if s.passwordReset == nil {
		http.Error(w, "password reset service not configured", http.StatusServiceUnavailable)
		return
	}
	var req PasswordResetQuestionRequest
	if !decodePasswordResetRequest(w, r, &req) {
		return
	}
	stampPasswordResetRequest(r, &req.RequestIP, &req.UserAgent)
	resp, err := s.passwordReset.Question(r.Context(), req)
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handlePasswordResetVerify(w http.ResponseWriter, r *http.Request) {
	if s.passwordReset == nil {
		http.Error(w, "password reset service not configured", http.StatusServiceUnavailable)
		return
	}
	var req PasswordResetVerifyRequest
	if !decodePasswordResetRequest(w, r, &req) {
		return
	}
	stampPasswordResetRequest(r, &req.RequestIP, &req.UserAgent)
	resp, err := s.passwordReset.Verify(r.Context(), req)
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handlePasswordResetComplete(w http.ResponseWriter, r *http.Request) {
	if s.passwordReset == nil {
		http.Error(w, "password reset service not configured", http.StatusServiceUnavailable)
		return
	}
	var req PasswordResetCompleteRequest
	if !decodePasswordResetRequest(w, r, &req) {
		return
	}
	stampPasswordResetRequest(r, &req.RequestIP, &req.UserAgent)
	resp, err := s.passwordReset.Complete(r.Context(), req)
	if err != nil {
		writePasswordResetError(w, err)
		return
	}
	writeJSON(w, resp)
}

func decodePasswordResetRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, passwordResetBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func stampPasswordResetRequest(r *http.Request, requestIP, userAgent *string) {
	*requestIP = r.RemoteAddr
	*userAgent = r.UserAgent()
}

func writePasswordResetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPasswordResetSamePassword):
		http.Error(w, "password reset same password", http.StatusConflict)
	case errors.Is(err, ErrPasswordResetDenied):
		http.Error(w, "password reset denied", http.StatusForbidden)
	case errors.Is(err, errPasswordResetInvalid):
		http.Error(w, "password reset invalid request", http.StatusBadRequest)
	default:
		http.Error(w, "password reset unavailable", http.StatusServiceUnavailable)
	}
}
