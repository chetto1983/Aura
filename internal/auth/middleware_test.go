package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"user":"` + UserIDFromContext(r.Context()) + `"}`))
	})
}

func TestRequireBearer_HappyPath(t *testing.T) {
	s := newTestStore(t)
	tok, err := s.Issue(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	allow := func(uid string) bool { return uid == "u1" }
	h := RequireBearer(s, allow, nil, okHandler())

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body=%s", rr.Code, rr.Body)
	}
	if got := rr.Body.String(); got == "" || got == `{"user":""}` {
		t.Errorf("user id missing from context; body=%s", got)
	}
}

func TestRequireBearer_MissingHeader(t *testing.T) {
	s := newTestStore(t)
	h := RequireBearer(s, nil, nil, okHandler())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestRequireBearer_BadHeaderShape(t *testing.T) {
	s := newTestStore(t)
	h := RequireBearer(s, nil, nil, okHandler())
	cases := []string{
		"",                  // missing
		"Token abc",         // wrong scheme
		"Bearer",            // no value
		"Bearer  ",          // whitespace only
		"basic abc:def",     // wrong scheme
		"abc",               // no scheme
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/health", nil)
			if c != "" {
				req.Header.Set("Authorization", c)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("status %d, want 401", rr.Code)
			}
		})
	}
}

func TestRequireBearer_BearerCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	tok, _ := s.Issue(context.Background(), "u1")
	h := RequireBearer(s, nil, nil, okHandler())
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status %d, want 200", rr.Code)
	}
}

func TestRequireBearer_WrongToken(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Issue(context.Background(), "u1")
	h := RequireBearer(s, nil, nil, okHandler())
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestRequireBearer_RevokedToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, _ := s.Issue(ctx, "u1")
	_ = s.Revoke(ctx, tok)
	h := RequireBearer(s, nil, nil, okHandler())
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestRequireBearer_UserDeAllowlisted(t *testing.T) {
	s := newTestStore(t)
	tok, _ := s.Issue(context.Background(), "u1")
	allow := func(string) bool { return false } // user is no longer allowlisted
	h := RequireBearer(s, allow, nil, okHandler())
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rr.Code)
	}
}

func TestUserIDFromContext_Empty(t *testing.T) {
	if got := UserIDFromContext(context.Background()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
