package toolsets

import (
	"reflect"
	"slices"
	"testing"
)

func TestResolveProfilesComposesAndDedupes(t *testing.T) {
	got, err := ResolveProfiles(ProfileMemoryRead, ProfileWebResearch, ProfileMemoryRead)
	if err != nil {
		t.Fatalf("ResolveProfiles: %v", err)
	}
	want := []string{"search_memory", "list_files", "read_file", "search_files", "list_sources", "read_source", "web_search", "web_fetch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}

func TestResolveProfilesRejectsUnknownProfile(t *testing.T) {
	if _, err := ResolveProfiles("memory_write"); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestResolveProfilesUnknownProfileDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveProfiles panicked for unknown profile: %v", r)
		}
	}()
	if _, err := ResolveProfiles("not-a-profile"); err == nil {
		t.Fatal("ResolveProfiles unknown profile error = nil, want error")
	}
}

func TestProfilesReturnClones(t *testing.T) {
	got, ok := Profile(ProfileMemoryRead)
	if !ok {
		t.Fatal("missing memory_read profile")
	}
	got[0] = "mutated"
	again, _ := Profile(ProfileMemoryRead)
	if again[0] == "mutated" {
		t.Fatal("profile result aliases internal slice")
	}
}

func TestSchedulerSafeExcludesRecursiveAndDangerousTools(t *testing.T) {
	safe := SchedulerSafeTools()
	for _, forbidden := range []string{
		"write_file",
		"apply_patch",
		"schedule_task",
		"run_task_now",
		"spawn_aurabot",
		"run_aurabot_swarm",
		"execute_code",
		"list_tools",
		"read_tool",
		"save_tool",
	} {
		if slices.Contains(safe, forbidden) {
			t.Fatalf("scheduler_safe includes forbidden tool %q: %+v", forbidden, safe)
		}
	}
	for _, required := range []string{"search_memory", "list_files", "read_file", "search_files", "web_search"} {
		if !slices.Contains(safe, required) {
			t.Fatalf("scheduler_safe missing %q: %+v", required, safe)
		}
	}
}

func TestSandboxCodeProfileIsExplicit(t *testing.T) {
	got, err := ResolveProfiles(ProfileSandboxCode)
	if err != nil {
		t.Fatalf("ResolveProfiles: %v", err)
	}
	want := []string{"execute_code", "list_tools", "read_tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sandbox_code tools = %+v, want %+v", got, want)
	}
	if slices.Contains(got, "save_tool") {
		t.Fatalf("sandbox_code should not include persistent save_tool by default: %+v", got)
	}
}

func TestFilterAllowedCleansAndKeepsRequestedOrder(t *testing.T) {
	got := FilterAllowed([]string{" web_search ", "write_file", "web_search", "read_file"}, SchedulerSafeTools())
	want := []string{"web_search", "read_file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered = %+v, want %+v", got, want)
	}
}

func TestRoleToolsMatchReadOnlyPresets(t *testing.T) {
	tests := map[string][]string{
		"librarian":   {"search_memory", "list_files", "read_file", "search_files", "list_sources", "read_source", "lint_sources"},
		"critic":      {"search_memory", "list_files", "read_file", "search_files", "lint_sources", "list_sources"},
		"researcher":  {"web_search", "web_fetch"},
		"skillsmith":  {"list_files", "read_file", "search_files"},
		"synthesizer": {"search_memory", "list_files", "read_file", "search_files", "list_sources", "read_source"},
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
