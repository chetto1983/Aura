package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
)

func upgradedReport() plugins.UpgradeReport {
	return plugins.UpgradeReport{
		Name: "broker-analysis", FromVersion: "0.1.1", FromCommit: pluginCommit,
		Version: "0.3.0", Commit: pluginCommitB, Applied: true,
		Skills:     []plugins.Step{{Name: "risk-analysis", Outcome: plugins.OutcomeOK, Reason: "updated"}},
		Connectors: []plugins.Step{{Name: "broker-analysis", Outcome: plugins.OutcomeOK}},
	}
}

// Codex upgrades with no question: the operator trusted the source at install, and an upgrade
// reads the same ref.
func TestPluginPackUpgradeRunsWithoutACard(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgrade = upgradedReport()
	res, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade","name":"broker-analysis"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"Upgraded plugin broker-analysis from 0.1.1 (" + pluginCommit + ") to 0.3.0 (" + pluginCommitB + ")",
		"- risk-analysis: ok — updated",
		"- broker-analysis: ok",
	} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("result lacks %q:\n%s", want, res.Preview)
		}
	}
	if strings.Contains(res.Preview, "plugin_approval_required") {
		t.Errorf("an upgrade asked for approval:\n%s", res.Preview)
	}
	if !slices.Equal(b.upgrades, []string{"broker-analysis"}) || !slices.Equal(b.actors, []string{pluginAdmin}) {
		t.Errorf("upgrades %v by %v", b.upgrades, b.actors)
	}
}

func TestPluginPackUpgradeSaysUpToDate(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgrade = plugins.UpgradeReport{Name: "broker-analysis", Version: "0.3.0", Commit: pluginCommitB, UpToDate: true}
	res, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade","name":"broker-analysis"}`)
	if err != nil || !strings.Contains(res.Preview, "broker-analysis is up to date at 0.3.0 ("+pluginCommitB+")") {
		t.Fatalf("Execute = (%q, %v)", res.Preview, err)
	}
}

func TestPluginPackUpgradeWithoutANameUpgradesEveryPlugin(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgrade = upgradedReport()
	res, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !slices.Equal(b.upgrades, []string{"*"}) || !strings.Contains(res.Preview, "Upgraded plugin broker-analysis") ||
		!strings.Contains(res.Preview, "other is up to date at 1.0.0") {
		t.Errorf("upgrades %v, result:\n%s", b.upgrades, res.Preview)
	}
}

func TestPluginPackUpgradeAllNamesEachFailure(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgrade = upgradedReport()
	b.upgradeErr = errors.New("third: github is unreachable")
	res, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade"}`)
	if err != nil || !strings.Contains(res.Preview, "Some upgrades failed: third: github is unreachable") ||
		!strings.Contains(res.Preview, "Upgraded plugin broker-analysis") {
		t.Fatalf("Execute = (%q, %v)", res.Preview, err)
	}
}

func TestPluginPackUpgradeRolledBackIsAnError(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgradeErr = fmt.Errorf("%w: broker-analysis stays at 0.1.1: connector broker-analysis: handshake failed", plugins.ErrUpgradeRolledBack)
	_, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade","name":"broker-analysis"}`)
	if !errors.Is(err, plugins.ErrUpgradeRolledBack) || !strings.Contains(err.Error(), "stays at 0.1.1") {
		t.Fatalf("Execute = %v, want the rollback", err)
	}
}

func TestPluginPackUpgradeStoppedPartWaySaysWhatItDid(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.upgrade = upgradedReport()
	b.upgradeErr = errors.New("write the record: disk full")
	res, err := callPlugin(t, pluginTool(b), pluginAdmin, `{"action":"upgrade","name":"broker-analysis"}`)
	if err != nil || !strings.Contains(res.Preview, "The upgrade of plugin broker-analysis to 0.3.0 stopped part way: write the record: disk full") ||
		!strings.Contains(res.Preview, "- risk-analysis: ok — updated") {
		t.Fatalf("Execute = (%q, %v)", res.Preview, err)
	}
}

func TestPluginPackUpgradeRefusesAMember(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	_, err := callPlugin(t, pluginTool(b), "member-1", `{"action":"upgrade","name":"broker-analysis"}`)
	if !errors.Is(err, errPluginNotOperator) || len(b.upgrades) != 0 {
		t.Fatalf("Execute = %v, upgrades %v", err, b.upgrades)
	}
}

// On 2026-10-25 an install of the installed broker plugin put a card with nothing on it in
// front of the operator, and failed after the accept. It now says what is installed and how to
// move to the repository's version, with no card.
func TestPluginPackInstallOfAnInstalledPluginPointsToUpgrade(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.review.Commit, b.review.Version = pluginCommitB, "0.3.0"
	b.records["broker-analysis"] = plugins.Record{Name: "broker-analysis", Version: "0.1.1", Commit: pluginCommit, Phase: plugins.PhaseInstalled}
	tool := pluginTool(b)
	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"broker-analysis is already installed at 0.1.1 (" + pluginCommit + ")",
		"0.3.0 (" + pluginCommitB + ")",
		`plugin_pack action=upgrade name=broker-analysis`,
	} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("result lacks %q:\n%s", want, res.Preview)
		}
	}
	if strings.Contains(res.Preview, "plugin_approval_required") {
		t.Errorf("an installed plugin got a card:\n%s", res.Preview)
	}
}

func TestPluginPackShowSaysWhatIsInstalledHere(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.records["sales"] = plugins.Record{Name: "sales", Version: "1.2.0", Commit: pluginCommit, Phase: plugins.PhaseInstalled}
	tool := pluginTool(b)
	tool.Resolve = func(context.Context, packs.Ref) ([]packs.Pack, error) { return packFixture(), nil }
	res, err := callPack(t, tool, `{"action":"show","ref":"anthropics/knowledge-work-plugins/sales"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "installed here: 1.2.0 at " + pluginCommit + " — the repository has 1.3.0: plugin_pack action=upgrade name=sales"; !strings.Contains(res.Preview, want) {
		t.Errorf("result lacks %q:\n%s", want, res.Preview)
	}

	delete(b.records, "sales")
	res, err = callPack(t, tool, `{"action":"show","ref":"anthropics/knowledge-work-plugins/sales"}`)
	if err != nil || !strings.Contains(res.Preview, "installed here: no") {
		t.Errorf("Execute = (%q, %v), want it said the plugin is not installed", res.Preview, err)
	}
}
