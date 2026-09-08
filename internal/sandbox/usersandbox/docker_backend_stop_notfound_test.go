package usersandbox

import (
	"errors"
	"fmt"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
)

// docker_backend_stop_notfound_test.go pins Stop's idempotency classifier. Daemon-free by
// necessity and by policy: DockerBackend.cli is a concrete *client.Client with no injection
// seam, and Stop itself only runs under docker_integration, which contributes zero coverage
// to the gate — so the pure logic it leans on would otherwise ship unasserted (CLAUDE.md).
//
// MEASURED 2026-09-08 on the live deployment: purging five identities whose boxes were
// already gone failed on the FIRST leg with `stop: remove box: Error response from daemon:
// No such container: aura-box-<id>`, while agui.SandboxPurger's own contract says
// "Idempotent -- destroying an absent box converges, because the backend does not raise
// removing something that is already gone." The backend did raise. Because the sandbox leg
// runs first and a failed step is never journalled, that made the whole purge unresumable
// for any identity whose box had been removed by anything other than a completed purge.

func TestIgnoreNotFoundCollapsesAbsentObjects(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		if err := ignoreNotFound(nil); err != nil {
			t.Fatalf("ignoreNotFound(nil) = %v, want nil", err)
		}
	})

	t.Run("the errdefs sentinel converges", func(t *testing.T) {
		if err := ignoreNotFound(cerrdefs.ErrNotFound); err != nil {
			t.Fatalf("ignoreNotFound(ErrNotFound) = %v, want nil", err)
		}
	})

	t.Run("a wrapped not-found converges", func(t *testing.T) {
		wrapped := fmt.Errorf("remove container: %w", cerrdefs.ErrNotFound)
		if err := ignoreNotFound(wrapped); err != nil {
			t.Fatalf("ignoreNotFound(wrapped) = %v, want nil", err)
		}
	})

	t.Run("moby's NotFound-interface shape converges", func(t *testing.T) {
		// The client's own objectNotFoundError is unexported and signals absence by
		// implementing NotFound(); cerrdefs.IsNotFound honours that interface as well as
		// the sentinel, and this is the shape a real ContainerRemove 404 arrives in.
		if err := ignoreNotFound(notFoundShape{}); err != nil {
			t.Fatalf("ignoreNotFound(NotFound-interface) = %v, want nil", err)
		}
	})

	t.Run("every other failure is preserved", func(t *testing.T) {
		boom := errors.New("Cannot connect to the Docker daemon")
		if err := ignoreNotFound(boom); !errors.Is(err, boom) {
			t.Fatalf("ignoreNotFound(boom) = %v, want it returned unchanged — an unreachable daemon is not an absent box", err)
		}
	})

	t.Run("a conflict is not a not-found", func(t *testing.T) {
		conflict := fmt.Errorf("remove: %w", cerrdefs.ErrConflict)
		if err := ignoreNotFound(conflict); err == nil {
			t.Fatal("ignoreNotFound(ErrConflict) = nil, want the failure preserved")
		}
	})
}

// notFoundShape mimics the moby client's unexported objectNotFoundError: absence signalled
// by the marker method rather than by a sentinel.
type notFoundShape struct{}

func (notFoundShape) NotFound()     {}
func (notFoundShape) Error() string { return "Error: No such container: aura-box-x" }
