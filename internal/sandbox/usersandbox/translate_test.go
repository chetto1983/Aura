package usersandbox

import (
	"flag"
	"strconv"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"pgregory.net/rapid"
)

// fatalf is the minimal test sink shared by the table (*testing.T) and property
// (*rapid.T) drivers of the SBX-02 behavioral assertion.
type fatalf interface {
	Fatalf(format string, args ...any)
}

// assertPinsSafe is the SBX-02 acceptance predicate: whatever the (possibly adversarial)
// SandboxSpec, toHostConfig must pin every host-exposure field safe. The docker socket is
// a HOST PATH, so its only mount vector is a bind — toHostConfig emits volume+tmpfs only,
// making the socket unrepresentable. A volume whose NAME merely mimics the socket path is
// a Docker volume name, not a host bind, so we assert on mount TYPE + TARGET, not Source.
func assertPinsSafe(t fatalf, hc *container.HostConfig) {
	if hc.Privileged {
		t.Fatalf("Privileged must be pinned false")
	}
	if hc.NetworkMode.IsHost() {
		t.Fatalf("NetworkMode must never be host, got %q", string(hc.NetworkMode))
	}
	if hc.Binds != nil {
		t.Fatalf("Binds must be pinned nil (volumes only, D-10), got %v", hc.Binds)
	}
	if hc.AutoRemove {
		t.Fatalf("AutoRemove must be pinned false — the box must be suspendable, not --rm")
	}
	if hc.Runtime != "" && hc.Runtime != "runsc" {
		t.Fatalf("Runtime must be \"\" or \"runsc\" (never a dangerous runtime), got %q", hc.Runtime)
	}
	for _, m := range hc.Mounts {
		if m.Type == mount.TypeBind {
			t.Fatalf("no bind mount allowed — host paths incl. the docker socket must be unrepresentable (got %q -> %q)", m.Source, m.Target)
		}
		if strings.Contains(m.Target, "docker.sock") {
			t.Fatalf("no mount may target the docker socket, got target %q", m.Target)
		}
	}
}

// TestTranslate_PinsSafe proves SBX-02 behaviorally over a table of hand-picked
// adversarial specs plus ≥1000 rapid-generated ones: toHostConfig ALWAYS emits a
// HostConfig with Privileged=false, non-host NetworkMode, nil Binds, AutoRemove=false,
// a safe Runtime, and exactly the three sanctioned volume/tmpfs mounts (no bind, no
// socket target).
func TestTranslate_PinsSafe(t *testing.T) {
	// (1) Hand-picked adversarial specs — note the workspace volume NAME mimicking the
	// docker socket path (case 1) and an out-of-range RuntimeClass (case 3).
	adversarial := []SandboxSpec{
		{}, // zero value
		{IdentityID: "--privileged", Image: "host", WorkspaceVol: "/var/run/docker.sock", Runtime: Runc},
		{IdentityID: "id", Image: "img", WorkspaceVol: "aura-box-id", Runtime: Runsc, Egress: EgressPolicy{FQDNAllowlist: []string{"host", "0.0.0.0/0"}}},
		{IdentityID: "id", WorkspaceVol: "/proc", Runtime: RuntimeClass(99), Limits: Resources{NanoCPUs: -1, MemoryBytes: -1, PidsLimit: -1}},
		{IdentityID: "id", WorkspaceVol: "aura-box-id", Runtime: Runc, Limits: Resources{NanoCPUs: 1 << 62, MemoryBytes: 1 << 62, PidsLimit: 1 << 62}},
	}
	for i, s := range adversarial {
		hc := toHostConfig(s)
		assertPinsSafe(t, hc)
		if len(hc.Mounts) != 3 {
			t.Fatalf("case %d: want exactly 3 mounts (workspace vol + tmpfs scratch + uv-cache vol), got %d", i, len(hc.Mounts))
		}
	}

	// (2) Property: for ≥1000 rapid-generated adversarial specs, the pins always hold.
	raiseRapidChecks(t, 1000)
	rapid.Check(t, func(rt *rapid.T) {
		spec := SandboxSpec{
			IdentityID:   rapid.String().Draw(rt, "identityID"),
			Image:        rapid.String().Draw(rt, "image"),
			WorkspaceVol: rapid.String().Draw(rt, "workspaceVol"),
			Runtime:      RuntimeClass(rapid.IntRange(-4, 4).Draw(rt, "runtime")),
			Egress: EgressPolicy{
				Floor:         rapid.Bool().Draw(rt, "floor"),
				FQDNAllowlist: rapid.SliceOf(rapid.String()).Draw(rt, "fqdnAllowlist"),
			},
			Limits: Resources{
				NanoCPUs:    rapid.Int64().Draw(rt, "nanoCPUs"),
				MemoryBytes: rapid.Int64().Draw(rt, "memoryBytes"),
				PidsLimit:   rapid.Int64().Draw(rt, "pidsLimit"),
			},
		}
		assertPinsSafe(rt, toHostConfig(spec))
	})
}

// raiseRapidChecks raises rapid's per-property check count to at least n for the calling
// test (respecting a higher -rapid.checks override and restoring the prior value after),
// so the SBX-02 property clears the ≥1000 adversarial-spec floor without a package-wide
// TestMain that later plans in this package would collide with.
func raiseRapidChecks(t *testing.T, n int) {
	t.Helper()
	f := flag.Lookup("rapid.checks")
	if f == nil {
		t.Fatalf("rapid.checks flag not registered — cannot guarantee the ≥%d adversarial-spec floor", n)
	}
	cur, err := strconv.Atoi(f.Value.String())
	if err != nil || cur >= n {
		return
	}
	prev := f.Value.String()
	if err := f.Value.Set(strconv.Itoa(n)); err != nil {
		t.Fatalf("could not raise rapid.checks to %d: %v", n, err)
	}
	t.Cleanup(func() { _ = f.Value.Set(prev) })
}
