package usersandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/moby/moby/client"
)

// router_image_test.go covers SandboxRouter.EnsureImage/EnsureEgressImage (D-03): the
// sandbox-image boot preflight's seam onto the backend's eager-pull capability.
//
// TestEnsureImagePullsOnInspectMiss drives DockerBackend.EnsureImage (via a real
// SandboxRouter over a real *DockerBackend) against a FAKE docker client — no daemon
// required. The moby client's own unexported WithMockClient test helper (client_mock_test.go)
// lives in a _test.go file and is therefore not importable from this package, so
// fakeDockerRoundTripper below is the same technique, reimplemented at the http.RoundTripper
// seam the client package documents (client_options.go's testRoundTripper comment: "We
// define it here so we can detect the tlsconfig ... for testing").

// fakeDockerRoundTripper lets a test answer Docker Engine API requests entirely in-process.
type fakeDockerRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeDockerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newFakeDockerClient builds a *client.Client whose HTTP transport never leaves the process.
// client.WithAPIVersion pins a fixed version so the client skips its normal /_ping version
// negotiation — no daemon, no negotiation round trip, exactly the "no daemon" requirement.
func newFakeDockerClient(t *testing.T, doer func(*http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	cli, err := client.New(
		client.WithHTTPClient(&http.Client{Transport: fakeDockerRoundTripper(doer)}),
		client.WithAPIVersion("1.51"),
	)
	if err != nil {
		t.Fatalf("newFakeDockerClient: %v", err)
	}
	return cli
}

// dockerJSONResponse builds a minimal Docker Engine API response the client's checkResponseErr
// / json.Unmarshal round trip accepts.
func dockerJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestEnsureImagePullsOnInspectMiss proves the three branches of DockerBackend.ensureImage,
// reached through the full SandboxRouter.EnsureImage -> DockerBackend.EnsureImage seam: no
// pull on an inspect hit, a pull on an inspect miss, and the wrapped pull error on a failed
// pull. All three run against a fake docker client — no daemon.
func TestEnsureImagePullsOnInspectMiss(t *testing.T) {
	t.Run("inspect hit skips pull", func(t *testing.T) {
		pullCalled := false
		cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/"):
				return dockerJSONResponse(http.StatusOK, `{}`), nil
			case req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/images/create"):
				pullCalled = true
				return dockerJSONResponse(http.StatusOK, ``), nil
			}
			return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		})
		backend := NewDockerBackend(cli, "aura-sandbox:latest", Resources{})
		r := NewSandboxRouter(backend, config.ProfileSingleUserHardened, unitSandboxConfig())

		if err := r.EnsureImage(context.Background()); err != nil {
			t.Fatalf("EnsureImage err = %v, want nil (image already present)", err)
		}
		if pullCalled {
			t.Fatal("ImagePull was called on an ImageInspect hit — want no pull")
		}
	})

	t.Run("inspect miss triggers pull", func(t *testing.T) {
		pullCalled := false
		cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/"):
				return dockerJSONResponse(http.StatusNotFound, `{"message":"no such image"}`), nil
			case req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/images/create"):
				pullCalled = true
				return dockerJSONResponse(http.StatusOK, ``), nil
			}
			return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		})
		backend := NewDockerBackend(cli, "aura-sandbox:latest", Resources{})
		r := NewSandboxRouter(backend, config.ProfileSingleUserHardened, unitSandboxConfig())

		if err := r.EnsureImage(context.Background()); err != nil {
			t.Fatalf("EnsureImage err = %v, want nil (pull succeeded)", err)
		}
		if !pullCalled {
			t.Fatal("ImagePull was never called on an ImageInspect miss — want a pull attempt")
		}
	})

	t.Run("failed pull returns a wrapped error", func(t *testing.T) {
		cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/"):
				return dockerJSONResponse(http.StatusNotFound, `{"message":"no such image"}`), nil
			case req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/images/create"):
				return dockerJSONResponse(http.StatusInternalServerError, `{"message":"registry unreachable"}`), nil
			}
			return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		})
		backend := NewDockerBackend(cli, "ghcr.io/example/aura-sandbox:edge", Resources{})
		r := NewSandboxRouter(backend, config.ProfileSingleUserHardened, unitSandboxConfig())

		err := r.EnsureImage(context.Background())
		if err == nil {
			t.Fatal("EnsureImage err = nil, want the wrapped pull error")
		}
		if !strings.Contains(err.Error(), "ghcr.io/example/aura-sandbox:edge") {
			t.Fatalf("err = %v, want it to name the image ref", err)
		}
	})
}

