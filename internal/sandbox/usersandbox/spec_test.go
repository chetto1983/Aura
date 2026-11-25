package usersandbox

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestSpec_NoHostExposureFields is the SBX-02 STRUCTURAL guard: it pins the exact field
// set of SandboxSpec (and its nested types) with a golden list and rejects any field —
// at any depth — whose name or type expresses a host-exposure escape. Host re-exposure
// is unrepresentable because there is NO field to set, not because a check rejects it;
// this test fails the moment someone adds one (forcing an SBX-02 review).
func TestSpec_NoHostExposureFields(t *testing.T) {
	got := collectSpecFields(reflect.TypeFor[SandboxSpec](), "")
	sort.Strings(got)

	want := []string{
		"Egress",
		"Egress.Floor",
		"IdentityID",
		"Image",
		"Kind",
		"Limits",
		"Limits.MemoryBytes",
		"Limits.NanoCPUs",
		"Limits.PidsLimit",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SandboxSpec field set drifted — SBX-02 requires reviewing EVERY added field for host exposure.\n got: %v\nwant: %v", got, want)
	}

	// Semantic guard: no field (at any depth) may name a host-exposure escape.
	forbidden := []string{
		"privileged", "network", "bind", "device", "capadd", "capdrop",
		"socket", "sysctl", "hostpath", "hostnetwork", "pidmode", "ipc", "userns", "workspacevol",
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
	assertNoMobyTypes(t, reflect.TypeFor[SandboxSpec]())
}

// collectSpecFields returns the dotted field names of a struct type, recursing into
// nested struct fields so an escape cannot hide inside EgressPolicy/Resources.
func collectSpecFields(t reflect.Type, prefix string) []string {
	var out []string
	for f := range t.Fields() {
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
	for field := range typ.Fields() {
		ft := field.Type
		elem := ft
		for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array || elem.Kind() == reflect.Map {
			elem = elem.Elem()
		}
		if strings.HasPrefix(elem.PkgPath(), "github.com/moby") {
			t.Fatalf("SandboxSpec field %q is a moby type %s — host-config types must stay behind toHostConfig (SBX-02)", field.Name, elem.String())
		}
		if elem.Kind() == reflect.Struct {
			assertNoMobyTypes(t, elem)
		}
	}
}
