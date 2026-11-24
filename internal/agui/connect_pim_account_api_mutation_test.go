package agui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
)

// connect_pim_account_api_mutation_test.go pins what the relay refuses and where it stops: every
// error answer is contract for the cockpit, and a refusal must end the request before anything
// reaches the sidecar or the mail watch.

const pimPatchPath = "/api/connect/pim/accounts/work/provider-config"

func TestTheRelayClientsBoundTheSidecarCalls(t *testing.T) {
	if pimDiscoverClient.Timeout != 15*time.Second {
		t.Fatalf("discovery timeout = %v, want 15s: above the sidecar's 12s cap", pimDiscoverClient.Timeout)
	}
	if pimVerifyClient.Timeout != 25*time.Second {
		t.Fatalf("verify timeout = %v, want 25s: an IMAP step of 12s and an SMTP step of 8s", pimVerifyClient.Timeout)
	}
}

func TestAnOversizedBodyIsRefusedBeforeTheSidecar(t *testing.T) {
	huge := `{"email":"` + strings.Repeat("a", maxRunBodyBytes) + `@example.com"}`
	for _, route := range []struct{ method, target string }{
		{http.MethodPost, "/api/connect/pim/discover"},
		{http.MethodPatch, pimPatchPath},
	} {
		p := &recordingPIM{status: http.StatusOK, answer: `{}`}
		s := pimAccountServer(t, p)

		rec := pimAccountCall(s, route.method, route.target, huge)

		const want = `{"error":"calendar request body too large"}`
		if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != want {
			t.Fatalf("%s %s = %d %q, want 400 %s and nothing more", route.method, route.target, rec.Code, rec.Body.String(), want)
		}
		if calls := p.recorded(); len(calls) != 0 {
			t.Fatalf("%s %s reached the sidecar after the refusal: %+v", route.method, route.target, calls)
		}
	}
}

func TestPatchRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"not JSON", `{"providerConfig":`, `{"error":"invalid JSON body"}`},
		{"providerConfig is a string", `{"providerConfig":"imap.example.com"}`, `{"error":"invalid providerConfig"}`},
		{"providerConfig holds a number", `{"providerConfig":{"imapPort":993}}`, `{"error":"invalid providerConfig"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &recordingPIM{status: http.StatusOK, answer: `{}`}
			s := pimAccountServer(t, p)
			watch := &fakeWatchService{}
			s.SetMailWatch(watch)

			rec := pimAccountCall(s, http.MethodPatch, pimPatchPath, tc.body)

			if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != tc.want {
				t.Fatalf("patch = %d %q, want 400 %s and nothing more", rec.Code, rec.Body.String(), tc.want)
			}
			if calls := p.recorded(); len(calls) != 0 {
				t.Fatalf("a refused patch reached the sidecar: %+v", calls)
			}
			if watch.repaired != "" {
				t.Fatalf("a refused patch woke the watch of %q", watch.repaired)
			}
		})
	}
}

func TestPatchWithoutTheSidecarIs503BeforeTheBodyIsJudged(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetIdentityAdmin(&fakeIdentityAdmin{caps: map[string][]string{fakePIMIdentity: {identity.CapIdentityCreate}}})

	rec := pimAccountCall(s, http.MethodPatch, pimPatchPath, `{"providerConfig":`)

	const want = `{"error":"calendar connect not configured"}`
	if rec.Code != http.StatusServiceUnavailable || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("patch without a sidecar = %d %q, want 503 %s and nothing more", rec.Code, rec.Body.String(), want)
	}
}

func TestTheLimiterForgetsAnAttemptExactlyAMinuteOld(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	limiter := newPIMAttemptLimiter(func() time.Time { return now })
	for i := range pimAttemptsPerMinute {
		if !limiter.allow("identity-1") {
			t.Fatalf("attempt %d inside the limit was refused", i+1)
		}
	}
	now = now.Add(time.Minute - time.Nanosecond)
	if limiter.allow("identity-1") {
		t.Fatal("an attempt a nanosecond inside the minute was allowed")
	}
	now = now.Add(time.Nanosecond)
	if !limiter.allow("identity-1") {
		t.Fatal("attempts exactly a minute old still counted")
	}
}

func TestOnlyA2xxRepairWakesTheWatch(t *testing.T) {
	for _, tc := range []struct {
		status int
		wakes  bool
	}{
		{http.StatusContinue, false},
		{199, false},
		{http.StatusOK, true},
		{http.StatusNoContent, true},
		{299, true},
		{http.StatusMultipleChoices, false},
		{http.StatusUnprocessableEntity, false},
		{http.StatusTooManyRequests, false},
	} {
		s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
		watch := &fakeWatchService{}
		s.SetMailWatch(watch)
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/", nil), fakePIMIdentity)

		s.wakeRepairedWatch(req, "work", tc.status)

		if woken := watch.repaired == "work"; woken != tc.wakes {
			t.Fatalf("status %d: watch woken = %v, want %v", tc.status, woken, tc.wakes)
		}
	}
}

func TestARepairWithoutAWatchIsStillAnswered(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(context.Background())

	s.wakeRepairedWatch(req, "work", http.StatusOK)
}

func TestAWakeThatFailsIsLoggedAsAWarning(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	p := &recordingPIM{status: http.StatusOK, answer: `{"ok":true}`}
	s := pimAccountServer(t, p)
	s.SetMailWatch(&fakeWatchService{err: errors.New("scheduler down")})

	pimAccountCall(s, http.MethodPost, "/api/connect/pim/accounts/work/verify", "")

	if got := buf.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "waking the repaired account's watch did not finish") {
		t.Fatalf("log = %q, want a warning that the wake did not finish", got)
	}
}
