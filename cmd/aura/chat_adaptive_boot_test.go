package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adaptiveBootPolicyReaderFunc func(context.Context) (adaptive.Policy, error)

func (fn adaptiveBootPolicyReaderFunc) CurrentPolicy(
	ctx context.Context,
) (adaptive.Policy, error) {
	return fn(ctx)
}

func TestSelectAdaptiveBootPoolReadsOnceAndClampsUnsafePolicy(t *testing.T) {
	t.Parallel()

	valid := func(mode adaptive.PolicyMode) adaptive.Policy {
		rolloutBPS := int32(0)
		switch mode {
		case adaptive.PolicyCanary:
			rolloutBPS = 500
		case adaptive.PolicyActive:
			rolloutBPS = 10_000
		}
		return adaptive.Policy{
			Epoch:      7,
			Version:    "policy-7",
			Mode:       mode,
			RolloutBPS: rolloutBPS,
			Config:     []byte(`{}`),
		}
	}

	tests := []struct {
		name       string
		policy     adaptive.Policy
		readErr    error
		wantPool   bool
		wantReason string
	}{
		{name: "off retains diagnostics", policy: valid(adaptive.PolicyOff), wantPool: true},
		{name: "shadow retains diagnostics", policy: valid(adaptive.PolicyShadow), wantPool: true},
		{name: "rollback retains diagnostics", policy: valid(adaptive.PolicyRollback), wantPool: true},
		{
			name:       "canary is unsupported",
			policy:     valid(adaptive.PolicyCanary),
			wantReason: "unsupported policy mode",
		},
		{
			name:       "active is unsupported",
			policy:     valid(adaptive.PolicyActive),
			wantReason: "unsupported policy mode",
		},
		{
			name:       "read failure",
			readErr:    errors.New(strings.Repeat("database unavailable ", 100)),
			wantReason: "policy unavailable",
		},
		{
			name: "unknown mode",
			policy: adaptive.Policy{
				Epoch: 7, Version: "policy-7",
				Mode: adaptive.PolicyMode(strings.Repeat("invalid", 100)),
			},
			wantReason: "invalid policy",
		},
		{
			name: "invalid epoch",
			policy: adaptive.Policy{
				Version: "policy-7", Mode: adaptive.PolicyShadow, Config: []byte(`{}`),
			},
			wantReason: "invalid policy",
		},
		{
			name: "invalid rollout",
			policy: adaptive.Policy{
				Epoch: 7, Version: "policy-7", Mode: adaptive.PolicyShadow,
				RolloutBPS: 1, Config: []byte(`{}`),
			},
			wantReason: "invalid policy",
		},
		{
			name: "invalid config",
			policy: adaptive.Policy{
				Epoch: 7, Version: "policy-7", Mode: adaptive.PolicyShadow,
				Config: []byte(`[]`),
			},
			wantReason: "invalid policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := new(pgxpool.Pool)
			reads := 0
			reader := adaptiveBootPolicyReaderFunc(func(context.Context) (adaptive.Policy, error) {
				reads++
				return tt.policy, tt.readErr
			})
			var warning bytes.Buffer

			got := selectAdaptiveBootPool(
				context.Background(),
				pool,
				reader,
				&warning,
			)

			if reads != 1 {
				t.Fatalf("policy reads = %d, want exactly 1", reads)
			}
			if tt.wantPool {
				if got != pool {
					t.Fatalf("retained pool = %p, want authoritative pool %p", got, pool)
				}
				if warning.Len() != 0 {
					t.Fatalf("safe policy emitted warning %q", warning.String())
				}
				return
			}
			if got != nil {
				t.Fatalf("unsafe policy retained adaptive pool %p", got)
			}
			if lines := strings.Count(warning.String(), "\n"); lines != 1 {
				t.Fatalf("warnings = %d lines, want exactly 1: %q", lines, warning.String())
			}
			if warning.Len() > 128 {
				t.Fatalf("warning is not bounded: %d bytes", warning.Len())
			}
			if !strings.Contains(warning.String(), tt.wantReason) {
				t.Fatalf("warning %q lacks reason %q", warning.String(), tt.wantReason)
			}
		})
	}
}

