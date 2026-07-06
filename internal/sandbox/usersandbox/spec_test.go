package usersandbox

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// TestSpec_NoHostExposureFields is the SBX-02 STRUCTURAL guard: it pins the exact field
// set of SandboxSpec (and its nested types) with a golden list and rejects any field —
// at any depth — whose name or type expresses a host-exposure escape. Host re-exposure
// is unrepresentable because there is NO field to set, not because a check rejects it;
// this test fails the moment someone adds one (forcing an SBX-02 review).
func TestSpec_NoHostExposureFields(t *testing.T) {
	got := collectSpecFields(reflect.TypeOf(SandboxSpec{}), "")
	sort.Strings(got)

	want := []string{
		"Egress",
		"Egress.FQDNAllowlist",
		"Egress.Floor",
		"IdentityID",
		"Image",
		"Limits",
		"Limits.MemoryBytes",
		"Limits.NanoCPUs",
		"Limits.PidsLimit",
		"Runtime",
		"WorkspaceVol",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SandboxSpec field set drifted — SBX-02 requires reviewing EVERY added field for host exposure.\n got: %v\nwant: %v", got, want)
	}

	// Semantic guard: no field (at any depth) may name a host-exposure escape.
	forbidden := []string{
		"privileged", "network", "bind", "device", "capadd", "capdrop",
		"socket", "sysctl", "hostpath", "hostnetwork", "pidmode", "ipc", "userns",
	}
	for _, f := range got {
		lower := strings.ToLower(f)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("SandboxSpec field %q contains forbidden host-exposure token %q (SBX-02: escapes must be unrepresentable)", f, bad)
			}
		}
	}

	// Structural guard: SandboxSpec must not reference any moby type — the moby
	// HostConfig lives ONLY behind toHostConfig (translate.go).
	assertNoMobyTypes(t, reflect.TypeOf(SandboxSpec{}))
}

// collectSpecFields returns the dotted field names of a struct type, recursing into
// nested struct fields so an escape cannot hide inside EgressPolicy/Resources.
func collectSpecFields(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := prefix + f.Name
		out = append(out, name)
		if f.Type.Kind() == reflect.Struct {
			out = append(out, collectSpecFields(f.Type, name+".")...)
		}
	}
	return out
}

// assertNoMobyTypes fails if any field of typ (recursively, unwrapping pointers/slices)
// is declared with a github.com/moby type — SandboxSpec is Aura's clean abstraction and
// must never carry a moby HostConfig/NetworkMode/mount type.
func assertNoMobyTypes(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		ft := typ.Field(i).Type
		elem := ft
		for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array || elem.Kind() == reflect.Map {
			elem = elem.Elem()
		}
		if strings.HasPrefix(elem.PkgPath(), "github.com/moby") {
			t.Fatalf("SandboxSpec field %q is a moby type %s — host-config types must stay behind toHostConfig (SBX-02)", typ.Field(i).Name, elem.String())
		}
		if elem.Kind() == reflect.Struct {
			assertNoMobyTypes(t, elem)
		}
	}
}

// TestSpec_RunscOnlyServerProduction pins the one runtime rule the type system alone
// cannot: Runsc (gVisor) is accepted ONLY under server_production; every other profile
// requesting it is rejected. A runc spec is always accepted and never becomes runsc, and
// RuntimeClass is a non-string enum so a free-string runtime (e.g. "host") is impossible.
func TestSpec_RunscOnlyServerProduction(t *testing.T) {
	runscSpec := func() SandboxSpec {
		return SandboxSpec{
			IdentityID:   "id-1",
			Image:        "img@sha256:deadbeef",
			WorkspaceVol: "aura-box-id-1",
			Runtime:      Runsc,
			Egress:       EgressPolicy{Floor: true},
			Limits:       Resources{NanoCPUs: 2_000_000_000, MemoryBytes: 2 << 30, PidsLimit: 512},
		}
	}

	// (a) Runsc is REJECTED under every non-server_production profile — including
	// single_user_hardened, which is Strict() yet still not server_production.
	for _, p := range []config.RuntimeProfile{
		config.ProfileDev,
		config.ProfileLocalTrusted,
		config.ProfileSingleUserHardened,
	} {
		if _, err := NewSandboxSpec(runscSpec(), p); !errors.Is(err, ErrRunscRequiresServerProduction) {
			t.Fatalf("profile %q: want ErrRunscRequiresServerProduction, got %v", p, err)
		}
	}

	// (b) Runsc is ACCEPTED under server_production and translates to the gVisor runtime.
	spec, err := NewSandboxSpec(runscSpec(), config.ProfileServerProduction)
	if err != nil {
		t.Fatalf("server_production must accept Runsc: %v", err)
	}
	if spec.Runtime != Runsc {
		t.Fatalf("accepted spec must retain Runsc, got %v", spec.Runtime)
	}
	if got := toHostConfig(spec).Runtime; got != "runsc" {
		t.Fatalf("Runsc must translate to moby runtime \"runsc\", got %q", got)
	}

	// (c) A runc spec is accepted under EVERY profile and NEVER becomes runsc.
	for _, p := range []config.RuntimeProfile{
		config.ProfileDev, config.ProfileLocalTrusted,
		config.ProfileSingleUserHardened, config.ProfileServerProduction,
	} {
		runcSpec := runscSpec()
		runcSpec.Runtime = Runc
		out, err := NewSandboxSpec(runcSpec, p)
		if err != nil {
			t.Fatalf("profile %q: runc must always be accepted: %v", p, err)
		}
		if hc := toHostConfig(out); hc.Runtime != "" {
			t.Fatalf("profile %q: runc must translate to moby runtime \"\" (never runsc), got %q", p, hc.Runtime)
		}
	}

	// (d) RuntimeClass is a typed non-string enum — a free-string runtime is
	// structurally unrepresentable, so it can never carry "host".
	if k := reflect.TypeOf(Runc).Kind(); k == reflect.String {
		t.Fatalf("RuntimeClass must NOT be string-backed (a free string could carry \"host\"); got kind %v", k)
	}
}
