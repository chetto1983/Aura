package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestBuildRegistry_RegistersAskUser guards the production wiring gap found in
// Phase-4 verification: ask_user is non-deferred and MUST appear in the live tool
// manifest, or the LLM never calls it and the entire HITL pause/resume flow is
// unreachable from `aura chat`. Unit/runner tests register the tool manually, so
// only an assertion against buildRegistry() itself catches the omission.
func TestBuildRegistry_RegistersAskUser(t *testing.T) {
	reg := buildRegistry()
	askUserName := tools.AskUser{}.Spec().Name
	if _, ok := reg.Get(askUserName); !ok {
		t.Fatalf("production registry is missing %q — the LLM cannot trigger HITL pause", askUserName)
	}
}

// TestBuildRegistry_CoreToolsPresent locks the rest of the always-on manifest so a
// future edit cannot silently drop a tool the agent depends on.
func TestBuildRegistry_CoreToolsPresent(t *testing.T) {
	reg := buildRegistry()
	for _, name := range []string{
		tools.TextResponse{}.Spec().Name,
		tools.CurrentTime{}.Spec().Name,
		(&tools.ReadToolOutput{}).Spec().Name,
		(&tools.ToolSearch{}).Spec().Name,
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("production registry is missing core tool %q", name)
		}
	}
}