func TestAdaptiveControlSetIsAllOrNothing(t *testing.T) {
	t.Parallel()

	disabled := newAdaptiveControlSet(nil, "openrouter", "production-model")
	if !adaptiveControlSetEmpty(disabled) {
		t.Fatal("nil adaptive pool produced a partial control set")
	}
	for _, tt := range []struct {
		name       string
		providerID string
		modelID    string
	}{
		{name: "missing provider", modelID: "production-model"},
		{name: "missing model", providerID: "openrouter"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			controls := newAdaptiveControlSet(
				new(pgxpool.Pool),
				tt.providerID,
				tt.modelID,
			)
			if !adaptiveControlSetEmpty(controls) {
				t.Fatal("invalid model binding produced a partial control set")
			}
		})
	}

	enabled := newAdaptiveControlSet(
		new(pgxpool.Pool),
		"openrouter",
		"production-model",
	)
	if enabled.skillRouting == nil ||
		enabled.documentRetrieval == nil ||
		enabled.memoryRecall == nil ||
		enabled.reasoning == nil {
		t.Fatal("live adaptive pool omitted one or more typed controls")
	}
}

func adaptiveControlSetEmpty(controls adaptiveControlSet) bool {
	return controls.skillRouting == nil &&
		controls.documentRetrieval == nil &&
		controls.memoryRecall == nil &&
		controls.reasoning == nil
}

func TestBuildBaseRegistryKeepsRuntimeStoresWhenAdaptiveIsClamped(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		LLM: llm.Config{
			Provider: "openrouter",
			Model:    "production-model",
		},
		SkillsDir:         t.TempDir(),
		SkillBodyCapBytes: 1 << 20,
		WorkspaceDir:      t.TempDir(),
	}
	runtimePool := new(pgxpool.Pool)
	taskStore := newCronTaskStore(runtimePool, nil)

	reg, _ := buildBaseRegistryWithAdaptiveControls(
		cfg,
		taskStore,
		nil,
		adaptiveControlSet{},
	)

	taskRegistered, ok := reg.Get((&tools.TaskTool{}).Spec().Name)
	if !ok {
		t.Fatal("clamping adaptive controls removed the task tool")
	}
	task, ok := taskRegistered.(*tools.TaskTool)
	if !ok || task.Store == nil {
		t.Fatalf("task persistence = %T/%v, want live store", taskRegistered, task)
	}
	skillRegistered, ok := reg.Get((&tools.SkillTool{}).Spec().Name)
	if !ok {
		t.Fatal("clamping adaptive controls removed the skill tool")
	}
	skill, ok := skillRegistered.(*tools.SkillTool)
	if !ok {
		t.Fatalf("skill tool type = %T", skillRegistered)
	}
	if skill.Writer == nil {
		t.Fatal("clamping adaptive controls disabled the skill writer")
	}
	if skill.Adaptive != nil {
		t.Fatal("clamped skill tool retained adaptive routing")
	}
	if _, ok := reg.Get((&tools.DocumentIndex{}).Spec().Name); !ok {
		t.Fatal("clamping adaptive controls removed document persistence")
	}
	assertRegistryAdaptiveState(t, reg, false)
}

func TestBuildBaseRegistryUsesOneCompleteAdaptiveControlSet(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		LLM: llm.Config{
			Provider: "openrouter",
			Model:    "production-model",
		},
		SkillsDir:         t.TempDir(),
		SkillBodyCapBytes: 1 << 20,
		WorkspaceDir:      t.TempDir(),
	}
	runtimePool := new(pgxpool.Pool)
	adaptivePool := new(pgxpool.Pool)
	controls := newAdaptiveControlSet(
		adaptivePool,
		cfg.LLM.Provider,
		cfg.LLM.Model,
	)

	reg, _ := buildBaseRegistryWithAdaptiveControls(
		cfg,
		newCronTaskStore(runtimePool, nil),
		nil,
		controls,
	)

	assertRegistryAdaptiveState(t, reg, true)
}

