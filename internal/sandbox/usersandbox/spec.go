// Package usersandbox defines the per-identity sandbox TYPE LAYER (SBX-02): the
// SandboxSpec that makes host re-exposure UNREPRESENTABLE (it deliberately has no field
// for privileged / host-network / bind-mount / docker-socket / runtime), the single private
// translator that pins the dangerous moby HostConfig fields to safe constants
// (translate.go), and the Backend seam speaking the E2B verbs (backend.go). No Docker
// client work lives here — the moby client binding is owned by the DockerBackend plan.
package usersandbox

// BoxKind fixes the volume and mount policy; callers cannot select arbitrary volumes.
type BoxKind int

// Shell and Browser never share credentials, processes or network namespaces.
const (
	Shell BoxKind = iota
	Browser
)

func workspaceName(id string, kind BoxKind) string {
	if kind == Browser {
		return "aura-browser-" + id
	}
	return boxName(id)
}

func sidecarIdentity(id string, kind BoxKind) string {
	if kind == Browser {
		return "browser-" + id
	}
	return id
}

// Resources is the cgroup cap set applied to the box (D-14). PidsLimit is a plain int64
// here; the translator converts it to the moby *int64.
type Resources struct {
	NanoCPUs    int64
	MemoryBytes int64
	PidsLimit   int64
}

// SandboxSpec is the ONLY box-config type a caller constructs. Its security property is
// what it OMITS: there is no Privileged, no NetworkMode/host-net, no Binds, no Devices, no
// CapAdd, no docker-socket path and no runtime — host re-exposure is unrepresentable
// (SBX-02), not merely validated. Every box runs on the daemon's default runtime, runc:
// gVisor was retired on 2026-10-11 (prd.md §12). toHostConfig (translate.go) is the single
// crossing that turns a spec into a moby HostConfig with the dangerous fields pinned safe.
type SandboxSpec struct {
	Kind       BoxKind
	IdentityID string
	Image      string
	Limits     Resources
}
