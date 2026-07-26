package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// onboarding_api_handlers_test.go covers the thin REST adapters onboarding_api.go exposes
// beyond start (which onboarding_api_test.go already covers): the seed-submit / provision /
// telegram-status handlers, the persisted onboarding-status read model, and the
// writeOnboardingError sentinel→HTTP mapping. Every case drives the real handler through a
// fake OnboardingService so the DI seams, the 503 unwired paths, the 401 no-principal
// paths, and the error projection are all exercised without a live saga.

// fakeOnboardingStatusSource is a scriptable OnboardingStatusSource: it returns a fixed
// status (or error) and records the identity it was asked about so the self-scoped read is
// provable.
type fakeOnboardingStatusSource struct {
	status OnboardingStatus
	err    error
	gotID  string
}

func (f *fakeOnboardingStatusSource) OnboardingStatus(_ context.Context, identityID string) (OnboardingStatus, error) {
	f.gotID = identityID
	return f.status, f.err
}

// validProvisionBody is a provision request that passes validateOnboardingProvision so the
// handler reaches the service call (where the fake injects the response/error under test).
const validProvisionBody = `{"email":"new@aura.local","password":"pw-1234567890","securityQuestion":"first pet?","securityAnswer":"rex","linkTelegram":true,"capabilities":["agent.run"]}`

func TestSetOnboardingStatusSourceOffConstructor(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if s.onboardingStatus != nil {
		t.Fatalf("NewServer must leave onboarding status source nil, got %v", s.onboardingStatus)
	}
	s.SetOnboardingStatusSource(&fakeOnboardingStatusSource{})
	if s.onboardingStatus == nil {
		t.Fatal("SetOnboardingStatusSource did not wire the source")
	}
}

