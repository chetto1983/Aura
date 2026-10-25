package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/plugins"
)

// PluginBackend is the plugin backend plugin_pack installs, upgrades and removes through
// (internal/plugins.Backend). The cockpit drives the same one.
type PluginBackend interface {
	Review(ctx context.Context, ref string) (plugins.Review, error)
	Install(ctx context.Context, actor string, req plugins.InstallRequest) (plugins.Report, error)
	Upgrade(ctx context.Context, actor, name string) (plugins.UpgradeReport, error)
	UpgradeAll(ctx context.Context, actor string) ([]plugins.UpgradeReport, error)
	Remove(ctx context.Context, actor, name string, deleteData bool) (plugins.RemoveReport, error)
	Get(name string) (plugins.Record, bool, error)
	MayAdminister(ctx context.Context, identityID string) (bool, error)
}

// PluginAction is what a plugin approval names.
type PluginAction struct {
	Action     string
	Ref        string
	Name       string
	Commit     string
	Digest     string
	DeleteData bool
}

// PluginApprovals is the plugin approvals' own store. It is not the shell's: separate
// instances keep the key spaces apart, so the shell's resume hook cannot approve a plugin.
type PluginApprovals = ApprovalStore[PluginAction]

// PluginApprovalContext is the resume_context type of a plugin approval.
const PluginApprovalContext = "plugin_pack_approval"

var (
	errNoPluginBackend = errors.New("plugin_pack: installing, upgrading and removing plugins is not available here — " +
		"this deployment has no plugin backend (it needs AURA_PLUGINS_DIR, a database and a skills directory)")
	errPluginNotOperator = errors.New("plugin_pack: only the operator (identity.create) installs, upgrades and removes plugins. " +
		"Ask them to do it in the cockpit, Governance → Plugins, or from their own conversation")
)

// operator returns the caller when they may administer plugins.
func (t *PackTool) operator(ctx context.Context) (string, error) {
	if t.Backend == nil {
		return "", errNoPluginBackend
	}
	actor := identityctx.IdentityID(ctx)
	ok, err := t.Backend.MayAdminister(ctx, actor)
	if err != nil {
		return "", fmt.Errorf("plugin_pack: check who is asking: %w", err)
	}
	if !ok {
		return "", errPluginNotOperator
	}
	return actor, nil
}

// install reviews the plugin and asks; the repeat of an approved review installs it once. A
// repeat that is not approved — another commit, a replayed one, a refused question — is
// answered with a fresh review and challenge.
func (t *PackTool) install(ctx context.Context, a packArgs) (ToolResult, error) {
	actor, err := t.operator(ctx)
	if err != nil {
		return ToolResult{}, err
	}
	session := SessionIDFromContext(ctx)
	if a.Commit != "" || a.Digest != "" {
		if t.Approvals.Consume(session, pluginApprovalDigest("install", a.Ref, a.Commit, a.Digest)) {
			report, err := t.Backend.Install(ctx, actor, plugins.InstallRequest{Ref: a.Ref, Commit: a.Commit, Digest: a.Digest})
			if err != nil && !report.Started() {
				return ToolResult{}, fmt.Errorf("plugin_pack install %s: %w", a.Ref, err)
			}
			return NewResult(ctx, installReport(report, err))
		}
	}
	review, err := t.Backend.Review(ctx, a.Ref)
	if err != nil {
		return ToolResult{}, err
	}
	// An install of an installed plugin can only be refused after the accept, so it gets no card.
	if rec, installed, err := t.Backend.Get(review.Name); err != nil || installed {
		if err != nil {
			return ToolResult{}, fmt.Errorf("plugin_pack install %s: %w", review.Ref, err)
		}
		return NewResult(ctx, fmt.Sprintf("Plugin %s is already installed at %s (%s). The repository's default branch has %s (%s). "+
			"To move to it: plugin_pack action=upgrade name=%s.\n", rec.Name, rec.Version, rec.Commit, review.Version, review.Commit, rec.Name))
	}
	digest := pluginApprovalDigest("install", review.Ref, review.Commit, review.Digest)
	question, err := review.Question(digest)
	if err != nil {
		return ToolResult{}, fmt.Errorf("plugin_pack install %s: %w", review.Ref, err)
	}
	challenge := t.Approvals.CreateChallenge(session, digest, question,
		PluginAction{Action: "install", Ref: review.Ref, Commit: review.Commit, Digest: review.Digest})
	return pluginApprovalRequired(challenge, map[string]any{
		"action": "install", "ref": review.Ref, "commit": review.Commit, "digest": review.Digest,
	}), nil
}

