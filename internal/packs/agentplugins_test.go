package packs

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// agentPlugin writes a one-plugin repository at its root: plugin.json plus any extra files,
// keyed by their plugin-relative path. The cases below are the fatal and non-fatal manifest
// rules of Agent Plugins 1.0 §5, quoted from spec/1.0.0.md (read 2026-10-25).
func agentPlugin(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "plugin.json"), manifest)
	for rel, body := range files {
		write(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return root
}

func readOne(t *testing.T, root string) (Pack, error) {
	t.Helper()
	found, err := ReadTree(root, Ref{Source: "o/r"})
	if err != nil {
		return Pack{}, err
	}
	if len(found) != 1 {
		t.Fatalf("want one pack, got %d", len(found))
	}
	return found[0], nil
}

const minimalAgentManifest = `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "minimal-plugin"}`

func TestAgentPluginsManifestRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifest  string
		files     map[string]string
		wantErr   string
		wantNotes []string
	}{
		{name: "minimal", manifest: minimalAgentManifest},
		{
			name:     "unsupported version",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.1.0/plugin.schema.json", "name": "p"}`,
			wantErr:  "unsupported Agent Plugins version 1.1.0",
		},
		{
			name:     "uppercase name",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "My-Plugin"}`,
			wantErr:  "name",
		},
		{
			name:     "leading hyphen",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "-a"}`,
			wantErr:  "name",
		},
		{
			name:     "double hyphen",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "a--b"}`,
			wantErr:  "name",
		},
		{
			name:     "missing name",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"}`,
			wantErr:  "name",
		},
		{
			name:      "unknown fields are reported and ignored, in order",
			manifest:  `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "commands": [], "agents": {}}`,
			wantNotes: []string{`unknown plugin.json field "agents" ignored`, `unknown plugin.json field "commands" ignored`},
		},
		{
			name:      "non-object extensions",
			manifest:  `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "extensions": "x"}`,
			wantNotes: []string{`plugin.json field "extensions" is not an object; ignored`},
		},
		{
			// §8.1: a namespace Aura does not implement is ignored without validating its value.
			name:     "unimplemented extension namespace is not validated",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "extensions": {"com.example.client": "not an object"}}`,
		},
		{
			name:     "author with an unknown field",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "author": {"name": "A", "twitter": "@a"}}`,
			wantErr:  "author",
		},
		{
			name:     "keywords not an array",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "keywords": "x"}`,
			wantErr:  "keywords",
		},
		{
			name:     "version of the wrong type",
			manifest: `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "p", "version": 1}`,
			wantErr:  "version",
		},
		{
			// v1 has no commands, and §11.3 forbids an unsupported component from sinking the plugin.
			name:     "a commands directory with broken frontmatter is ignored",
			manifest: minimalAgentManifest,
			files:    map[string]string{"commands/x.md": "---\n: [broken\n---\nbody"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack, err := readOne(t, agentPlugin(t, tc.manifest, tc.files))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one naming %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if pack.Format != FormatAgentPlugins {
				t.Errorf("format = %q, want %q", pack.Format, FormatAgentPlugins)
			}
			if len(pack.Commands) != 0 {
				t.Errorf("an Agent Plugins plugin read commands: %v", pack.Commands)
			}
			if !slices.Equal(pack.Notes, tc.wantNotes) {
				t.Errorf("notes = %q, want %q", pack.Notes, tc.wantNotes)
			}
		})
	}
}

func TestAgentPluginsManifestCarriesItsMetadata(t *testing.T) {
	t.Parallel()
	pack, err := readOne(t, agentPlugin(t, `{
		"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
		"name": "broker-analysis", "version": "0.1.1", "description": "Read-only analysis",
		"author": {"name": "Davide Marchetto"}, "license": "MIT", "keywords": ["finance"]
	}`, map[string]string{"skills/risk-analysis/SKILL.md": "---\nname: risk-analysis\ndescription: d\n---\nbody"}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pack.Name != "broker-analysis" || pack.Version != "0.1.1" || pack.Author != "Davide Marchetto" {
		t.Errorf("metadata = %+v", pack)
	}
	if !slices.Equal(pack.Skills, []string{"risk-analysis"}) {
		t.Errorf("skills = %v", pack.Skills)
	}
}

func TestAgentPluginsWinsOverTheClaudeManifest(t *testing.T) {
	t.Parallel()
	root := agentPlugin(t, minimalAgentManifest, map[string]string{
		".claude-plugin/plugin.json": `{"name": "claude-name"}`,
	})
	pack, err := readOne(t, root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pack.Format != FormatAgentPlugins || pack.Name != "minimal-plugin" {
		t.Errorf("read %q as %q; Agent Plugins must win (§5.1)", pack.Name, pack.Format)
	}
}

func TestARootPluginJSONWithoutSchemaLeavesTheClaudeFormat(t *testing.T) {
	t.Parallel()
	root := agentPlugin(t, `{"name": "whatever"}`, map[string]string{
		".claude-plugin/plugin.json": `{"name": "claude-name"}`,
	})
	pack, err := readOne(t, root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pack.Format != FormatClaude || pack.Name != "claude-name" {
		t.Errorf("read %q as %q, want the Claude manifest", pack.Name, pack.Format)
	}
}

func TestAgentPluginsPluginsAreFoundOneLevelDown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "broker-analysis", "plugin.json"),
		`{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "broker-analysis"}`)
	write(t, filepath.Join(root, "legacy", ".claude-plugin", "plugin.json"), `{"name": "legacy"}`)
	found, err := ReadTree(root, Ref{Source: "o/r"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var names []string
	for _, p := range found {
		names = append(names, p.Name+"="+p.Format)
	}
	if !slices.Equal(names, []string{"broker-analysis=" + FormatAgentPlugins, "legacy=" + FormatClaude}) {
		t.Errorf("found %v", names)
	}
}
