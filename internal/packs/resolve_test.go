package packs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures below mirror the layout measured on
// anthropics/knowledge-work-plugins at main (2026-08-23): a monorepo whose
// plugin directories each carry `.claude-plugin/plugin.json`, most carrying
// `skills/` and `.mcp.json`, and only two of eighteen carrying `commands/`.
// The `sales` connector block is that repository's own, OAuth clientId shape
// included, because a hand-simplified one would not have exercised the oauth
// detection at all.

const salesMCP = `{
  "mcpServers": {
    "slack": {
      "type": "http",
      "url": "https://mcp.slack.com/mcp",
      "oauth": { "clientId": "1601185624273.8899143856786", "callbackPort": 3118 }
    },
    "hubspot": { "type": "http", "url": "https://mcp.hubspot.com/anthropic" },
    "local-notes": {
      "type": "stdio",
      "command": "notes-mcp",
      "args": ["--vault", "/data"],
      "env": { "NOTES_TOKEN": "x" }
    }
  }
}`

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// marketplace builds a repository shaped like the real one: two plugins, one
// with every plane populated and one with skills only.
func marketplace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write(t, filepath.Join(root, "sales", ".claude-plugin", "plugin.json"),
		`{"name":"sales","version":"1.3.1","description":"Research prospects","author":{"name":"Anthropic"}}`)
	write(t, filepath.Join(root, "sales", ".mcp.json"), salesMCP)
	write(t, filepath.Join(root, "sales", "skills", "call-prep", "SKILL.md"),
		"---\nname: call-prep\ndescription: Prepare for a sales call\n---\n\n# Call Prep\n")
	write(t, filepath.Join(root, "sales", "skills", "forecast", "SKILL.md"),
		"---\nname: forecast\ndescription: Forecast the pipeline\n---\n\nbody\n")
	// A directory under skills/ with no SKILL.md — shared assets sit here in the
	// real repositories and must not be mistaken for a skill.
	if err := os.MkdirAll(filepath.Join(root, "sales", "skills", "_shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "sales", "commands", "call-prep.md"),
		"---\ndescription: Prep me for a call\nargument-hint: \"[company]\"\n---\n\n# Call prep\n\nDo the thing.\n")
	write(t, filepath.Join(root, "sales", "commands", "NOTES.txt"), "not a command")

	write(t, filepath.Join(root, "legal", ".claude-plugin", "plugin.json"),
		`{"name":"legal","version":"0.2.0","description":"Review contracts","author":"Anthropic"}`)
	write(t, filepath.Join(root, "legal", "skills", "nda-triage", "SKILL.md"),
		"---\nname: nda-triage\ndescription: Triage an NDA\n---\n\nbody\n")

	// Noise that must not resolve as a pack.
	write(t, filepath.Join(root, "README.md"), "# marketplace")
	write(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "on: push")
	return root
}