// remove asks, then removes once. The approval is bound to the installed commit: a reinstall
// at another commit since makes it count for nothing.
func (t *PackTool) remove(ctx context.Context, a packArgs) (ToolResult, error) {
	actor, err := t.operator(ctx)
	if err != nil {
		return ToolResult{}, err
	}
	name := strings.TrimSpace(a.Name)
	rec, ok, err := t.Backend.Get(name)
	if err != nil {
		return ToolResult{}, fmt.Errorf("plugin_pack remove %q: %w", name, err)
	}
	if !ok {
		return ToolResult{}, fmt.Errorf("plugin_pack: %q is not installed", name)
	}
	session := SessionIDFromContext(ctx)
	digest := pluginApprovalDigest("remove", rec.Name, rec.Commit, strconv.FormatBool(a.DeleteData))
	if a.Commit == rec.Commit && t.Approvals.Consume(session, digest) {
		report, err := t.Backend.Remove(ctx, actor, rec.Name, a.DeleteData)
		if err != nil && !report.Started() {
			return ToolResult{}, fmt.Errorf("plugin_pack remove %s: %w", rec.Name, err)
		}
		return NewResult(ctx, removeReport(report, err))
	}
	question, err := plugins.RemovalQuestion(rec, a.DeleteData, digest)
	if err != nil {
		return ToolResult{}, fmt.Errorf("plugin_pack remove %s: %w", rec.Name, err)
	}
	challenge := t.Approvals.CreateChallenge(session, digest, question,
		PluginAction{Action: "remove", Name: rec.Name, Commit: rec.Commit, DeleteData: a.DeleteData})
	return pluginApprovalRequired(challenge, map[string]any{
		"action": "remove", "name": rec.Name, "commit": rec.Commit, "delete_data": a.DeleteData,
	}), nil
}

func pluginApprovalDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// pluginApprovalRequired is Aura's own text, marked trusted so the model copies the question
// and resume_context exactly, as shell_exec's challenge is (spec 2026-10-12). Every string
// the plugin controls is quoted and capped in the question (plugins.Review.Question), and
// json.Marshal's HTML-safe encoding keeps <, > and & out of the payload.
func pluginApprovalRequired(c ApprovalChallenge[PluginAction], repeat map[string]any) ToolResult {
	payload := map[string]any{
		"error":           "plugin_approval_required",
		"approval_sha256": c.Digest,
		"question":        c.Question,
		"repeat":          repeat,
		"resume_context":  map[string]string{"type": PluginApprovalContext, "approval_sha256": c.Digest},
		"message": "This needs the operator's approval. Call ask_user with kind=\"approval\", question exactly equal to the question field " +
			"and resume_context exactly equal to the resume_context field. After the operator accepts, call plugin_pack again with " +
			"exactly the arguments in repeat. If they decline, do not retry.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"plugin_approval_required"}`)
	}
	return ToolResult{
		Preview: string(raw), Bytes: len(raw),
		Provenance: &ToolResultProvenance{Source: "aura:plugin_approval", Trust: TrustTrusted},
	}
}

// installReport is the account of an install. One that stopped part way says why first, then
// what landed: the tree is claimed and some of it may be live, which the operator must know.
func installReport(r plugins.Report, stopped error) string {
	var b strings.Builder
	plugin := strings.TrimSpace(r.Review.Name + " " + r.Review.Version)
	if stopped != nil {
		slog.Warn("plugin_pack: the install stopped part way", "plugin", r.Review.Name, "err", stopped)
		fmt.Fprintf(&b, "The install of plugin %s from %s@%s stopped part way: %v\nWhat it did:\n", plugin, r.Review.Ref, r.Review.Commit, stopped)
	} else {
		fmt.Fprintf(&b, "Installed plugin %s from %s@%s.\n", plugin, r.Review.Ref, r.Review.Commit)
	}
	writeSteps(&b, "skills", r.Skills)
	writeSteps(&b, "connectors", r.Connectors)
	if slices.ContainsFunc(r.Connectors, func(s plugins.Step) bool { return s.Outcome == plugins.OutcomeAttention }) {
		b.WriteString("A connector that needs attention waits for the operator, who authorizes it in the cockpit: Governance → MCP servers.\n")
	}
	return b.String()
}

// removeReport is the account of a removal. One that stopped part way says why, and that a
// second removal finishes the job.
func removeReport(r plugins.RemoveReport, stopped error) string {
	var b strings.Builder
	switch {
	case stopped != nil:
		slog.Warn("plugin_pack: the removal stopped part way", "plugin", r.Name, "err", stopped)
		fmt.Fprintf(&b, "The removal of plugin %s stopped part way: %v\nRemoving it again finishes the job. What it did:\n", r.Name, stopped)
	case r.DataDeleted:
		fmt.Fprintf(&b, "Removed plugin %s. Its data was deleted in your box.\n", r.Name)
	default:
		fmt.Fprintf(&b, "Removed plugin %s. Its data was kept.\n", r.Name)
	}
	writeSteps(&b, "skills", r.Skills)
	writeSteps(&b, "connectors", r.Connectors)
	return b.String()
}

func writeSteps(b *strings.Builder, label string, steps []plugins.Step) {
	fmt.Fprintf(b, "%s:\n", label)
	if len(steps) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, s := range steps {
		fmt.Fprintf(b, "- %s: %s", s.Name, s.Outcome)
		if s.Reason != "" {
			fmt.Fprintf(b, " — %s", s.Reason)
		}
		b.WriteString("\n")
	}
}
