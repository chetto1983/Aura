package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapOperatorCreatesFirstOperator(t *testing.T) {
	service := &fakeBootstrapService{
		resp: BootstrapCreateResponse{IdentityID: "11111111-1111-1111-1111-111111111111"},
	}
	server := NewServer(nil, nil, ServerConfig{})
	server.SetBootstrapService(service)

	rec := httptest.NewRecorder()
	body := `{"email":" first@example.com ","password":"correct-horse","securityQuestion":"First school?","securityAnswer":"Ada"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", strings.NewReader(body))
	server.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var got BootstrapCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.IdentityID != service.resp.IdentityID {
		t.Fatalf("identityId = %q, want %q", got.IdentityID, service.resp.IdentityID)
	}
	if service.req.Email != "first@example.com" {
		t.Fatalf("email = %q, want trimmed first@example.com", service.req.Email)
	}
	if service.req.Password != "correct-horse" || service.req.SecurityQuestion != "First school?" || service.req.SecurityAnswer != "Ada" {
		t.Fatalf("request not forwarded safely: %#v", service.req)
	}
}

func TestBootstrapOperatorRejectsInvalidRequest(t *testing.T) {
	server := NewServer(nil, nil, ServerConfig{})
	server.SetBootstrapService(&fakeBootstrapService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", strings.NewReader(`{"email":"","password":""}`))
	server.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBootstrapOperatorRejectsAfterFirstUserExists(t *testing.T) {
	server := NewServer(nil, nil, ServerConfig{})
	server.SetBootstrapService(&fakeBootstrapService{err: ErrBootstrapAlreadyConfigured})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", bytes.NewBufferString(
		`{"email":"first@example.com","password":"correct-horse","securityQuestion":"First school?","securityAnswer":"Ada"}`,
	))
	server.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestBootstrapOperatorRequiresConfiguredService(t *testing.T) {
	server := NewServer(nil, nil, ServerConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", strings.NewReader(
		`{"email":"first@example.com","password":"correct-horse","securityQuestion":"First school?","securityAnswer":"Ada"}`,
	))
	server.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

type fakeBootstrapService struct {
	req  BootstrapCreateRequest
	resp BootstrapCreateResponse
	err  error
}

func (f *fakeBootstrapService) CreateFirstOperator(_ context.Context, req BootstrapCreateRequest) (BootstrapCreateResponse, error) {
	f.req = req
	if f.err != nil {
		return BootstrapCreateResponse{}, f.err
	}
	if f.resp.IdentityID == "" {
		f.resp.IdentityID = "22222222-2222-2222-2222-222222222222"
	}
	return f.resp, nil
}

var _ BootstrapService = (*fakeBootstrapService)(nil)

func TestBootstrapOperatorMapsUnexpectedServiceError(t *testing.T) {
	server := NewServer(nil, nil, ServerConfig{})
	server.SetBootstrapService(&fakeBootstrapService{err: errors.New("authula refused password secret")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", strings.NewReader(
		`{"email":"first@example.com","password":"correct-horse","securityQuestion":"First school?","securityAnswer":"Ada"}`,
	))
	server.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("service error leaked into response: %s", rec.Body.String())
	}
}

// TestBootstrapOperatorNamesTheOffendingField: this endpoint is the very first request a
// new deployment ever receives, and it used to answer "bootstrap invalid request" for any
// of four fields. On a live stack that cost two blind retries before securityQuestion and
// securityAnswer turned out to be the missing pair. Naming the field enumerates nothing —
// the route only answers while Authula has no enrolled user.
func TestBootstrapOperatorNamesTheOffendingField(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no email", `{"email":"","password":"p","securityQuestion":"q","securityAnswer":"a"}`, "email is required"},
		{"no password", `{"email":"e@x.test","password":"","securityQuestion":"q","securityAnswer":"a"}`, "password is required"},
		{"no question", `{"email":"e@x.test","password":"p","securityQuestion":"","securityAnswer":"a"}`, "securityQuestion is required"},
		{"no answer", `{"email":"e@x.test","password":"p","securityQuestion":"q","securityAnswer":""}`, "securityAnswer is required"},
		{"email too long", `{"email":"` + strings.Repeat("a", onboardingEmailMaxLen+1) + `","password":"p","securityQuestion":"q","securityAnswer":"a"}`, "email is longer than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := NewServer(nil, nil, ServerConfig{})
			server.SetBootstrapService(&fakeBootstrapService{})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap/operator", strings.NewReader(tc.body))
			server.Mux().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body %q does not name the field (%q)", rec.Body.String(), tc.want)
			}
		})
	}
}