func TestReadTreeResolvesEveryPackInAMarketplace(t *testing.T) {
	t.Parallel()
	root := marketplace(t)

	got, err := ReadTree(root, Ref{Source: "anthropics/knowledge-work-plugins"})
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("resolved %d packs, want 2 (sales, legal): %+v", len(got), got)
	}
	if got[0].Name != "legal" || got[1].Name != "sales" {
		t.Fatalf("packs are not in a stable order: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestReadTreeReadsEveryPlaneOfAPack(t *testing.T) {
	t.Parallel()
	root := marketplace(t)

	got, err := ReadTree(root, Ref{Source: "anthropics/knowledge-work-plugins", Directory: "sales"})
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a named directory resolved %d packs, want exactly 1", len(got))
	}
	p := got[0]

	if p.Name != "sales" || p.Version != "1.3.1" || p.Author != "Anthropic" {
		t.Errorf("manifest = %q/%q by %q, want sales/1.3.1 by Anthropic", p.Name, p.Version, p.Author)
	}
	if want := []string{"call-prep", "forecast"}; !equal(p.Skills, want) {
		t.Errorf("skills = %v, want %v (the _shared directory has no SKILL.md and is not a skill)", p.Skills, want)
	}
	if len(p.Servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(p.Servers))
	}
	if p.Servers[0].Name != "hubspot" || p.Servers[2].Name != "slack" {
		t.Errorf("servers are not sorted: %q … %q", p.Servers[0].Name, p.Servers[2].Name)
	}
	if len(p.Commands) != 1 || p.Commands[0].Name != "call-prep" {
		t.Fatalf("commands = %+v, want just call-prep (NOTES.txt is not a command)", p.Commands)
	}
	cmd := p.Commands[0]
	if cmd.Description != "Prep me for a call" || cmd.ArgumentHint != "[company]" {
		t.Errorf("command frontmatter = %q / %q", cmd.Description, cmd.ArgumentHint)
	}
	if !strings.HasPrefix(cmd.Body, "# Call prep") {
		t.Errorf("command body kept its frontmatter: %q", cmd.Body[:min(40, len(cmd.Body))])
	}
}

// The oauth flag is the one connector fact an operator needs BEFORE approving a
// pack: a browser round-trip cannot happen unattended.
func TestReadTreeReportsWhichConnectorsNeedOAuth(t *testing.T) {
	t.Parallel()
	got, err := ReadTree(marketplace(t), Ref{Source: "o/r", Directory: "sales"})
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	want := map[string]bool{"slack": true, "hubspot": false, "local-notes": false}
	for _, s := range got[0].Servers {
		if s.OAuth != want[s.Name] {
			t.Errorf("%s oauth = %v, want %v", s.Name, s.OAuth, want[s.Name])
		}
	}
}

func TestReadTreeCarriesStdioConnectorFieldsThrough(t *testing.T) {
	t.Parallel()
	got, _ := ReadTree(marketplace(t), Ref{Source: "o/r", Directory: "sales"})

	var notes Server
	for _, s := range got[0].Servers {
		if s.Name == "local-notes" {
			notes = s
		}
	}
	if notes.Command != "notes-mcp" || !equal(notes.Args, []string{"--vault", "/data"}) {
		t.Errorf("stdio connector lost its argv: %+v", notes)
	}
	if notes.Env["NOTES_TOKEN"] != "x" {
		t.Errorf("stdio connector lost its env: %+v", notes.Env)
	}
}

// A repository whose manifest sits at the root is a single-plugin repository,
// which is what a pack authored outside a marketplace looks like.
func TestReadTreeResolvesASinglePluginRepository(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"solo","version":"1.0.0"}`)
	write(t, filepath.Join(root, "skills", "only", "SKILL.md"), "---\nname: only\ndescription: d\n---\nb\n")

	got, err := ReadTree(root, Ref{Source: "me/solo"})
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(got) != 1 || got[0].Name != "solo" || got[0].Directory != "" {
		t.Fatalf("root manifest did not resolve as one pack: %+v", got)
	}
}

func TestReadTreeRefusesARepositoryWithNoManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "README.md"), "nothing here")

	_, err := ReadTree(root, Ref{Source: "me/empty"})
	if err == nil {
		t.Fatal("a repository with no plugin resolved without error; the caller asked to install something")
	}
	if !strings.Contains(err.Error(), "no plugin manifest") {
		t.Errorf("error does not say what was missing: %v", err)
	}
}

func TestReadTreeRefusesANamedDirectoryThatIsNotThere(t *testing.T) {
	t.Parallel()
	_, err := ReadTree(marketplace(t), Ref{Source: "o/r", Directory: "finance"})
	if err == nil {
		t.Fatal("a missing plugin directory resolved without error")
	}
}

// A manifest with no name cannot be addressed, installed, or shown. Falling back
// to the folder name would invent an identity the author never wrote.
func TestReadTreeRefusesAManifestWithNoName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "x", ".claude-plugin", "plugin.json"), `{"version":"1.0.0"}`)

	_, err := ReadTree(root, Ref{Source: "o/r", Directory: "x"})
	if err == nil || !strings.Contains(err.Error(), "no name") {
		t.Fatalf("err = %v, want a refusal naming the missing name", err)
	}
}

func TestReadTreeReportsMalformedThirdPartyFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		body string
	}{
		{"manifest", filepath.Join(".claude-plugin", "plugin.json"), `{"name": `},
		{"connectors", ".mcp.json", `{"mcpServers": [] }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write(t, filepath.Join(root, "p", ".claude-plugin", "plugin.json"), `{"name":"p"}`)
			write(t, filepath.Join(root, "p", tt.file), tt.body)

			if _, err := ReadTree(root, Ref{Source: "o/r", Directory: "p"}); err == nil {
				t.Fatalf("malformed %s resolved without error", tt.name)
			}
		})
	}
}

