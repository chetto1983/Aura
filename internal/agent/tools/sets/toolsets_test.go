package toolsets

import (
	"reflect"
	"slices"
	"testing"
)

func TestResolveToolsetsComposesAndDedupes(t *testing.T) {
	got, err := ResolveToolsets(ToolsetMemoryRead, ToolsetWebResearch, ToolsetMemoryRead)
	if err != nil {
		t.Fatalf("ResolveToolsets: %v", err)
	}
	want := []string{"search_memory", "file", "source", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}

func TestResolveToolsetsRejectsUnknownToolset(t *testing.T) {
	if _, err := ResolveToolsets("memory_write"); err == nil {
		t.Fatal("expected unknown toolset error")
	}
}

func TestResolveToolsetsUnknownToolsetDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveToolsets panicked for unknown toolset: %v", r)
		}
	}()
	if _, err := ResolveToolsets("not-a-toolset"); err == nil {
		t.Fatal("ResolveToolsets unknown toolset error = nil, want error")
	}
}

func TestToolsetsReturnClones(t *testing.T) {
	got, ok := Toolset(ToolsetMemoryRead)
	if !ok {
		t.Fatal("missing memory_read toolset")
	}
	got[0] = "mutated"
	again, _ := Toolset(ToolsetMemoryRead)
	if again[0] == "mutated" {
		t.Fatal("toolset result aliases internal slice")
	}
}

func TestSchedulerSafeExcludesRecursiveAndDangerousTools(t *testing.T) {
	safe := SchedulerSafeTools()
	for _, forbidden := range []string{
		"task",
		"spawn_aurabot",
		"run_aurabot_swarm",
		"execute_code",
		"execute_shell",
		"dev_tool",
	} {
		if slices.Contains(safe, forbidden) {
			t.Fatalf("scheduler_safe includes forbidden tool %q: %+v", forbidden, safe)
		}
	}
	for _, required := range []string{"search_memory", "file", "web"} {
		if !slices.Contains(safe, required) {
			t.Fatalf("scheduler_safe missing %q: %+v", required, safe)
		}
	}
}

func TestSandboxCodeToolsetIsExplicit(t *testing.T) {
	got, err := ResolveToolsets(ToolsetSandboxCode)
	if err != nil {
		t.Fatalf("ResolveToolsets: %v", err)
	}
	want := []string{"execute_code", "execute_shell", "dev_tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sandbox_code tools = %+v, want %+v", got, want)
	}
}

func TestFilterAllowedCleansAndKeepsRequestedOrder(t *testing.T) {
	got := FilterAllowed([]string{" web ", "write_file", "web", "file"}, SchedulerSafeTools())
	want := []string{"web", "file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered = %+v, want %+v", got, want)
	}
}

func TestRoleToolsMatchReadOnlyPresets(t *testing.T) {
	tests := map[string][]string{
		"librarian":   {"search_memory", "file", "source"},
		"critic":      {"search_memory", "file", "source"},
		"researcher":  {"web"},
		"skillsmith":  {"file"},
		"synthesizer": {"search_memory", "file", "source"},
	}
	for role, want := range tests {
		got, ok := RoleTools(role)
		if !ok {
			t.Fatalf("missing role %q", role)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s tools = %+v, want %+v", role, got, want)
		}
	}
	if _, ok := RoleTools("writer"); ok {
		t.Fatal("unexpected writer role")
	}
}

func TestRolePresetsReturnsDeepCopy(t *testing.T) {
	got := RolePresets()
	got["librarian"][0] = "mutated"
	again, _ := RoleTools("librarian")
	if again[0] == "mutated" {
		t.Fatal("role presets result aliases internal slice")
	}
}
