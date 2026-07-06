// Package usersandbox defines the per-identity sandbox TYPE LAYER (SBX-02): the
// SandboxSpec that makes host re-exposure UNREPRESENTABLE (it deliberately has no field
// for privileged / host-network / bind-mount / docker-socket), the single private
// translator that pins the dangerous moby HostConfig fields to safe constants
// (translate.go), and the Backend seam speaking the E2B verbs (backend.go). No Docker
// client work lives here — the moby client binding is owned by the DockerBackend plan.
package usersandbox

import (
	"errors"

	"github.com/chetto1983/aura/internal/config"
)

// RuntimeClass is the container runtime selector — a typed, INT-backed enum, deliberately
// not a free string, so it can never carry "host" or any other dangerous runtime name
// (SBX-02 prohibition). Its zero value is Runc, the safe default; Runsc (gVisor) is gated
// to server_production by NewSandboxSpec.
type RuntimeClass int

const (
	// Runc is the default OCI runtime (moby HostConfig.Runtime ""). It is the zero value.
	Runc RuntimeClass = iota
	// Runsc is the gVisor runtime (moby HostConfig.Runtime "runsc"); server_production only.
	Runsc
)

// dockerRuntime maps the enum to the moby HostConfig.Runtime string. It is total: Runc —
// and any unknown value — yields "" (runc, the safe default); ONLY Runsc yields "runsc".
// No input can produce "host" or another dangerous runtime.
func (rc RuntimeClass) dockerRuntime() string {
	if rc == Runsc {
		return "runsc"
	}
	return ""
}

// EgressPolicy is the box network egress posture. Floor is the always-on default tenancy
// floor (drop RFC1918 + cloud metadata); FQDNAllowlist is the opt-in tightened allowlist.
// There is deliberately no "host"/"none-with-holes" escape here.
type EgressPolicy struct {
	Floor         bool
	FQDNAllowlist []string
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
// CapAdd, and no docker-socket path — host re-exposure is unrepresentable (SBX-02), not
// merely validated. toHostConfig (translate.go) is the single crossing that turns a spec
// into a moby HostConfig with the dangerous fields pinned safe.
type SandboxSpec struct {
	IdentityID   string
	Image        string
	WorkspaceVol string
	Runtime      RuntimeClass
	Egress       EgressPolicy
	Limits       Resources
}

// ErrRunscRequiresServerProduction is returned by NewSandboxSpec when Runsc (gVisor) is
// requested outside the server_production profile (D-12: runsc is server_production only).
var ErrRunscRequiresServerProduction = errors.New("usersandbox: runsc runtime requires server_production profile")

// NewSandboxSpec is the sanctioned SandboxSpec builder/validator. It enforces the one
// runtime rule the type system alone cannot: Runsc is accepted only under
// server_production; every other profile requesting Runsc is rejected with
// ErrRunscRequiresServerProduction. Every host-exposure escape is already unrepresentable
// (no field exists), so there is nothing else to validate for containment.
func NewSandboxSpec(spec SandboxSpec, profile config.RuntimeProfile) (SandboxSpec, error) {
	if spec.Runtime == Runsc && profile != config.ProfileServerProduction {
		return SandboxSpec{}, ErrRunscRequiresServerProduction
	}
	return spec, nil
}
