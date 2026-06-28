package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const bootstrapBodyMaxBytes = 16 << 10

// ErrBootstrapAlreadyConfigured means a loginable Authula operator already exists, so
// the public first-operator endpoint is closed.
var ErrBootstrapAlreadyConfigured = errors.New("bootstrap: first operator already configured")

// ErrBootstrapInvalid means the first-operator payload is missing required fields or
// exceeds the bounded field sizes.
var ErrBootstrapInvalid = errors.New("bootstrap: invalid request")

// BootstrapCreateRequest is the public first-run request used from the login screen.
type BootstrapCreateRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	SecurityQuestion string `json:"securityQuestion"`
	SecurityAnswer   string `json:"securityAnswer"`
}

// BootstrapCreateResponse is returned after the first operator has been created.
type BootstrapCreateResponse struct {
	IdentityID string `json:"identityId"`
}

// BootstrapService creates the first operator only while Authula has no enrolled users.
type BootstrapService interface {
	CreateFirstOperator(context.Context, BootstrapCreateRequest) (BootstrapCreateResponse, error)
}

// SetBootstrapService wires the optional public first-operator API.
func (s *Server) SetBootstrapService(service BootstrapService) {
	s.bootstrap = service
}

func (s *Server) registerBootstrapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/bootstrap/operator", s.handleBootstrapOperator)
}

func (s *Server) handleBootstrapOperator(w http.ResponseWriter, r *http.Request) {
	if s.bootstrap == nil {
		http.Error(w, "bootstrap service not configured", http.StatusServiceUnavailable)
		return
	}
	var req BootstrapCreateRequest
	if !decodeBootstrapRequest(w, r, &req) {
		return
	}
	if err := normalizeAndValidateBootstrapRequest(&req); err != nil {
		http.Error(w, "bootstrap invalid request", http.StatusBadRequest)
		return
	}
	resp, err := s.bootstrap.CreateFirstOperator(r.Context(), req)
	if err != nil {
		writeBootstrapError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, resp)
}

func decodeBootstrapRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, bootstrapBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func normalizeAndValidateBootstrapRequest(req *BootstrapCreateRequest) error {
	req.Email = strings.TrimSpace(req.Email)
	req.SecurityQuestion = strings.TrimSpace(req.SecurityQuestion)
	req.SecurityAnswer = strings.TrimSpace(req.SecurityAnswer)
	if req.Email == "" || len(req.Email) > onboardingEmailMaxLen {
		return ErrBootstrapInvalid
	}
	if req.Password == "" || len(req.Password) > onboardingPasswordMaxLen {
		return ErrBootstrapInvalid
	}
	if req.SecurityQuestion == "" || len(req.SecurityQuestion) > onboardingSecurityQuestionMaxLen {
		return ErrBootstrapInvalid
	}
	if req.SecurityAnswer == "" || len(req.SecurityAnswer) > onboardingSecurityAnswerMaxLen {
		return ErrBootstrapInvalid
	}
	return nil
}

func writeBootstrapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBootstrapAlreadyConfigured):
		http.Error(w, "bootstrap already configured", http.StatusConflict)
	case errors.Is(err, ErrBootstrapInvalid):
		http.Error(w, "bootstrap invalid request", http.StatusBadRequest)
	default:
		http.Error(w, "bootstrap unavailable", http.StatusServiceUnavailable)
	}
}
