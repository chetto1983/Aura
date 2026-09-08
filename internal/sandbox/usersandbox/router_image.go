// router_image.go adds EnsureImage (D-03): the sandbox-image boot preflight's seam onto the
// backend's eager-pull capability. It mirrors runtimeChecker/artifactCopier/fileWriter's
// structural-capability pattern in router_tools.go — the capability is resolved via a type
// assertion on the concrete backend rather than widened onto the core Backend interface, so a
// backend without eager-pull support stays a valid Backend.
package usersandbox

import (
	"context"
	"fmt"
)

// imageEnsurer is the optional eager-pull capability the sandbox-image boot preflight needs:
// pull/verify the box image is present BEFORE any tool call reaches the lazy first-use path
// in DockerBackend.ensureImage. DockerBackend satisfies it (docker_backend_lifecycle.go).
type imageEnsurer interface {
	EnsureImage(ctx context.Context) error
}

// EnsureImage eagerly ensures the box image is present or pullable, without creating a box.
// It resolves the capability structurally; a backend that does not implement it returns a
// named error rather than a silent nil success — the boot preflight (D-03) must not read "no
// daemon I/O happened" as "the image is fine."
func (r *SandboxRouter) EnsureImage(ctx context.Context) error {
	e, ok := backendAs[imageEnsurer](r)
	if !ok {
		return fmt.Errorf("sandbox backend does not support eager image ensure")
	}
	return e.EnsureImage(ctx)
}

// egressImageEnsurer is the DISCRETIONARY egress-sidecar counterpart to imageEnsurer. Unlike
// imageEnsurer, a backend without this capability is not itself an error: egress probing is
// advisory (D-03 names only the box image as Fatal), so EnsureEgressImage degrades to a silent
// no-op rather than a named error.
type egressImageEnsurer interface {
	EnsureEgressImage(ctx context.Context) error
}

// EnsureEgressImage eagerly ensures the egress sidecar image is present or pullable. A backend
// without the capability is a silent no-op (nil) — never promoted to an error, matching the
// WARN-only treatment the boot preflight gives this check.
func (r *SandboxRouter) EnsureEgressImage(ctx context.Context) error {
	e, ok := backendAs[egressImageEnsurer](r)
	if !ok {
		return nil
	}
	return e.EnsureEgressImage(ctx)
}