// The plane files are all optional: a pack that ships only skills is the common
// case (sixteen of eighteen in the measured marketplace ship no commands).
func TestReadTreeTreatsEveryPlaneAsOptional(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"bare"}`)

	got, err := ReadTree(root, Ref{Source: "me/bare"})
	if err != nil {
		t.Fatalf("a manifest-only pack failed to resolve: %v", err)
	}
	p := got[0]
	if p.Skills != nil || p.Servers != nil || p.Commands != nil {
		t.Errorf("absent planes came back non-nil: %+v", p)
	}
}

// A command file IS its instruction; the frontmatter is a convenience. Refusing
// one without a fence would make Aura stricter than the format's own authors.
func TestReadTreeKeepsACommandThatHasNoFrontmatter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"p"}`)
	write(t, filepath.Join(root, "commands", "bare.md"), "# Bare\n\nJust do it.\n")

	got, err := ReadTree(root, Ref{Source: "o/r"})
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(got[0].Commands) != 1 || got[0].Commands[0].Description != "" {
		t.Fatalf("commands = %+v", got[0].Commands)
	}
	if !strings.HasPrefix(got[0].Commands[0].Body, "# Bare") {
		t.Errorf("body = %q", got[0].Commands[0].Body)
	}
}

func TestResolveFetchesOnceThenReads(t *testing.T) {
	t.Parallel()
	fixture := marketplace(t)
	calls := 0
	r := &Resolver{Fetch: func(_ context.Context, source, dir string) error {
		calls++
		if source != "anthropics/knowledge-work-plugins" {
			t.Errorf("fetched %q", source)
		}
		return copyTree(fixture, dir)
	}}

	got, err := r.Resolve(t.Context(), Ref{Source: "anthropics/knowledge-work-plugins", Directory: "sales"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetched %d times, want 1", calls)
	}
	if got[0].Name != "sales" {
		t.Errorf("resolved %q", got[0].Name)
	}
}

func TestResolveSurfacesAFetchFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("network is down")
	r := &Resolver{Fetch: func(context.Context, string, string) error { return boom }}

	_, err := r.Resolve(t.Context(), Ref{Source: "o/r"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the fetch failure", err)
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Ref
		wantErr bool
	}{
		{in: "anthropics/knowledge-work-plugins", want: Ref{Source: "anthropics/knowledge-work-plugins"}},
		{in: "anthropics/knowledge-work-plugins/sales", want: Ref{Source: "anthropics/knowledge-work-plugins", Directory: "sales"}},
		{in: "  owner/repo  ", want: Ref{Source: "owner/repo"}},
		{in: "/owner/repo/", want: Ref{Source: "owner/repo"}},
		{in: "owner", wantErr: true},
		{in: "owner/repo/plugin/deeper", wantErr: true},
		{in: "owner//repo", wantErr: true},
		{in: "owner/repo/..", wantErr: true},
		{in: "owner/repo/.", wantErr: true},
		{in: `owner/repo/a:b`, wantErr: true},
		{in: `owner/repo/a\b`, wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRef(%q) = %+v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRefStringRoundTrips(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"owner/repo", "owner/repo/sales"} {
		got, err := ParseRef(in)
		if err != nil {
			t.Fatalf("ParseRef(%q): %v", in, err)
		}
		if got.String() != in {
			t.Errorf("%q round-tripped to %q", in, got.String())
		}
	}
}

// SkillRefs is what makes installing a pack's skills a loop over the installer
// that already exists rather than a second installer.
func TestSkillRefsRenderTheInstallerSyntax(t *testing.T) {
	t.Parallel()
	p := Pack{Source: "anthropics/knowledge-work-plugins", Skills: []string{"call-prep", "forecast"}}
	want := []string{
		"anthropics/knowledge-work-plugins@call-prep",
		"anthropics/knowledge-work-plugins@forecast",
	}
	if !equal(p.SkillRefs(), want) {
		t.Errorf("SkillRefs = %v, want %v", p.SkillRefs(), want)
	}
}

func TestPackRefReconstructsWhatResolvesToIt(t *testing.T) {
	t.Parallel()
	p := Pack{Source: "o/r", Directory: "sales"}
	if got := p.Ref().String(); got != "o/r/sales" {
		t.Errorf("Ref() = %q", got)
	}
}

// cloneURL is the boundary where a catalogue string becomes an argument to git.
func TestCloneURLAcceptsOnlyAnOwnerRepoPair(t *testing.T) {
	t.Parallel()
	if got, err := cloneURL("anthropics/knowledge-work-plugins"); err != nil ||
		got != "https://github.com/anthropics/knowledge-work-plugins.git" {
		t.Fatalf("cloneURL = %q, %v", got, err)
	}
	for _, bad := range []string{
		"https://evil.example/x/y",
		"git@github.com:owner/repo",
		"owner/repo/sales",
		"owner",
		"../../etc",
		"owner/repo?x=1/y",
	} {
		if got, err := cloneURL(bad); err == nil {
			t.Errorf("cloneURL(%q) = %q, want a refusal", bad, got)
		}
	}
}

// The clone flags are the substance of the fetcher: --depth 1 and
// --filter=blob:none are what keep a marketplace checkout small, and
// --recurse-submodules=no is what stops a second repository arriving under the
// trust the operator granted the first.
func TestCloneArgsPinTheFlagsThatMatter(t *testing.T) {
	t.Parallel()
	args, err := cloneArgs("anthropics/knowledge-work-plugins", "/tmp/dest")
	if err != nil {
		t.Fatalf("cloneArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"clone", "--depth 1", "--filter=blob:none", "--no-tags",
		"--recurse-submodules=no",
		"https://github.com/anthropics/knowledge-work-plugins.git /tmp/dest",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv is missing %q: %s", want, joined)
		}
	}
	if args[len(args)-1] != "/tmp/dest" {
		t.Errorf("destination is not the last argument: %v", args)
	}
}

func TestCloneArgsRefuseABadSourceOrAnEmptyDestination(t *testing.T) {
	t.Parallel()
	if _, err := cloneArgs("https://evil.example/x/y", "/tmp/d"); err == nil {
		t.Error("a source carrying its own host built an argv")
	}
	if _, err := cloneArgs("owner/repo", "  "); err == nil {
		t.Error("an empty destination built an argv; git would clone into the cwd")
	}
}

func TestGitFetcherRefusesABadSourceWithoutRunningGit(t *testing.T) {
	t.Parallel()
	// No network and no git process: the refusal happens while building argv.
	if err := GitFetcher(t.Context(), "https://evil.example/x/y", t.TempDir()); err == nil {
		t.Fatal("GitFetcher accepted a source that names its own host")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyTree materializes a fixture the way a fetcher would.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
}
