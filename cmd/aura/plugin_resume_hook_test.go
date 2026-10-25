package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/plugins"
	"github.com/chetto1983/aura/internal/runner"
)

const pluginHookDigest = "4444444444444444444444444444444444444444444444444444444444444444"

// adminOnlyBackend answers MayAdminister for one identity; the hook needs nothing else.
type adminOnlyBackend struct{ admin string }

func (adminOnlyBackend) Review(context.Context, string) (plugins.Review, error) {
	return plugins.Review{}, nil
}
func (adminOnlyBackend) Install(context.Context, string, plugins.InstallRequest) (plugins.Report, error) {
	return plugins.Report{}, nil
}
func (adminOnlyBackend) Upgrade(context.Context, string, string) (plugins.UpgradeReport, error) {
	return plugins.UpgradeReport{}, nil
}
func (adminOnlyBackend) UpgradeAll(context.Context, string) ([]plugins.UpgradeReport, error) {
	return nil, nil
}
func (adminOnlyBackend) Remove(context.Context, string, string, bool) (plugins.RemoveReport, error) {
	return plugins.RemoveReport{}, nil
}
func (adminOnlyBackend) Get(string) (plugins.Record, bool, error) {
	return plugins.Record{}, false, nil
}
func (b adminOnlyBackend) MayAdminister(_ context.Context, identityID string) (bool, error) {
	return identityID == b.admin, nil
}

func pluginPending(t *testing.T, question, contextType, digestField string) askuser.Pending {
	t.Helper()
	return askuser.Pending{
		ConversationID: "conv-plugin",
		Kind:           tools.KindApproval,
		Question:       question,
		ResumeContext:  rawJSON(t, map[string]string{"type": contextType, digestField: pluginHookDigest}),
	}
}

// The two stores are separate instances, so the shell hook cannot approve a plugin
// challenge: relayed as a shell context the digest is unknown to it, and relayed as its own
// type the shell hook does not read it.
func TestShellHookCannotApproveAPluginChallenge(t *testing.T) {
	shell := tools.NewShellApprovals()
	plugin := &tools.PluginApprovals{}
	ch := plugin.CreateChallenge("conv-plugin", pluginHookDigest, "Approve plugin install?", tools.PluginAction{Action: "install"})
	hook := newShellResumeHook(shell)
	accept := runner.ResponseInput{Action: askuser.ActionAccept}

	err := hook(t.Context(), pluginPending(t, ch.Question, "shell_exec_approval", "command_sha256"), accept)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("shell hook on a plugin digest: err = %v, want not found", err)
	}
	if err := hook(t.Context(), pluginPending(t, ch.Question, "plugin_pack_approval", "approval_sha256"), accept); err != nil {
		t.Fatalf("shell hook on a plugin context: %v", err)
	}
	if plugin.Consume("conv-plugin", pluginHookDigest) || shell.Consume("conv-plugin", pluginHookDigest) {
		t.Fatal("the shell hook approved a plugin challenge")
	}
}

// Only an identity that may administer plugins counts as an accept, and a refused accept
// returns nil: the hook runs after the resolve is committed. An expiry discards the challenge
// whoever the context names.
func TestPluginResumeHookCountsOnlyAnAdminAndAlwaysDiscardsOnExpiry(t *testing.T) {
	pack := &tools.PackTool{Approvals: &tools.PluginApprovals{}}
	hook := newPluginResumeHook(pack)
	ch := pack.Approvals.CreateChallenge("conv-plugin", pluginHookDigest, "Approve plugin install?", tools.PluginAction{Action: "install"})
	pending := pluginPending(t, ch.Question, "plugin_pack_approval", "approval_sha256")
	accept := runner.ResponseInput{Action: askuser.ActionAccept}
	admin := identityctx.WithIdentityID(t.Context(), "admin-1")
	member := identityctx.WithIdentityID(t.Context(), "member-1")

	if err := hook(admin, pending, accept); err != nil {
		t.Fatalf("accept with no backend bound: %v", err)
	}
	pack.Backend = adminOnlyBackend{admin: "admin-1"}
	if err := hook(member, pending, accept); err != nil {
		t.Fatalf("a member's accept: %v", err)
	}
	if pack.Approvals.Consume("conv-plugin", pluginHookDigest) {
		t.Fatal("an accept counted without an administrator")
	}

	if err := hook(admin, pending, accept); err != nil {
		t.Fatalf("an administrator's accept: %v", err)
	}
	if !pack.Approvals.Consume("conv-plugin", pluginHookDigest) {
		t.Fatal("an administrator's accept was not recorded")
	}

	pack.Approvals.CreateChallenge("conv-plugin", pluginHookDigest, ch.Question, tools.PluginAction{})
	if err := hook(member, pending, runner.ResponseInput{Action: askuser.ActionExpired}); err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if _, ok := pack.Approvals.PendingChallenge("conv-plugin", pluginHookDigest); ok {
		t.Fatal("an expired challenge stayed pending")
	}
	if pack.Approvals.Consume("conv-plugin", pluginHookDigest) {
		t.Fatal("an expiry became an approval")
	}
	if newPluginResumeHook(nil) != nil || newPluginResumeHook(&tools.PackTool{}) != nil {
		t.Fatal("a hook without a store must be nil, as chainResumeHooks filters")
	}
}
