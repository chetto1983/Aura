package usersandbox

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// TestResolveBrowserNeverTouchesTheShellBox: starting the browser box reads and writes only
// browser objects. Resolve once copied a legacy profile out of the shell box first, and on the
// lab VM (2026-10-12) a copy that failed its verification kept the browser box from starting.
func TestResolveBrowserNeverTouchesTheShellBox(t *testing.T) {
	const id = "448ddbe1"
	var touched []string
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		if target := req.URL.Path + " " + req.URL.Query().Get("filters"); strings.Contains(target, boxName(id)) {
			touched = append(touched, req.Method+" "+target)
		}
		switch {
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/volumes/create"):
			return dockerJSONResponse(http.StatusCreated, `{"Name":"volume"}`), nil
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/containers/json"):
			return dockerJSONResponse(http.StatusOK, `[]`), nil
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/info"):
			return dockerJSONResponse(http.StatusOK, `{"MemTotal":17179869184}`), nil
		case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/"):
			return dockerJSONResponse(http.StatusOK, `{}`), nil
		}
		return nil, fmt.Errorf("stop here: %s %s", req.Method, req.URL.Path)
	})
	backend := NewDockerBackend(cli, "aura-sandbox:test", Resources{})
	_, err := backend.Resolve(context.Background(), SandboxSpec{IdentityID: id, Kind: Browser})
	if !strings.Contains(fmt.Sprint(err), "create box") {
		t.Fatalf("Resolve stopped before creating the browser box: %v", err)
	}
	if len(touched) > 0 {
		t.Fatalf("browser resolve touched the shell box: %v", touched)
	}
}

func TestBrowserMountsCannotReachShellVolumes(t *testing.T) {
	for _, kind := range []BoxKind{Shell, Browser} {
		s := SandboxSpec{IdentityID: "a", Kind: kind}
		hc := toHostConfig(s)
		assertPinsSafe(t, hc)
		want, count := "aura-box-a", 6
		if kind == Browser {
			want, count = "aura-browser-a", 2
		}
		if hc.Mounts[0].Source != want || len(hc.Mounts) != count {
			t.Fatalf("kind %v: %v", kind, hc.Mounts)
		}
	}
}

func TestBrowserRoutingIsIndependentOfShell(t *testing.T) {
	b := &fakeBackend{}
	r := NewSandboxRouter(b, config.SandboxConfig{IdleTTLSec: 1800, BrowserMemoryLimit: 123, BrowserPidsLimit: 456, BrowserCPULimit: 3})
	ctx := identityctx.WithIdentityID(context.Background(), "a")
	if _, err := r.Route(ctx); err != nil {
		t.Fatal(err)
	}
	if len(b.resolved) != 1 {
		t.Fatal("browser created during shell routing")
	}
	h, err := r.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != Browser || b.resolved[1].Kind != Browser || b.resolved[1].Limits.MemoryBytes != 123 {
		t.Fatalf("browser = %+v; specs = %+v", h, b.resolved)
	}
	if err := r.Destroy(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if len(b.stopped) != 2 {
		t.Fatalf("purged %+v", b.stopped)
	}
}

// TestResolveStartsABrowserBoxWithoutASlot: starting a browser box takes no admission slot, so
// the boot start brings back every identity's box however many the host admits. A slot is a
// Chrome, and the browser call that can open one asks for it (operator, 2026-10-15).
func TestResolveStartsABrowserBoxWithoutASlot(t *testing.T) {
	const id = "b"
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/volumes/create"):
			return dockerJSONResponse(http.StatusCreated, `{}`), nil
		case strings.HasSuffix(req.URL.Path, "/containers/json") && strings.Contains(req.URL.Query().Get("filters"), workspaceName(id, Browser)):
			return dockerJSONResponse(http.StatusOK, `[]`), nil
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			return dockerJSONResponse(http.StatusOK, `[{"Id":"other","Names":["/aura-browser-a"]}]`), nil
		case strings.Contains(req.URL.Path, "/images/"):
			return dockerJSONResponse(http.StatusOK, `{}`), nil
		}
		return nil, fmt.Errorf("stop here: %s %s", req.Method, req.URL.Path)
	})
	backend := NewDockerBackend(cli, "aura-sandbox:test", Resources{}, WithBrowserLimits(Resources{}, 1))
	_, err := backend.Resolve(context.Background(), SandboxSpec{IdentityID: id, Kind: Browser})
	if !strings.Contains(fmt.Sprint(err), "create box") {
		t.Fatalf("Resolve = %v; want it to reach the box create, past any admission", err)
	}
}

func TestBrowserAdmissionDerivation(t *testing.T) {
	for _, tc := range []struct {
		total, limit int64
		want         int
	}{
		{16 << 30, 2 << 30, 4}, {15 << 30, 2 << 30, 3}, {1 << 30, 2 << 30, 1}, {0, 0, 1},
	} {
		if got := browserCapacity(tc.total, tc.limit); got != tc.want {
			t.Errorf("capacity(%d,%d) = %d, want %d", tc.total, tc.limit, got, tc.want)
		}
	}
}
