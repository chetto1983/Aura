package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/scoring"
)

// This file wires the skill tool's CATALOG + INSTALL actions (Slice 7d, plan 11-06),
// replacing the "not yet wired" placeholders from 11-02. Together they complete the
// model-facing discovery→install loop:
//
//   - action=catalog {query}: browse skills.sh (/api/search) — DEFAULT-ON (D-12), the
//     model can discover skills out of the box; a disabled catalog returns guidance.
//   - action=install {repo}: clone + stage a third-party skill into pending/ via the
//     installer seam, surfacing the supply-chain red flags (D-13), then PAUSE the turn
//     via ask_user for human approval. The model CANNOT self-approve the install (D-03):
//     activation is the human resume (skills.ResumeHandler) or the `aura skills` CLI.
//
// The tools package stays free of an internal/skills import: skillCatalog +
// skillInstaller are consumer-declared seams the live *skills.CatalogClient /
// *skills.Installer satisfy through thin adapters wired at the composition root.

// skillCatalog is the consumer-declared catalog seam the action=catalog handler
// dispatches against. The live internal/skills.CatalogClient satisfies it through an
// adapter wired at registration (cmd/aura). Search returns ranked results; Disabled
// reports the D-12 escape-hatch state so the handler can return guidance without dialing.
type skillCatalog interface {
	// Search queries the catalog and returns results ranked by installs descending.
	Search(ctx context.Context, query string) ([]CatalogResult, error)
	// Disabled reports whether the catalog is turned off (D-12). When true the handler
	// returns disable guidance instead of dialing.
	Disabled() bool
}

// CatalogResult is the tool-local projection of one catalog search hit (decoupled
// from internal/skills.CatalogItem so the tools package declares its own seam).
type CatalogResult struct {
	Name     string
	Source   string
	Installs int
}

// skillInstaller is the consumer-declared install seam the action=install handler
// dispatches against. The live internal/skills.Installer satisfies it through an
// adapter. Install clones + stages the skill into pending/ + audit (it NEVER
// activates) and returns the red-flag summary + tier for the ask_user gate.
type skillInstaller interface {
	// Install lands the repo's skill in pending/ + audit, returning the install
	// summary (name, content hash, red flags, whether always:true was stripped, tier).
	Install(ctx context.Context, repoURL string) (InstallSummary, error)
}

// InstallSummary is the tool-local projection of an install outcome the gate frames.
type InstallSummary struct {
	Name           string
	ContentHash    string
	AlwaysStripped bool
	RedFlags       []string // human-readable red-flag details (D-13)
	Tier           string   // risk tier label for the pause question
}

// skillCatalogArgs / skillInstallArgs are the per-action wire shapes.
type skillCatalogArgs struct {
	Action string `json:"action"`
	Query  string `json:"query"`
}

type skillInstallArgs struct {
	Action string `json:"action"`
	Repo   string `json:"repo"`
	Query  string `json:"query"` // tolerated alias when the model passes the repo as `query`
}

// actionCatalog handles action=catalog: browse skills.sh for installable skills
// (default-ON, D-12). It ranks by installs and returns a compact NewResult. A disabled
// catalog returns the enable-guidance instead of dialing. An empty query is guarded.
func (t *SkillTool) actionCatalog(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a skillCatalogArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill catalog args: %w", err)
	}
	if t.Catalog == nil {
		return ToolResult{}, fmt.Errorf("skill catalog: no catalog is wired in this context")
	}
	if t.Catalog.Disabled() {
		return NewResult(ctx, "The skill catalog is disabled. An operator can re-enable it with `aura skills catalog enable-catalog`.")
	}
	query := strings.TrimSpace(a.Query)
	if query == "" {
		return ToolResult{}, fmt.Errorf("skill catalog: a query is required (e.g. action=catalog with query=\"spreadsheet\")")
	}

	results, err := t.Catalog.Search(ctx, query)
	if err != nil {
		return ToolResult{}, fmt.Errorf("skill catalog %q: %w", query, err)
	}
	return NewResult(ctx, renderCatalog(query, results))
}