func TestBuildRegistryWithMCPKeepsMountedServicesWhenAdaptiveIsClamped(t *testing.T) {
	cfg := &config.Config{
		MCPServers: map[string]mcp.ServerConfig{
			"calculator": {
				Command: os.Args[0],
				Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
				Env:     []string{"AURA_MCP_HELPER=1"},
			},
		},
	}

	reg, _, closers, err := buildRegistryWithMCPAndAdaptiveControls(
		context.Background(),
		cfg,
		nil,
		nil,
		adaptiveControlSet{},
	)
	if err != nil {
		t.Fatalf("build clamped registry with MCP: %v", err)
	}
	t.Cleanup(func() { _ = closeMCPServers(closers) })
	calc, ok := reg.Get("calculator__calculate")
	if !ok {
		t.Fatal("adaptive clamp removed a mounted MCP service")
	}
	callCtx := tools.WithToolCallContext(
		context.Background(),
		"session",
		"call-1",
		t.TempDir(),
		2048,
	)
	result, err := calc.Execute(
		callCtx,
		json.RawMessage(`{"expression":"2+2"}`),
	)
	if err != nil || result.Preview != "4" {
		t.Fatalf("mounted MCP after clamp = result %q err %v", result.Preview, err)
	}
}

func assertRegistryAdaptiveState(
	t *testing.T,
	reg *tools.Registry,
	want bool,
) {
	t.Helper()
	if _, ok := reg.Get((&tools.ToolSearch{}).Spec().Name); !ok {
		t.Fatal("tool_search is not registered")
	}
	documentRegistered, ok := reg.Get((&tools.DocumentSearch{}).Spec().Name)
	if !ok {
		t.Fatal("document_search is not registered")
	}
	document, ok := documentRegistered.(*tools.DocumentSearch)
	if !ok {
		t.Fatalf("document_search type = %T", documentRegistered)
	}
	skillRegistered, ok := reg.Get((&tools.SkillTool{}).Spec().Name)
	if !ok {
		t.Fatal("skill is not registered")
	}
	skill, ok := skillRegistered.(*tools.SkillTool)
	if !ok {
		t.Fatalf("skill type = %T", skillRegistered)
	}
	got := []bool{
		skill.Adaptive != nil,
		document.Adaptive != nil,
	}
	for i, state := range got {
		if state != want {
			t.Fatalf("registry adaptive domain %d wired = %t, want %t", i, state, want)
		}
	}
}