func TestHandleOnboardingStatus(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		rec := httptest.NewRecorder()
		(&Server{}).handleOnboardingStatus(rec, httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("401 without principal", func(t *testing.T) {
		s := &Server{onboardingStatus: &fakeOnboardingStatusSource{}}
		rec := httptest.NewRecorder()
		s.handleOnboardingStatus(rec, httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("502 on store error", func(t *testing.T) {
		src := &fakeOnboardingStatusSource{err: errors.New("read model down")}
		s := &Server{onboardingStatus: src}
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil), testLocalID)
		s.handleOnboardingStatus(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
	})

	t.Run("200 self-scoped", func(t *testing.T) {
		src := &fakeOnboardingStatusSource{status: OnboardingStatus{Required: true, Completed: false, Skipped: false}}
		s := &Server{onboardingStatus: src}
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil), testLocalID)
		s.handleOnboardingStatus(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if src.gotID != testLocalID {
			t.Fatalf("status read identity = %q, want the bound principal", src.gotID)
		}
		var got OnboardingStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !got.Required {
			t.Fatalf("status = %+v, want required", got)
		}
	})
}

// TestHandleProfileSubmit covers the ONE stateless seed route that replaced the interview's
// profile-start + {token}/profile pair (Amendment #95): unwired 503, malformed body 400,
// over-cap field 400 (both before the service is reached), the sentinel→HTTP projection,
// and a happy path proving the decoded seed and the authenticated principal both reach the
// service.
func TestHandleProfileSubmit(t *testing.T) {
	post := func(t *testing.T, h http.Handler, body string) *http.Response {
		t.Helper()
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		resp, err := http.Post(srv.URL+"/api/onboarding/profile", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST profile: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	t.Run("503 unwired", func(t *testing.T) {
		s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
		if got := post(t, withTestPrincipal(s.Mux(), testLocalID), `{}`).StatusCode; got != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", got)
		}
	})

	t.Run("401 without principal", func(t *testing.T) {
		s := &Server{onboarding: &fakeOnboarding{}}
		rec := httptest.NewRecorder()
		s.handleProfileSubmit(rec, httptest.NewRequest(http.MethodPost, "/api/onboarding/profile", strings.NewReader(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("400 before the service", func(t *testing.T) {
		for name, body := range map[string]string{
			"malformed body": `{"name":`,
			"overlong name":  `{"name":"` + strings.Repeat("a", onboardingSeedFieldMaxLen+1) + `"}`,
		} {
			t.Run(name, func(t *testing.T) {
				fake := &fakeOnboarding{}
				if got := post(t, onboardingTestServer(t, fake), body).StatusCode; got != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", got)
				}
				if fake.gotRequester != "" {
					t.Fatalf("service was called on an invalid body (requester=%q)", fake.gotRequester)
				}
			})
		}
	})

	t.Run("error maps through writeOnboardingError", func(t *testing.T) {
		fake := &fakeOnboarding{profileErr: errOnboardingForbidden}
		if got := post(t, onboardingTestServer(t, fake), `{}`).StatusCode; got != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", got)
		}
	})

	t.Run("200 routes the seed + principal", func(t *testing.T) {
		fake := &fakeOnboarding{profileDone: OnboardingProfileComplete{
			Completed: true, SessionToken: "poll-1", DeepLink: "https://t.me/AuraBot?start=x", QRSVG: "<svg/>",
		}}
		resp := post(t, onboardingTestServer(t, fake),
			`{"name":"Davide","lang":"it","location":"Caraglio","timezone":"Europe/Rome","role":"founder","company":"PmSync"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if fake.gotRequester != testLocalID {
			t.Fatalf("service got requester=%q, want the bound principal", fake.gotRequester)
		}
		if fake.gotSeed != fullSeed {
			t.Fatalf("service got seed = %+v, want the submitted form", fake.gotSeed)
		}
		var got OnboardingProfileComplete
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !got.Completed || got.SessionToken != "poll-1" || got.DeepLink == "" {
			t.Fatalf("completion = %+v, want completed with a pollable token and deep-link", got)
		}
	})
}

func TestHandleTelegramStatusHandler(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
		srv := httptest.NewServer(withTestPrincipal(s.Mux(), testLocalID))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL + "/api/onboarding/tok/telegram-status")
		if err != nil {
			t.Fatalf("GET telegram-status: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
	})

	t.Run("error maps to 404 on unknown session", func(t *testing.T) {
		fake := &fakeOnboarding{statusErr: errOnboardingSessionNotFound}
		srv := httptest.NewServer(onboardingTestServer(t, fake))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL + "/api/onboarding/gone/telegram-status")
		if err != nil {
			t.Fatalf("GET telegram-status: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("200 reports linked", func(t *testing.T) {
		fake := &fakeOnboarding{statusResp: OnboardingTelegramStatus{Linked: true}}
		srv := httptest.NewServer(onboardingTestServer(t, fake))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL + "/api/onboarding/tok-7/telegram-status")
		if err != nil {
			t.Fatalf("GET telegram-status: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if fake.gotToken != "tok-7" {
			t.Fatalf("routed token = %q, want tok-7", fake.gotToken)
		}
		var got OnboardingTelegramStatus
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !got.Linked {
			t.Fatal("linked = false, want true")
		}
	})
}

// TestHandleOnboardingProvisionValidation proves the provision handler front door: a body
// that fails validateOnboardingProvision is a 400 with the service never called, and a
// valid body reaches the service and routes the token.
func TestHandleOnboardingProvisionValidation(t *testing.T) {
	t.Run("invalid body is 400 before the service", func(t *testing.T) {
		fake := &fakeOnboarding{}
		srv := httptest.NewServer(onboardingTestServer(t, fake))
		t.Cleanup(srv.Close)
		// Missing password + no telegram link → validation fails.
		resp, err := http.Post(srv.URL+"/api/onboarding/tok-1/provision", "application/json",
			strings.NewReader(`{"email":"a@b.c"}`))
		if err != nil {
			t.Fatalf("POST provision: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		if fake.gotToken != "" {
			t.Fatalf("service was called on an invalid body (token=%q)", fake.gotToken)
		}
	})

	t.Run("valid body reaches the service", func(t *testing.T) {
		fake := &fakeOnboarding{provResp: OnboardingProvisionResponse{IdentityID: "id-1", DeepLink: "https://t.me/AuraBot?start=x", QRSVG: "<svg/>"}}
		srv := httptest.NewServer(onboardingTestServer(t, fake))
		t.Cleanup(srv.Close)
		resp, err := http.Post(srv.URL+"/api/onboarding/tok-9/provision", "application/json",
			strings.NewReader(validProvisionBody))
		if err != nil {
			t.Fatalf("POST provision: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: reached service?", resp.StatusCode)
		}
		if fake.gotToken != "tok-9" || fake.gotRequester != testLocalID {
			t.Fatalf("service got requester=%q token=%q, want principal/tok-9", fake.gotRequester, fake.gotToken)
		}
		var got OnboardingProvisionResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.IdentityID != "id-1" {
			t.Fatalf("provision = %+v, want identity id-1", got)
		}
	})
}

// TestWriteOnboardingErrorMapping pins every service-error sentinel onto its HTTP status
// through the provision handler (the one that funnels a service error into
// writeOnboardingError). An unmapped error is a sanitized 502.
func TestWriteOnboardingErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"session not found", errOnboardingSessionNotFound, http.StatusNotFound},
		{"escalation", ErrOnboardingEscalation, http.StatusBadRequest},
		{"duplicate", ErrOnboardingDuplicate, http.StatusConflict},
		{"forbidden", errOnboardingForbidden, http.StatusForbidden},
		{"isolation disabled", errIsolationDisabled, http.StatusConflict},
		{"unmapped backend error", errors.New("saga blew up"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOnboarding{provErr: tc.err}
			srv := httptest.NewServer(onboardingTestServer(t, fake))
			t.Cleanup(srv.Close)
			resp, err := http.Post(srv.URL+"/api/onboarding/tok/provision", "application/json",
				strings.NewReader(validProvisionBody))
			if err != nil {
				t.Fatalf("POST provision: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s: status = %d, want %d", tc.name, resp.StatusCode, tc.want)
			}
		})
	}
}