// renderCatalog formats the ranked results into a compact, model-readable listing.
func renderCatalog(query string, results []CatalogResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No skills found for %q.", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Skills matching %q (ranked by installs). To install one, call action=install with its source as `repo`:\n\n", query)
	for _, r := range results {
		src := r.Source
		if src == "" {
			src = "(source unavailable)"
		}
		fmt.Fprintf(&b, "- %s  (installs=%d)  source=%s\n", r.Name, r.Installs, src)
	}
	return b.String()
}

// actionInstall handles action=install: clone + stage a third-party skill into
// pending/ via the installer seam, then PAUSE for human approval surfacing the
// supply-chain red flags (D-13). The model CANNOT self-approve (D-03) — activation is
// the human resume or the `aura skills approve` CLI. The repo may be passed as `repo`
// or (tolerated) `query`.
func (t *SkillTool) actionInstall(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a skillInstallArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill install args: %w", err)
	}
	repo := strings.TrimSpace(a.Repo)
	if repo == "" {
		repo = strings.TrimSpace(a.Query)
	}
	if repo == "" {
		return ToolResult{}, fmt.Errorf("skill install: a repo URL is required (e.g. action=install with repo=\"https://github.com/owner/skill\")")
	}
	if t.Installer == nil {
		return ToolResult{}, fmt.Errorf("skill install: no installer is wired in this context")
	}

	// The installer clones + stages into pending/ + audit; it NEVER activates. A
	// clone/validation failure (a blocklist hit, a missing SKILL.md, an unreachable
	// repo) returns here as an error — surfaced as a tool error so the model self-
	// corrects, NOT a pause.
	summary, err := t.Installer.Install(ctx, repo)
	if err != nil {
		return ToolResult{}, fmt.Errorf("skill install %q: %w", repo, err)
	}

	// Pause for human approval, surfacing the red flags (D-13). The mutation is already
	// pending and CANNOT self-activate. A headless context (no interactive resume) gets
	// the optional alert so the operator learns of the attempt.
	if t.Alerter != nil {
		t.Alerter.AlertPendingSkill(ctx, summary.Name, "install", tierFromLabel(summary.Tier))
	}

	question := renderInstallGate(repo, summary)
	return ToolResult{}, &ErrAwaitingUserInput{
		Question: question,
		Kind:     KindApproval,
		Priority: 70, // install gate ranks with the Risky write gates (above routine clarifications)
	}
}

// tierFromLabel maps the installer's tier label string onto a scoring.RiskTier for
// the headless alert. An unrecognized label falls back to Risky (an install is at
// least Risky — conservative, never Safe).
func tierFromLabel(label string) scoring.RiskTier {
	switch scoring.RiskTier(label) {
	case scoring.Safe, scoring.Normal, scoring.Risky, scoring.Destructive:
		return scoring.RiskTier(label)
	default:
		return scoring.Risky
	}
}

// renderInstallGate frames the install-approval pause: the staged skill, whether
// always:true was stripped, and the supply-chain red flags the human must weigh (D-13).
func renderInstallGate(repo string, s InstallSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Approve installing skill %q from %s (risk=%s)? It is staged as pending and will NOT take effect until you approve.\n",
		s.Name, repo, s.Tier)
	if s.AlwaysStripped {
		b.WriteString("- Note: the skill declared always:true; that always-on flag was stripped on install (re-enable later via `aura skills always`).\n")
	}
	if len(s.RedFlags) == 0 {
		b.WriteString("- No supply-chain red flags detected.\n")
	} else {
		b.WriteString("- Supply-chain red flags (review before approving):\n")
		for _, f := range s.RedFlags {
			fmt.Fprintf(&b, "    * %s\n", f)
		}
	}
	b.WriteString("Approve to activate, or decline to discard the pending install. (Bundled scripts never run automatically — only via sandbox_exec.)")
	return b.String()
}