func TestAssembleChatEnvAppliesOnePolicyClampToEveryAdaptiveDomain(t *testing.T) {
	tests := []struct {
		name       string
		policy     adaptive.Policy
		readErr    error
		wantActive bool
	}{
		{name: "off", policy: validAdaptiveBootTestPolicy(adaptive.PolicyOff), wantActive: true},
		{name: "shadow", policy: validAdaptiveBootTestPolicy(adaptive.PolicyShadow), wantActive: true},
		{name: "rollback", policy: validAdaptiveBootTestPolicy(adaptive.PolicyRollback), wantActive: true},
		{name: "canary", policy: validAdaptiveBootTestPolicy(adaptive.PolicyCanary)},
		{name: "active", policy: validAdaptiveBootTestPolicy(adaptive.PolicyActive)},
		{name: "read error", readErr: errors.New("policy unavailable")},
		{
			name: "invalid",
			policy: adaptive.Policy{
				Epoch: 7, Version: "policy-7",
				Mode: adaptive.PolicyMode("future"), Config: []byte(`{}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := unreachablePool(t)
			cfg := validBootConfig()
			cfg.SkillsDir = t.TempDir()
			cfg.SkillExportDir = t.TempDir()
			cfg.SkillBodyCapBytes = 1 << 20
			cfg.WorkspaceDir = t.TempDir()
			reads := 0
			reader := adaptiveBootPolicyReaderFunc(func(context.Context) (adaptive.Policy, error) {
				reads++
				return tt.policy, tt.readErr
			})
			var warnings bytes.Buffer
			var projectorOrder []string

			env, err := assembleChatEnvWithAdaptivePolicy(
				context.Background(),
				cfg,
				pool,
				func(context.Context, *knowledge.Config) (adaptiveGraphClient, error) {
					return &adaptiveProjectorClientSpy{order: &projectorOrder}, nil
				},
				reader,
				&warnings,
			)
			if err != nil {
				t.Fatalf("assembleChatEnvWithAdaptivePolicy: %v", err)
			}
			t.Cleanup(env.close)
			if reads != 1 {
				t.Fatalf("policy reads = %d, want exactly 1", reads)
			}
			assertRegistryAdaptiveState(t, env.reg, tt.wantActive)
			if got := runnerHasDynamicRecallControl(env.run); got != tt.wantActive {
				t.Fatalf("memory recall control wired = %t, want %t", got, tt.wantActive)
			}
			if got := runnerHasReasoningControl(env.run); got != tt.wantActive {
				t.Fatalf("reasoning control wired = %t, want %t", got, tt.wantActive)
			}
			if got := env.reasoningControl != nil; got != tt.wantActive {
				t.Fatalf("shared reasoning control retained = %t, want %t", got, tt.wantActive)
			}
			cronDeps := newCronAgentDeps(env)
			if got := cronDeps.ReasoningControl != nil; got != tt.wantActive {
				t.Fatalf("cron reasoning control wired = %t, want %t", got, tt.wantActive)
			}
			if tt.wantActive && cronDeps.ReasoningControl != env.reasoningControl {
				t.Fatal("cron did not receive the boot-clamped reasoning control")
			}
			assertRuntimeServicesRemainLive(t, env)
			if tt.wantActive && warnings.Len() != 0 {
				t.Fatalf("safe policy emitted warning %q", warnings.String())
			}
			if !tt.wantActive && strings.Count(warnings.String(), "\n") != 1 {
				t.Fatalf("unsafe policy warning = %q, want exactly one line", warnings.String())
			}
		})
	}
}

// An unopenable graph client must not stop `aura serve` from booting: mcp-neo4j-cypher is
// a Python sidecar the appliance does not require, and a fatal open made the whole process
// exit 71 on any host without it on PATH. Projection resumes on a later boot; the outbox
// keeps the events meanwhile.
func TestAssembleChatEnvBootsWithoutAReachableProjectorGraph(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	cfg.SkillsDir = t.TempDir()
	cfg.SkillExportDir = t.TempDir()
	cfg.SkillBodyCapBytes = 1 << 20
	cfg.WorkspaceDir = t.TempDir()
	var warnings bytes.Buffer

	env, err := assembleChatEnvWithAdaptivePolicy(
		context.Background(),
		cfg,
		pool,
		func(context.Context, *knowledge.Config) (adaptiveGraphClient, error) {
			return nil, errors.New(`spawn mcp-neo4j-cypher: executable file not found in $PATH`)
		},
		adaptiveBootPolicyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			return validAdaptiveBootTestPolicy(adaptive.PolicyOff), nil
		}),
		&warnings,
	)
	if err != nil {
		t.Fatalf("boot refused an unreachable projector graph: %v", err)
	}
	t.Cleanup(env.close)
	if env.adaptiveProjector != nil {
		t.Fatal("projector runtime built from an opener that returned an error")
	}
	if _, ok := env.reg.Get((&tools.TaskTool{}).Spec().Name); !ok {
		t.Fatal("an unreachable graph cost the boot its task tool")
	}
}

func validAdaptiveBootTestPolicy(mode adaptive.PolicyMode) adaptive.Policy {
	rolloutBPS := int32(0)
	switch mode {
	case adaptive.PolicyCanary:
		rolloutBPS = 500
	case adaptive.PolicyActive:
		rolloutBPS = 10_000
	}
	return adaptive.Policy{
		Epoch: 7, Version: "policy-7", Mode: mode,
		RolloutBPS: rolloutBPS, Config: []byte(`{}`),
	}
}

func runnerHasDynamicRecallControl(run any) bool {
	return runnerHasControl(run, "dynamicRecallControl")
}

func runnerHasReasoningControl(run any) bool {
	return runnerHasControl(run, "reasoningControl")
}

func runnerHasControl(run any, field string) bool {
	value := reflect.ValueOf(run)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return false
	}
	control := value.Elem().FieldByName(field)
	return control.IsValid() && !control.IsNil()
}

func assertRuntimeServicesRemainLive(t *testing.T, env *chatEnv) {
	t.Helper()
	if env.adaptiveProjector == nil {
		t.Fatal("adaptive clamp disabled the private projector")
	}
	taskRegistered, ok := env.reg.Get((&tools.TaskTool{}).Spec().Name)
	if !ok {
		t.Fatal("adaptive clamp removed the task tool")
	}
	task, ok := taskRegistered.(*tools.TaskTool)
	if !ok || task.Store == nil {
		t.Fatal("adaptive clamp disabled task persistence")
	}
	skillRegistered, ok := env.reg.Get((&tools.SkillTool{}).Spec().Name)
	if !ok {
		t.Fatal("adaptive clamp removed the skill tool")
	}
	skill, ok := skillRegistered.(*tools.SkillTool)
	if !ok || skill.Writer == nil {
		t.Fatal("adaptive clamp disabled the skill writer")
	}
}