// TestEnsureImageOnBackendWithoutCapability proves EnsureImage returns a NAMED error — not a
// silent nil success — for a Backend that does not implement the imageEnsurer capability. The
// boot preflight (D-03) must never read "the backend has no opinion" as "the image is fine."
func TestEnsureImageOnBackendWithoutCapability(t *testing.T) {
	be := &fakeBackend{t: t} // router_test.go's fakeBackend implements Backend, not imageEnsurer
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())

	err := r.EnsureImage(context.Background())
	if err == nil {
		t.Fatal("EnsureImage err = nil, want a named capability error")
	}
	if !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("err = %v, want it to name the missing capability", err)
	}
}

// TestEnsureImageNilRouterDenies mirrors the router's other nil-receiver/nil-backend shared
// denial points (Route, EnsureBox): a zero-value/nil router must deny, never panic.
func TestEnsureImageNilRouterDenies(t *testing.T) {
	if err := (&SandboxRouter{}).EnsureImage(context.Background()); err == nil {
		t.Fatal("zero-value router EnsureImage err = nil, want a denial")
	}
	var nilRouter *SandboxRouter
	if err := nilRouter.EnsureImage(context.Background()); err == nil {
		t.Fatal("nil receiver EnsureImage err = nil, want a denial")
	}
}

// fakeImageEnsureBackend implements Backend + imageEnsurer for the DockerBackend-shaped
// preflight fake-router tests exercised from cmd/aura (composition-level tests live there);
// this local type also lets EnsureEgressImage's silent-skip-on-missing-capability branch be
// asserted here without pulling in a real DockerBackend for that half.
type fakeImageEnsureBackend struct {
	fakeBackend
	ensureImageErr error
}

func (f *fakeImageEnsureBackend) EnsureImage(context.Context) error { return f.ensureImageErr }

func TestEnsureImageResolvesCapabilityOnAnyBackend(t *testing.T) {
	be := &fakeImageEnsureBackend{fakeBackend: fakeBackend{t: t}}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())
	if err := r.EnsureImage(context.Background()); err != nil {
		t.Fatalf("EnsureImage err = %v, want nil", err)
	}

	sentinel := errors.New("boom")
	beErr := &fakeImageEnsureBackend{fakeBackend: fakeBackend{t: t}, ensureImageErr: sentinel}
	rErr := NewSandboxRouter(beErr, config.ProfileSingleUserHardened, unitSandboxConfig())
	if err := rErr.EnsureImage(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("EnsureImage err = %v, want %v", err, sentinel)
	}
}

// TestEnsureEgressImageSkipsSilentlyWithoutCapability proves the DISCRETIONARY egress check
// degrades to a silent no-op (nil) on a backend without the capability — never an error, since
// egress probing is advisory, unlike the box image itself.
func TestEnsureEgressImageSkipsSilentlyWithoutCapability(t *testing.T) {
	be := &fakeBackend{t: t}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())
	if err := r.EnsureEgressImage(context.Background()); err != nil {
		t.Fatalf("EnsureEgressImage err = %v, want nil (advisory, missing capability is not an error)", err)
	}
}

// TestDockerBackendEnsureEgressImageEmptyIsNoop proves an unset egress image (WithEgress never
// called) is a no-op success — there is no sidecar to ensure.
func TestDockerBackendEnsureEgressImageEmptyIsNoop(t *testing.T) {
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
	})
	backend := NewDockerBackend(cli, "aura-sandbox:latest", Resources{})
	if err := backend.EnsureEgressImage(context.Background()); err != nil {
		t.Fatalf("EnsureEgressImage err = %v, want nil (no egress image configured)", err)
	}
}
