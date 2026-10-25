package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/plugins"
)

// upgrade moves an installed plugin, or every one, to the head of its repository's default
// branch, as `codex plugin marketplace upgrade [NAME]` does. Like Codex it asks nothing: the
// operator trusted the source at install and an upgrade reads the same ref. Only the operator
// may run it, as install and remove.
func (t *PackTool) upgrade(ctx context.Context, a packArgs) (ToolResult, error) {
	actor, err := t.operator(ctx)
	if err != nil {
		return ToolResult{}, err
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		reports, err := t.Backend.UpgradeAll(ctx, actor)
		var b strings.Builder
		for _, r := range reports {
			if r.Started() || r.UpToDate {
				b.WriteString(upgradeReport(r, nil))
			}
		}
		if err != nil {
			fmt.Fprintf(&b, "Some upgrades failed: %v\n", err)
		}
		return NewResult(ctx, b.String())
	}
	report, err := t.Backend.Upgrade(ctx, actor, name)
	if err != nil && !report.Started() {
		return ToolResult{}, fmt.Errorf("plugin_pack upgrade %s: %w", name, err)
	}
	return NewResult(ctx, upgradeReport(report, err))
}

// upgradeReport is the account of one upgrade. It names the version before and after, so a
// reply about the upgrade rests on what the backend did and not on what the repository holds.
func upgradeReport(r plugins.UpgradeReport, stopped error) string {
	if r.UpToDate {
		return fmt.Sprintf("Plugin %s is up to date at %s (%s).\n", r.Name, r.Version, r.Commit)
	}
	var b strings.Builder
	if stopped != nil {
		slog.Warn("plugin_pack: the upgrade stopped part way", "plugin", r.Name, "err", stopped)
		fmt.Fprintf(&b, "The upgrade of plugin %s to %s stopped part way: %v\nWhat it did:\n", r.Name, r.Version, stopped)
	} else {
		fmt.Fprintf(&b, "Upgraded plugin %s from %s (%s) to %s (%s).\n", r.Name, r.FromVersion, r.FromCommit, r.Version, r.Commit)
	}
	writeSteps(&b, "skills", r.Skills)
	writeSteps(&b, "connectors", r.Connectors)
	return b.String()
}
