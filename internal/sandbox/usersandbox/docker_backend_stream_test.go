// docker_backend_stream_test.go locks the D-02 seam for the 37-09 background streamed exec: the
// streamed/detached verb is a CONCRETE DockerBackend method reachable only via a structural
// type-assertion, NOT a 6th Backend verb — the Backend interface must stay exactly
// Resolve/Exec/Suspend/Resume/Stop so the DGX agent-sandbox E2B impl drops in unmodified. Untagged
// (runs locally); no dockerd needed — these are type-level assertions.

package usersandbox

import (
	"context"
	"io"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// execStreamerIface is the EXACT concrete signature the router surfaces (docker_backend_exec.go).
type execStreamerIface interface {
	ExecStream(context.Context, BoxHandle, ExecRequest, io.Writer) (*ExecStreamHandle, error)
}

// TestBackend_StreamVerbNotOnInterface asserts the streamed exec stays OFF the Backend seam (D-02).
func TestBackend_StreamVerbNotOnInterface(t *testing.T) {
	// A backend with ONLY the 5 verbs still satisfies Backend — ExecStream is not required by it.
	var _ Backend = (*fakeBackend)(nil)

	// A Backend value must NOT expose ExecStream (the verb is off the interface).
	var b Backend = &fakeBackend{}
	if _, ok := b.(execStreamerIface); ok {
		t.Fatal("Backend must NOT expose ExecStream — D-02 locks the seam at 5 verbs (Resolve/Exec/Suspend/Resume/Stop)")
	}

	// DockerBackend DOES expose it concretely — the router reaches it structurally, never via Backend.
	var _ execStreamerIface = (*DockerBackend)(nil)
}

// TestRouterExecStream_UnsupportedBackendErrors proves the router turns a backend WITHOUT the
// streaming capability into a clean error (the tool maps it to a fail-CLOSED deny), never a panic
// or a host fallback.
func TestRouterExecStream_UnsupportedBackendErrors(t *testing.T) {
	r := NewSandboxRouter(&fakeBackend{t: t}, config.ProfileSingleUserHardened, unitSandboxConfig())
	if _, err := r.ExecStream(context.Background(), BoxHandle{ContainerID: "c"}, ExecRequest{Command: "true"}, io.Discard); err == nil {
		t.Fatal("router.ExecStream must error when the backend lacks background streaming (fail-CLOSED)")
	}
}
