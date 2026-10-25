package plugins

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
)

const reviewCommit = "0123456789abcdef0123456789abcdef01234567"

func skillMD(name string) string {
	return "---\nname: " + name + "\ndescription: d\n---\nbody"
}

// claudePlugin writes a Claude-format plugin "p" under a checkout root and reads it.
func claudePlugin(t *testing.T, name, mcpJSON string, skillDirs map[string]string) (packs.Checkout, packs.Ref) {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "p")
	writeFile(t, filepath.Join(base, ".claude-plugin", "plugin.json"),
		`{"name":"`+name+`","version":"1.0.0","author":{"name":"A"}}`, 0o644)
	if mcpJSON != "" {
		writeFile(t, filepath.Join(base, ".mcp.json"), mcpJSON, 0o644)
	}
	for dir, md := range skillDirs {
		writeFile(t, filepath.Join(base, "skills", dir, "SKILL.md"), md, 0o644)
	}
	ref := packs.Ref{Source: "o/r", Directory: "p"}
	pk, err := packs.ReadTree(root, ref)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	return packs.Checkout{Root: root, Commit: reviewCommit, Packs: pk}, ref
}

func nothingTaken() Taken {
	return Taken{
		Skill:  func(string) bool { return false },
		Server: func(string) (string, bool) { return "", false },
	}
}

func review(t *testing.T, c packs.Checkout, ref packs.Ref, taken Taken) Review {
	t.Helper()
	r, err := BuildReview(c, ref, taken, nil, 32768)
	if err != nil {
		t.Fatalf("BuildReview: %v", err)
	}
	return r
}

func TestReviewPlacesStdioInTheBoxAndRemoteAsRemote(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "p", `{"mcpServers":{
		"local":{"command":"uvx","args":["x"]},
		"remote":{"type":"http","url":"https://r.example/mcp","oauth":{"clientId":"c"}}}}`, nil)
	r := review(t, c, ref, nothingTaken())
	if len(r.Connectors) != 2 {
		t.Fatalf("connectors = %+v", r.Connectors)
	}
	local, remote := r.Connectors[0], r.Connectors[1]
	if local.Name != "local" || local.Placement != PlacementBox || local.Trust != mcp.TrustSandboxedLocal || local.OAuth {
		t.Errorf("local = %+v", local)
	}
	if remote.Name != "remote" || remote.Placement != PlacementRemote || remote.Trust != mcp.TrustRemoteHTTP || !remote.OAuth ||
		remote.Transport != mcp.ServerTypeStreamableHTTP {
		t.Errorf("remote = %+v", remote)
	}
	if r.Ref != "o/r/p" || r.Commit != reviewCommit || r.Name != "p" || r.Version != "1.0.0" || r.Author != "A" ||
		r.Format != packs.FormatClaude || len(r.Digest) != 64 {
		t.Errorf("identity = %+v", r)
	}
}

func reasonOf(skipped []packs.Skipped, name string) string {
	for _, s := range skipped {
		if s.Name == name {
			return s.Reason
		}
	}
	return ""
}

func TestReviewSkipsTakenAndReservedNames(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "p", `{"mcpServers":{
		"memory":{"command":"uvx"},
		"mine":{"command":"uvx"},
		"empty":{"type":"http","url":""},
		"fresh":{"command":"uvx"}}}`,
		map[string]string{"taken": skillMD("taken"), "fresh": skillMD("fresh")})
	taken := Taken{
		Skill: func(name string) bool { return name == "taken" },
		Server: func(name string) (string, bool) {
			switch name {
			case "memory":
				return "recipe:memory", true
			case "mine":
				return "custom", true
			}
			return "", false
		},
	}
	r := review(t, c, ref, taken)
	for name, want := range map[string]string{
		"memory": "reserved by recipe:memory",
		"mine":   `name is taken by a server from "custom"`,
		"empty":  "no endpoint declared (placeholder)",
	} {
		if got := reasonOf(r.SkippedConnectors, name); !strings.Contains(got, want) {
			t.Errorf("connector %s skipped for %q, want %q", name, got, want)
		}
	}
	if got := reasonOf(r.SkippedSkills, "taken"); got != "a skill with this name already exists (active or archived)" {
		t.Errorf("skill taken skipped for %q", got)
	}
	if len(r.Connectors) != 1 || r.Connectors[0].Name != "fresh" || len(r.Skills) != 1 || r.Skills[0].Name != "fresh" {
		t.Errorf("kept connectors %+v, skills %+v", r.Connectors, r.Skills)
	}
}

func TestReviewSkipsAnInvalidSkill(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "p", "", map[string]string{"wrong": skillMD("other"), "good": skillMD("good")})
	r := review(t, c, ref, nothingTaken())
	if got := reasonOf(r.SkippedSkills, "wrong"); !strings.Contains(got, "invalid skill name") {
		t.Errorf("skill wrong skipped for %q, want ErrInvalidName's text", got)
	}
	if len(r.Skills) != 1 || r.Skills[0].Name != "good" || !strings.HasPrefix(r.Skills[0].Hash, "sha256:") {
		t.Errorf("skills = %+v", r.Skills)
	}
}

