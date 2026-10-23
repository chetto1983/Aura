package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
)

func packRef(t *testing.T) packs.Ref {
	t.Helper()
	ref, err := packs.ParseRef("anthropics/knowledge-work-plugins/sales")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	return ref
}

func emptyConfig() mcp.ManagedConfig {
	return mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{}}
}

// Every connector a pack installs arrives BLOCKED. That is what makes the single
// approval the operator runs next meaningful — a pack must not be able to grant
// itself the trust it wants.
func TestStagePackServerArrivesBlockedAndMarked(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	ref := packRef(t)

	if err := stagePackServer(&doc, ref, "hubspot", packs.Server{
		Type: "http", URL: "https://mcp.hubspot.com/anthropic",
	}); err != nil {
		t.Fatalf("stagePackServer: %v", err)
	}

	got := doc.MCPServers["hubspot"]
	if got.Trust.Class != mcp.TrustBlocked {
		t.Errorf("trust = %q, want blocked", got.Trust.Class)
	}
	if got.Source != "pack:anthropics/knowledge-work-plugins/sales" {
		t.Errorf("source = %q — the pack marker is what the single trust-approve ranges over", got.Source)
	}
	if got.URL != "https://mcp.hubspot.com/anthropic" || got.Type != "http" {
		t.Errorf("connector lost its target: %+v", got)
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Error("connector arrived disabled; blocked is the gate, not enabled=false")
	}
}

func TestStagePackServerCarriesStdioArgvAndEnv(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	if err := stagePackServer(&doc, packRef(t), "notes", packs.Server{
		Type: "stdio", Command: "notes-mcp",
		Args: []string{"--vault", "/data"},
		Env:  map[string]string{"B": "2", "A": "1"},
	}); err != nil {
		t.Fatalf("stagePackServer: %v", err)
	}
	got := doc.MCPServers["notes"]
	if got.Command != "notes-mcp" || len(got.Args) != 2 {
		t.Errorf("argv lost: %+v", got)
	}
	// Sorted, because a map has no order and an unsorted write churns the file
	// on every install for no reason.
	if len(got.Env) != 2 || got.Env[0] != "A=1" || got.Env[1] != "B=2" {
		t.Errorf("env = %v, want sorted KEY=VALUE pairs", got.Env)
	}
}

// A pack's stdio connector is third-party code: it runs in the calling identity's
// sandbox box, never as a child of the Aura process, whose environment holds the
// database credentials.
func TestStagePackServerRunsAStdioConnectorInTheBox(t *testing.T) {
	t.Parallel()
	for name, server := range map[string]packs.Server{
		"typed":   {Type: "stdio", Command: "uvx", Args: []string{"--from", "git+https://github.com/chetto1983/broker-analysis-mcp@v0.1.0", "broker-analysis-mcp"}},
		"untyped": {Command: "notes-mcp"},
	} {
		doc := emptyConfig()
		if err := stagePackServer(&doc, packRef(t), name, server); err != nil {
			t.Fatalf("%s: stagePackServer: %v", name, err)
		}
		got := doc.MCPServers[name]
		if !mcp.IsBoxRuntime(got) {
			t.Errorf("%s: runtime = %q, want the identity's box", name, got.Runtime.Kind)
		}
		if mcp.BoxInitTimeout(got) != 120*time.Second {
			t.Errorf("%s: first-start budget = %v, want 120s", name, mcp.BoxInitTimeout(got))
		}
		if err := mcp.ValidateManagedServer(name, got); err != nil {
			t.Errorf("%s: the staged connector would not survive the write: %v", name, err)
		}
	}
}

func TestStagePackServerLeavesARemoteConnectorWithoutARuntime(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	if err := stagePackServer(&doc, packRef(t), "hubspot", packs.Server{Type: "http", URL: "https://mcp.hubspot.com/anthropic"}); err != nil {
		t.Fatalf("stagePackServer: %v", err)
	}
	if got := doc.MCPServers["hubspot"].Runtime; got.Kind != "" || got.InitTimeoutSec != 0 {
		t.Errorf("runtime = %+v, want none: a remote connector runs nowhere in Aura", got)
	}
}

// A box server takes no secrets: everything in the box is readable by the agent's
// shell. A pack that hands its stdio connector a credential is refused at the write,
// by name, instead of starting without it.
func TestStagePackServerWithASecretIsRefusedForTheBox(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	if err := stagePackServer(&doc, packRef(t), "crm", packs.Server{
		Command: "crm-mcp", Env: map[string]string{"CRM_API_KEY": "sk-live"},
	}); err != nil {
		t.Fatalf("stagePackServer: %v", err)
	}
	err := mcp.ValidateManagedServer("crm", doc.MCPServers["crm"])
	if err == nil || !strings.Contains(err.Error(), "CRM_API_KEY") || !strings.Contains(err.Error(), "box") {
		t.Fatalf("err = %v, want a refusal naming the key and the box", err)
	}
}

// The existing server may be one the operator configured by hand. A pack must
// not silently replace it.
func TestStagePackServerRefusesANameAlreadyTaken(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	doc.MCPServers["slack"] = mcp.ManagedServer{Source: "manual"}

	err := stagePackServer(&doc, packRef(t), "slack", packs.Server{URL: "https://mcp.slack.com/mcp"})
	if err == nil {
		t.Fatal("a pack overwrote a hand-configured server")
	}
	if !strings.Contains(err.Error(), "manual") {
		t.Errorf("the refusal does not say who holds the name: %v", err)
	}
	if doc.MCPServers["slack"].Source != "manual" {
		t.Error("the existing server was mutated anyway")
	}
}

func TestStagePackServerReportsAReinstallDistinctly(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	ref := packRef(t)
	if err := stagePackServer(&doc, ref, "slack", packs.Server{URL: "u"}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	err := stagePackServer(&doc, ref, "slack", packs.Server{URL: "u"})
	if err == nil || !strings.Contains(err.Error(), "already installed from this pack") {
		t.Fatalf("err = %v, want a distinct reinstall message", err)
	}
}

func TestStagePackServerJoinsTheActiveProfile(t *testing.T) {
	t.Parallel()
	doc := emptyConfig()
	if err := stagePackServer(&doc, packRef(t), "hubspot", packs.Server{URL: "u"}); err != nil {
		t.Fatalf("stagePackServer: %v", err)
	}
	profile := doc.Profiles[doc.ActiveProfileName()]
	if len(profile.Servers) != 1 || profile.Servers[0] != "hubspot" {
		t.Errorf("connector did not join the active profile: %+v", doc.Profiles)
	}
}

func TestPackEnvPairsIsEmptyForNoEnv(t *testing.T) {
	t.Parallel()
	if got := packEnvPairs(nil); got != nil {
		t.Errorf("packEnvPairs(nil) = %v, want nil", got)
	}
}
