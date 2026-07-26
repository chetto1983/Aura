package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

func TestAdaptiveBenchmarkRestrictedRegistryExposesOnlyReadOnlySurface(
	t *testing.T,
) {
	t.Parallel()
	full := adaptiveBenchmarkRegistryFixture(t)
	restricted, err := restrictAdaptiveBenchmarkRegistry(full)
	if err != nil {
		t.Fatalf("restrictAdaptiveBenchmarkRegistry: %v", err)
	}
	names := make([]string, 0, len(restricted.All()))
	for _, tool := range restricted.All() {
		names = append(names, tool.Spec().Name)
	}
	slices.Sort(names)
	want := []string{
		"document_search",
		"read_tool_output",
		"skill",
		"text_response",
		"tool_search",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("restricted names=%v want=%v", names, want)
	}
	if _, ok := restricted.Get("shell_exec"); ok {
		t.Fatal("restricted registry retained a mutation tool")
	}
	search, ok := restricted.Get("tool_search")
	if !ok {
		t.Fatal("restricted registry has no tool_search")
	}
	callCtx := tools.WithToolCallContext(
		t.Context(),
		"adaptive-benchmark-test",
		"tool-search",
		t.TempDir(),
		4096,
	)
	result, err := search.Execute(
		callCtx,
		json.RawMessage(`{"query":"select:shell_exec"}`),
	)
	if err != nil {
		t.Fatalf("restricted tool_search: %v", err)
	}
	if strings.Contains(result.Preview, "shell_exec") {
		t.Fatalf("tool_search exposed removed tool: %q", result.Preview)
	}
}

func TestAdaptiveBenchmarkRestrictedSkillAllowsOnlyNonblankListQuery(
	t *testing.T,
) {
	t.Parallel()
	restricted, err := restrictAdaptiveBenchmarkRegistry(
		adaptiveBenchmarkRegistryFixture(t),
	)
	if err != nil {
		t.Fatalf("restrictAdaptiveBenchmarkRegistry: %v", err)
	}
	skill, ok := restricted.Get("skill")
	if !ok {
		t.Fatal("restricted registry has no skill tool")
	}
	if spec := skill.Spec(); spec.Mutating ||
		spec.Multiplexed ||
		strings.Contains(string(spec.Parameters), `"use"`) {
		t.Fatalf("restricted skill spec is unsafe: %#v", spec)
	}
	callCtx := tools.WithToolCallContext(
		t.Context(),
		"adaptive-benchmark-test",
		"skill-list",
		t.TempDir(),
		4096,
	)
	for _, payload := range []string{
		`{"action":"use","name":"anything"}`,
		`{"action":"list"}`,
		`{"action":"list","query":" "}`,
		`{"action":"list","query":"safe","name":"extra"}`,
		`{"action":"list","query":" safe "}`,
	} {
		if _, err := skill.Execute(
			callCtx,
			json.RawMessage(payload),
		); err == nil {
			t.Fatalf("restricted skill accepted %s", payload)
		}
	}
	result, err := skill.Execute(
		callCtx,
		json.RawMessage(`{"action":"list","query":"benchmark"}`),
	)
	if err != nil || result.Preview == "" {
		t.Fatalf("safe skill list result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkRestrictedRegistryRejectsMissingOrSpoofedTools(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*tools.Registry)
	}{
		{
			name: "missing required",
			mutate: func(registry *tools.Registry) {
				registry.Register(tools.TextResponse{})
			},
		},
		{
			name: "spoofed tool search",
			mutate: func(registry *tools.Registry) {
				registry.Register(adaptiveBenchmarkNamedTool{name: "tool_search"})
				registry.Register(tools.TextResponse{})
				registry.Register(&tools.DocumentSearch{})
				registry.Register(newSkillToolWithAdaptive(
					&config.Config{SkillsDir: t.TempDir()},
					nil,
					nil,
					nil,
				))
			},
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			registry := tools.NewRegistry()
			testCase.mutate(registry)
			if restricted, err := restrictAdaptiveBenchmarkRegistry(
				registry,
			); err == nil {
				t.Fatalf("accepted restricted registry %#v", restricted)
			}
		})
	}
}

func adaptiveBenchmarkRegistryFixture(t *testing.T) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&tools.ToolSearch{Registry: registry})
	registry.Register(&tools.DocumentSearch{})
	registry.Register(&tools.ReadToolOutput{})
	cfg := &config.Config{
		SkillsDir:         t.TempDir(),
		SkillBodyCapBytes: 1 << 20,
	}
	registry.Register(newSkillToolWithAdaptive(cfg, nil, nil, nil))
	registry.Register(adaptiveBenchmarkNamedTool{name: "shell_exec"})
	return registry
}

type adaptiveBenchmarkNamedTool struct {
	name string
}

func (tool adaptiveBenchmarkNamedTool) Spec() tools.Spec {
	return tools.Spec{
		Name: tool.name, Summary: "unsafe fixture",
		Description: "unsafe fixture",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Deferred:    true,
		Mutating:    true,
	}
}

func (adaptiveBenchmarkNamedTool) Execute(
	context.Context,
	json.RawMessage,
) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