func TestReviewDigestIsStable(t *testing.T) {
	t.Parallel()
	const servers = `{"mcpServers":{"a":{"command":"uvx","args":["x"],"env":{"K":"1","J":"2"}},"b":{"command":"uvx"}}}`
	c, ref := claudePlugin(t, "p", servers, map[string]string{"s": skillMD("s")})
	first := review(t, c, ref, nothingTaken())
	for range 10 {
		again := review(t, c, ref, nothingTaken())
		if again.Digest != first.Digest {
			t.Fatalf("the digest moved between two reviews of one tree")
		}
	}
	reordered := first
	reordered.Connectors = []Connector{first.Connectors[1], first.Connectors[0]}
	if got, err := reordered.digest(); err != nil || got != first.Digest {
		t.Errorf("reordered connectors changed the digest: %v", err)
	}
	for _, by := range []string{"alice", "bob"} {
		approved := first
		approved.Connectors = slices.Clone(first.Connectors)
		approved.Connectors[0].server.Trust = mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal, ApprovedBy: by, Reason: "plugin install"}
		if got, err := approved.digest(); err != nil || got != first.Digest {
			t.Errorf("an approval by %s changed the digest: %v", by, err)
		}
	}
	changed := first
	changed.Connectors = slices.Clone(first.Connectors)
	changed.Connectors[0].server.Args = []string{"y"}
	if got, _ := changed.digest(); got == first.Digest {
		t.Error("a changed arg kept the digest")
	}
}

func TestReviewRefusesAnUnsafePluginName(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "Broker Analysis", "", nil)
	if _, err := BuildReview(c, ref, nothingTaken(), nil, 32768); err == nil {
		t.Fatal("a plugin named \"Broker Analysis\" was reviewed")
	}
}

func TestReviewNamesThePluginOfAMarketplaceAndAcceptsARootPlugin(t *testing.T) {
	t.Parallel()
	market := t.TempDir()
	for _, p := range []string{"a", "b"} {
		writeFile(t, filepath.Join(market, p, ".claude-plugin", "plugin.json"), `{"name":"`+p+`"}`, 0o644)
	}
	ref := packs.Ref{Source: "o/market"}
	pk, err := packs.ReadTree(market, ref)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	_, err = BuildReview(packs.Checkout{Root: market, Commit: reviewCommit, Packs: pk}, ref, nothingTaken(), nil, 32768)
	if err == nil || !strings.Contains(err.Error(), "holds 2 plugins (a, b)") || !strings.Contains(err.Error(), "o/market/<plugin>") {
		t.Fatalf("marketplace review = %v, want the plugins named", err)
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"solo"}`, 0o644)
	ref = packs.Ref{Source: "o/solo"}
	if pk, err = packs.ReadTree(root, ref); err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	r, err := BuildReview(packs.Checkout{Root: root, Commit: reviewCommit, Packs: pk}, ref, nothingTaken(), nil, 32768)
	if err != nil || r.Name != "solo" || r.Ref != "o/solo" {
		t.Fatalf("root plugin review = (%+v, %v)", r, err)
	}
	if _, err := BuildReview(packs.Checkout{Root: root}, ref, nothingTaken(), nil, 32768); err == nil {
		t.Error("a checkout with no plugin was reviewed")
	}
	if _, err := BuildReview(packs.Checkout{Root: root, Packs: pk}, ref, Taken{}, nil, 32768); err == nil {
		t.Error("a review with no name lookups was built")
	}
}

// The review refuses what Claim would refuse at install, so an approved plugin cannot fail on
// its own tree.
func TestReviewRefusesATreeTheStoreWouldRefuse(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "p", "", nil)
	outside := filepath.Join(t.TempDir(), "secret")
	writeFile(t, outside, "x", 0o600)
	symlinkOrSkip(t, outside, filepath.Join(c.Root, "p", "secret"))
	if _, err := BuildReview(c, ref, nothingTaken(), nil, 32768); err == nil || !strings.Contains(err.Error(), "leaves the plugin") {
		t.Fatalf("review = %v, want the escaping link refused", err)
	}
}

// The operator trusts what a connector runs, or where it connects, so the review shows the
// staged entry: the command as the box will start it, its args expanded, or the URL.
func TestReviewNamesWhatEachConnectorRuns(t *testing.T) {
	t.Parallel()
	c, ref := claudePlugin(t, "p", `{"mcpServers":{
		"local":{"command":"./bin/srv","args":["--data","${PLUGIN_DATA}"]},
		"remote":{"type":"http","url":"https://r.example/mcp"}}}`, nil)
	r := review(t, c, ref, nothingTaken())
	if len(r.Connectors) != 2 {
		t.Fatalf("connectors = %+v", r.Connectors)
	}
	local, remote := r.Connectors[0], r.Connectors[1]
	if local.Command != "/plugins/p/bin/srv" || !slices.Equal(local.Args, []string{"--data", DataDir("p", ref)}) || local.URL != "" {
		t.Errorf("local = %+v", local)
	}
	if remote.URL != "https://r.example/mcp" || remote.Command != "" || len(remote.Args) != 0 {
		t.Errorf("remote = %+v", remote)
	}
}
