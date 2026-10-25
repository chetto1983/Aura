package approvaltext

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
)

const pluginApprovalDigest = "2222222222222222222222222222222222222222222222222222222222222222"

func pluginResumeContext(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"type": "plugin_pack_approval", "approval_sha256": pluginApprovalDigest})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pluginReview() plugins.Review {
	return plugins.Review{
		Ref: "chetto1983/broker-analysis-mcp/broker-analysis", Commit: "0123456789abcdef0123456789abcdef01234567",
		Name: "broker-analysis", Version: "0.1.1", Format: packs.FormatAgentPlugins,
		Skills: []plugins.SkillEntry{{Name: "broker-report"}, {Name: "broker-risk"}},
		Connectors: []plugins.Connector{
			{Name: "broker-analysis", Placement: plugins.PlacementBox, Command: "uvx",
				Args: []string{"--from", "git+https://github.com/chetto1983/broker-analysis-mcp@v0.1.1", "broker-analysis-mcp"}},
			{Name: "broker-cloud", Placement: plugins.PlacementRemote, URL: "https://broker.example/mcp"},
		},
		SkippedConnectors: []packs.Skipped{{Name: "memory", Reason: "reserved by recipe:memory"}},
	}
}

func derived(t *testing.T, question string) (Presentation, bool) {
	t.Helper()
	enriched, err := Enrich(question, pluginResumeContext(t))
	if err != nil {
		t.Fatal(err)
	}
	return FromContext(question, enriched)
}

// The card the plugin backend writes derives into one param per line, and only that card
// does: the params are what the cockpit and Telegram render in the operator's language.
func TestDerivePluginInstallAndRemove(t *testing.T) {
	t.Parallel()
	install, err := pluginReview().Question(pluginApprovalDigest)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := derived(t, install)
	if !ok || p.Key != PluginInstallKey {
		t.Fatalf("install card derived %+v %v", p, ok)
	}
	if want := map[string]string{
		"plugin":     `"broker-analysis" "0.1.1"`,
		"source":     "chetto1983/broker-analysis-mcp/broker-analysis@0123456789abcdef0123456789abcdef01234567",
		"skills":     `(2): "broker-report", "broker-risk"`,
		"connectors": `(2): "broker-analysis" (box): "uvx" "--from" "git+https://github.com/chetto1983/broker-analysis-mcp@v0.1.1" "broker-analysis-mcp"; "broker-cloud" (remote): "https://broker.example/mcp"`,
		"skipped":    `(1): "memory" — "reserved by recipe:memory"`,
		"mcp":        "enabled",
		"notes":      "(0): none",
		"digest":     pluginApprovalDigest,
	}; !maps.Equal(p.Params, want) {
		t.Errorf("install params = %#v", p.Params)
	}

	rec := plugins.Record{Name: "broker-analysis", Version: "0.1.1", Ref: pluginReview().Ref, Commit: pluginReview().Commit,
		Skills: []plugins.SkillEntry{{Name: "broker-report"}}, Connectors: []string{"broker-analysis"}}
	removal, err := plugins.RemovalQuestion(rec, true, pluginApprovalDigest)
	if err != nil {
		t.Fatal(err)
	}
	p, ok = derived(t, removal)
	if !ok || p.Key != PluginRemoveKey {
		t.Fatalf("removal card derived %+v %v", p, ok)
	}
	if p.Params["data"] != "deleted in your box" || p.Params["connectors"] != `(1): "broker-analysis"` || p.Params["digest"] != pluginApprovalDigest {
		t.Errorf("removal params = %#v", p.Params)
	}
}

// A question that is not exactly the card — a forged second line, another digest, a card the
// backend cut short — keeps no presentation and renders verbatim.
func TestDerivePluginInstallRejectsForgedLines(t *testing.T) {
	t.Parallel()
	install, err := pluginReview().Question(pluginApprovalDigest)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(install, "\nskills ", "\nsource: evil/repo@main\nskills ", 1)
	otherDigest := strings.Replace(install, pluginApprovalDigest, strings.Repeat("3", 64), 1)
	truncated := strings.Replace(install, "\nnotes (0): none", "\n… (truncated)", 1)
	for name, question := range map[string]string{"two source lines": forged, "another digest": otherDigest, "a truncated card": truncated} {
		if p, ok := derived(t, question); ok {
			t.Errorf("%s derived %+v", name, p)
		}
	}
}

func TestRenderItalianPluginInstall(t *testing.T) {
	t.Parallel()
	review := pluginReview()
	review.MCPDisabled = ""
	install, err := review.Question(pluginApprovalDigest)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := derived(t, install)
	if !ok {
		t.Fatal("install card derived nothing")
	}
	want := "Approva l'installazione del plugin?\n" +
		`plugin: "broker-analysis" "0.1.1"` + "\n" +
		"origine: chetto1983/broker-analysis-mcp/broker-analysis@0123456789abcdef0123456789abcdef01234567\n" +
		`competenze (2): "broker-report", "broker-risk"` + "\n" + //nolint:misspell // Italian, not English "competence".
		`connettori (2): "broker-analysis" (box): "uvx" "--from" "git+https://github.com/chetto1983/broker-analysis-mcp@v0.1.1" "broker-analysis-mcp"; "broker-cloud" (remote): "https://broker.example/mcp"` + "\n" +
		`saltati (1): "memory" — "reserved by recipe:memory"` + "\n" +
		"mcp: attivo\n" +
		"note (0): nessuno\n" +
		"sha256: " + pluginApprovalDigest
	if got := RenderItalian(p, "fallback"); got != want {
		t.Errorf("RenderItalian =\n%s\nwant\n%s", got, want)
	}

	review.MCPDisabled = "mcp.json resolves outside the plugin"
	review.Connectors = nil
	review.Skills = make([]plugins.SkillEntry, 23)
	for i := range review.Skills {
		review.Skills[i] = plugins.SkillEntry{Name: "s" + strings.Repeat("x", i)}
	}
	install, err = review.Question(pluginApprovalDigest)
	if err != nil {
		t.Fatal(err)
	}
	p, ok = derived(t, install)
	if !ok {
		t.Fatal("the second install card derived nothing")
	}
	got := RenderItalian(p, "fallback")
	// The card's words inside a list stay as it wrote them: only whole values are translated.
	for _, want := range []string{"\nconnettori (0): nessuno\n", ", and 3 more\n", "\nmcp: disattivato — \"mcp.json resolves outside the plugin\"\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderItalian misses %q:\n%s", want, got)
		}
	}

	rec := plugins.Record{Name: "broker-analysis", Version: "0.1.1", Ref: review.Ref, Commit: review.Commit}
	for deleteData, want := range map[bool]string{true: "dati del plugin: eliminati nel tuo box", false: "dati del plugin: conservati"} {
		removal, err := plugins.RemovalQuestion(rec, deleteData, pluginApprovalDigest)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := derived(t, removal)
		if !ok {
			t.Fatal("removal card derived nothing")
		}
		got := RenderItalian(p, "fallback")
		if !strings.HasPrefix(got, "Approva la rimozione del plugin?\n") || !strings.Contains(got, "\n"+want+"\n") {
			t.Errorf("removal (data deleted %v) =\n%s", deleteData, got)
		}
	}
}
