package browserlogin

import (
	"strings"
	"testing"
)

func loginForOptions(t *testing.T, launcher Launcher, opts Options) *Login {
	t.Helper()
	l := New(t.Context(), launcher, opts)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func startWithOptions(t *testing.T, l *Login, attempt string, opts LaunchOptions, a Authorizer) Flow {
	t.Helper()
	flow, err := l.Start(ownerContext("alice"), "alice", attempt, opts, a)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func TestTwoLoginsKeepTheirOwnSessions(t *testing.T) {
	launcher := newBrowserLauncher()
	chat := loginForOptions(t, launcher, Options{Prefix: "chatgpt-", Route: "/browser/"})
	signin := loginForOptions(t, launcher, Options{Prefix: "signin-", Route: "/signing-in/"})
	chatFlow := startWithOptions(t, chat, "chat", LaunchOptions{}, newAuthorizerFixture(t))
	chatBrowser := receiveBrowser(t, launcher.opened)
	startWithOptions(t, signin, "signin", LaunchOptions{}, newAuthorizerFixture(t))
	signinBrowser := receiveBrowser(t, launcher.opened)
	if !strings.HasPrefix(chatBrowser.session, "chatgpt-") || !strings.HasPrefix(signinBrowser.session, "signin-") {
		t.Fatal("login prefixes collided")
	}
	if err := chat.Cancel(ownerContext("alice"), "alice", chatFlow.Route); err != nil {
		t.Fatal(err)
	}
	if !signin.Owns(ownerContext("alice"), "alice", signinBrowser.session) || chat.Owns(ownerContext("alice"), "alice", chatBrowser.session) {
		t.Fatal("one manager cancelled the other's session")
	}
}

func TestAnOwnerHasOneSignInAtATime(t *testing.T) {
	launcher := newBrowserLauncher()
	l := loginForOptions(t, launcher, Options{})
	a := newAuthorizerFixture(t)
	old := startWithOptions(t, l, "old", LaunchOptions{}, a)
	oldBrowser := receiveBrowser(t, launcher.opened)
	fresh := startWithOptions(t, l, "fresh", LaunchOptions{}, a)
	freshBrowser := receiveBrowser(t, launcher.opened)
	receiveBrowser(t, oldBrowser.browser.closed)
	if old.Route == fresh.Route || l.Owns(ownerContext("alice"), "alice", oldBrowser.session) || !l.Owns(ownerContext("alice"), "alice", freshBrowser.session) {
		t.Fatal("replacement retained the old live session")
	}
}

func TestStartHandsTheRedirectToTheLauncher(t *testing.T) {
	launcher := newBrowserLauncher()
	l := loginForOptions(t, launcher, Options{})
	opts := LaunchOptions{Redirect: "http://127.0.0.1:9100/auth/callback"}
	startWithOptions(t, l, "redirect", opts, newAuthorizerFixture(t))
	if opening := receiveBrowser(t, launcher.opened); opening.opts.Redirect != opts.Redirect {
		t.Fatalf("launcher redirect = %q", opening.opts.Redirect)
	}
}

func TestStartKeepsEachAttemptsOrigins(t *testing.T) {
	launcher := newBrowserLauncher()
	l := loginForOptions(t, launcher, Options{})
	a := newAuthorizerFixture(t)
	first := LaunchOptions{CockpitOrigin: "https://a.example:8443", StreamOrigin: "https://a.example:8444"}
	startWithOptions(t, l, "first", first, a)
	old := receiveBrowser(t, launcher.opened)
	second := LaunchOptions{CockpitOrigin: "https://b.example:9443", StreamOrigin: "https://b.example:9444"}
	startWithOptions(t, l, "second", second, a)
	fresh := receiveBrowser(t, launcher.opened)
	if old.opts != first || fresh.opts != second {
		t.Fatalf("origins leaked across attempts: %+v, %+v", old.opts, fresh.opts)
	}
}
