package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestFreeBytesReadsDfOutput(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   int64
		valid  bool
	}{
		{"  Avail\n9876543210\n", 9876543210, true},
		{"Avail\n0\n", 0, true},
		{"", 0, false}, {"Avail\njunk\n", 0, false}, {"Avail\n-1\n", 0, false},
	} {
		got, err := freeBytesFromDf([]byte(tc.output))
		if (err == nil) != tc.valid || got != tc.want {
			t.Errorf("df %q = %d, %v", tc.output, got, err)
		}
	}
}

type signInPreflightBackend struct {
	fakePreflightBackend
	present           bool
	swept             int
	admitted, stopped []string
	df                string
}

func (b *signInPreflightBackend) AdmitBrowser(_ context.Context, key string) error {
	b.admitted = append(b.admitted, key)
	return nil
}
func (b *signInPreflightBackend) StartSignIn(context.Context, usersandbox.SignInSpec, usersandbox.SignInPrepare) (usersandbox.SignInBox, error) {
	return usersandbox.SignInBox{}, nil
}
func (b *signInPreflightBackend) StopSignIn(_ context.Context, session string) error {
	b.stopped = append(b.stopped, session)
	return nil
}
func (b *signInPreflightBackend) SweepSignIns(context.Context) error { b.swept++; return nil }
func (b *signInPreflightBackend) SignInImageState(context.Context, string) (bool, error) {
	return b.present, nil
}
func (*signInPreflightBackend) PullSignInImage(context.Context, string, func(int64, int64)) error {
	return nil
}
func (b *signInPreflightBackend) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{Stdout: []byte(b.df)}, nil
}

func TestSignInBrowserRequiresAnIdentityBeforeAdmission(t *testing.T) {
	b := &signInPreflightBackend{present: true}
	launcher := &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:local", sidecarImage: "sidecar:local"}
	if _, err := launcher.Open(t.Context(), "signin-0123456789abcdef01234567", browserlogin.LaunchOptions{Redirect: mcp.LoopbackCallback}); err == nil {
		t.Fatal("anonymous sign-in admitted")
	}
	if len(b.admitted) != 0 {
		t.Fatal("anonymous sign-in reserved host capacity")
	}
}

func TestSignInBrowserReservesAndReleasesItsOwnSlot(t *testing.T) {
	b := &signInPreflightBackend{present: true}
	launcher := &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:local", sidecarImage: "sidecar:local"}
	session := "signin-0123456789abcdef01234567"
	ctx := identityctx.WithIdentityID(t.Context(), "owner")
	opened, err := launcher.Open(ctx, session, browserlogin.LaunchOptions{Redirect: mcp.LoopbackCallback})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.admitted) != 1 || b.admitted[0] != session || opened.RedirectURI() != mcp.LoopbackCallback {
		t.Fatal("sign-in borrowed the owner's other browser slot or lost its redirect")
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if len(b.stopped) != 1 || b.stopped[0] != session {
		t.Fatal("sign-in close retained its reservation")
	}
}

func TestMissingSignInImageReportsTheDiskOrTheDownload(t *testing.T) {
	for _, tc := range []struct {
		df   string
		want error
	}{
		{"Avail\n1\n", errSignInDiskLow}, {"Avail\n9000000000\n", errSignInImageMissing},
	} {
		b := &signInPreflightBackend{df: tc.df}
		launcher := &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:local", sidecarImage: "sidecar:local"}
		opened, err := launcher.Open(identityctx.WithIdentityID(t.Context(), "owner"), "signin-0123456789abcdef01234567", browserlogin.LaunchOptions{Redirect: mcp.LoopbackCallback})
		if !errors.Is(err, tc.want) {
			t.Fatalf("preflight = %v, want %v", err, tc.want)
		}
		if opened == nil {
			t.Fatal("failed preflight lost its cleanup handle")
		}
		if err := opened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBootSweepsSignInDesktopsWithoutPullingTheImage(t *testing.T) {
	b := &signInPreflightBackend{}
	router := usersandbox.NewSandboxRouter(b, config.SandboxConfig{})
	sweepSignInBrowsers(t.Context(), &chatEnv{sandboxRouter: router})
	if b.swept != 1 || b.ensureImageCalls != 0 || len(b.resolved) != 0 {
		t.Fatal("boot omitted cleanup or pulled/started a desktop")
	}
}

func TestListenerAddressComesFromTheRedirect(t *testing.T) {
	for _, tc := range []struct {
		redirect string
		port     int
		path     string
	}{
		{mcp.LoopbackCallback, 43121, mcp.LoopbackCallbackPath},
		{"http://127.0.0.1:9100/cb", 9100, "/cb"},
		{"http://localhost:9200/cb", 9200, "/cb"},
		{"http://[::1]:9300/cb", 9300, "/cb"},
	} {
		got, err := listenerAddressFromRedirect(tc.redirect)
		if err != nil || got.Port != tc.port || got.Path != tc.path {
			t.Errorf("listener %q = %+v, %v", tc.redirect, got, err)
		}
	}
	for _, redirect := range []string{"https://a.example/cb", "http://192.168.1.2:9100/cb", "http://127.0.0.1:0/cb", "http://127.0.0.1:9100/cb?state=s"} {
		if _, err := listenerAddressFromRedirect(redirect); err == nil {
			t.Errorf("accepted non-loopback/invalid listener %q", redirect)
		}
	}
}
