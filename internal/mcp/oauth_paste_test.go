package mcp

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func pasteFlow(t *testing.T, state string) (*Flows, Flow) {
	t.Helper()
	f := newTestFlows(t, openerRunningTheFlow("https://provider.example/authorize?state="+url.QueryEscape(state), nil))
	flow, err := f.Start(t.Context(), "owner", "canva", remoteServer(), LoopbackCallback)
	if err != nil {
		t.Fatal(err)
	}
	return f, flow
}

func TestPasteAddress(t *testing.T) {
	for _, tc := range []struct {
		name, address, owner string
		expired, replay      bool
		want                 error
	}{
		{name: "code", address: LoopbackCallback + "?state=s1&code=c"},
		{name: "wrong state", address: LoopbackCallback + "?state=s2&code=c", want: ErrPasteMismatch},
		{name: "refused", address: LoopbackCallback + "?state=s1&error=access_denied&error_description=no"},
		{name: "invalid URL", address: "not a url", want: ErrPasteMismatch},
		{name: "empty", want: ErrPasteMismatch},
		{name: "oversized", address: strings.Repeat("x", 16*1024+1), want: ErrPasteMismatch},
		{name: "no code", address: LoopbackCallback + "?state=s1", want: ErrPasteMismatch},
		{name: "replayed", address: LoopbackCallback + "?state=s1&code=c", replay: true, want: ErrFlowNotWaiting},
		{name: "other owner", address: LoopbackCallback + "?state=s1&code=c", owner: "other", want: ErrNoSuchFlow},
		{name: "expired", address: LoopbackCallback + "?state=s1&code=c", expired: true, want: ErrNoSuchFlow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, flow := pasteFlow(t, "s1")
			if tc.expired {
				f.mu.Lock()
				f.byID[flow.ID].ExpiresAt = time.Now().Add(-time.Minute)
				f.mu.Unlock()
			}
			if tc.replay {
				if _, err := f.Paste("owner", flow.ID, tc.address); err != nil {
					t.Fatal(err)
				}
			}
			owner := tc.owner
			if owner == "" {
				owner = "owner"
			}
			got, err := f.Paste(owner, flow.ID, tc.address)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Paste error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if !tc.expired && !tc.replay {
					pending, statusErr := f.Status("owner", flow.ID)
					if statusErr != nil || pending.Status != FlowAuthorizationRequired {
						t.Fatalf("rejected paste changed flow: %+v, %v", pending, statusErr)
					}
				}
				return
			}
			if tc.name == "refused" {
				if got.Status != FlowError || !strings.Contains(got.Error, "access_denied") {
					t.Fatalf("refusal = %+v", got)
				}
			} else {
				waitForStatus(t, f, "owner", flow.ID, FlowApproved)
			}
		})
	}
}

func TestPasteTakesAnyHostBecauseOnlyTheStateProvesIt(t *testing.T) {
	f, flow := pasteFlow(t, "s1")
	if _, err := f.Paste("owner", flow.ID, "https://example.com/cb?state=s1&code=c"); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, f, "owner", flow.ID, FlowApproved)
}

func TestPasteCarriesARelayedState(t *testing.T) {
	state := "s1.aHR0cDovLzEyNy4wLjAuMTo0MzEyMS9hdXJhL21jcC9vYXV0aC9jYWxsYmFjaw"
	f, flow := pasteFlow(t, state)
	if _, err := f.Paste("owner", flow.ID, LoopbackCallback+"?state="+url.QueryEscape(state)+"&code=c"); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, f, "owner", flow.ID, FlowApproved)
}

func TestCancelOnlyEndsTheOwnersFlow(t *testing.T) {
	f, flow := pasteFlow(t, "s1")
	if err := f.Cancel("other", flow.ID); !errors.Is(err, ErrNoSuchFlow) {
		t.Fatalf("other Cancel = %v", err)
	}
	if err := f.Cancel("owner", flow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Paste("owner", flow.ID, LoopbackCallback+"?state=s1&code=c"); !errors.Is(err, ErrNoSuchFlow) {
		t.Fatalf("cancelled Paste = %v", err)
	}
}

func TestSignInRedirect(t *testing.T) {
	for _, tc := range []struct {
		configured, want string
		loopback         bool
	}{
		{"", LoopbackCallback, true},
		{"http://localhost:9000/cb", "http://localhost:9000/cb", true},
		{"http://[::1]:9100/cb", "http://[::1]:9100/cb", true},
		{"https://a.example/cb", "https://a.example/cb", false},
		{"http://localhost.evil.example/cb", "http://localhost.evil.example/cb", false},
	} {
		s := OAuthSettings{RedirectURL: tc.configured}
		if s.SignInRedirect() != tc.want || s.RedirectIsLoopback() != tc.loopback {
			t.Fatalf("redirect %q = %q, loopback %v", tc.configured, s.SignInRedirect(), s.RedirectIsLoopback())
		}
	}
}
