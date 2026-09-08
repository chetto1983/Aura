// install_env_contract_test.go proves the shipped key set actually boots, instead of
// asserting it (Task 3, D-01/D-03). It parses `scripts/install.sh`'s fresh-`.env` heredoc
// from disk — never a second copy of the values, because a copy is exactly what drifts,
// as the installer's own trailing comment on that heredoc says — applies the parsed pairs
// plus shaped stand-ins for every secret `ensure_internal_env_secrets` generates, and loads
// through the same `config.LoadServe()` entry point `aura config validate` uses. This test
// is the reason commit 37211f83d's lesson does not have to be relearned: it fails in CI the
// moment the shipped default and the config contract disagree, instead of failing on an
// operator's machine as a boot loop.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// repoRootForTest (container_artifacts_test.go, same package) already locates the repo
// root from the test's own working directory — reused here rather than duplicated
// (CLAUDE.md REUSABLE CODE).

// parseInstallerHeredoc extracts the KEY=VALUE pairs scripts/install.sh's
// write_env_if_missing writes into a brand-new .env: the region between the
// `cat > .env <<EOF` line and its terminating `EOF`. A parse that finds no heredoc (or
// an empty one) returns an empty map — the caller decides whether that is fatal, so this
// helper itself never hides a drifted marker behind a silent pass.
func parseInstallerHeredoc(t *testing.T, installSh string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(installSh)
	if err != nil {
		t.Fatalf("read %s: %v", installSh, err)
	}
	pairs := map[string]string{}
	inHeredoc := false
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if !inHeredoc {
			if strings.Contains(trimmed, "cat > .env <<EOF") {
				inHeredoc = true
			}
			continue
		}
		if trimmed == "EOF" {
			break
		}
		stripped := strings.TrimSpace(trimmed)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		pairs[trimmed[:idx]] = trimmed[idx+1:]
	}
	return pairs
}

// generatedSecretRe matches an `ensure_generated_env_secret KEY BYTES [PREFIX]` call —
// scripts/install.sh's own idempotent-secret-generation helper: KEY gets
// `${PREFIX}$(openssl rand -hex BYTES)` when absent.
var generatedSecretRe = regexp.MustCompile(`^\s*ensure_generated_env_secret\s+(\S+)\s+(\d+)(?:\s+(\S+))?\s*$`)

type generatedSecret struct {
	Key    string
	Bytes  int
	Prefix string
}

// parseGeneratedSecrets scans the WHOLE installer (not just one function body) for every
// ensure_generated_env_secret call: ensure_internal_env_secrets calls some directly and
// ensure_objectstore_env_secrets (which it calls) defines the rest, so scanning the file
// reads the real call graph instead of assuming which function owns which name.
func parseGeneratedSecrets(t *testing.T, installSh string) []generatedSecret {
	t.Helper()
	data, err := os.ReadFile(installSh)
	if err != nil {
		t.Fatalf("read %s: %v", installSh, err)
	}
	var out []generatedSecret
	for line := range strings.SplitSeq(string(data), "\n") {
		m := generatedSecretRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parse ensure_generated_env_secret byte count %q in %q: %v", m[2], line, err)
		}
		out = append(out, generatedSecret{Key: m[1], Bytes: n, Prefix: m[3]})
	}
	return out
}

// TestInstallerFreshEnvValidatesUnderStrictProfile proves the EXACT key set a fresh
// `scripts/install.sh` run writes satisfies the strict-profile config contract
// (ValidateProfile(config.ProfileSingleUserHardened)) with zero Fatal violations. It reads
// scripts/install.sh from disk — it does not hardcode a second copy of the shipped values.
func TestInstallerFreshEnvValidatesUnderStrictProfile(t *testing.T) {
	installSh := filepath.Join(repoRootForTest(t), "scripts", "install.sh")

	pairs := parseInstallerHeredoc(t, installSh)
	if len(pairs) == 0 {
		t.Fatalf("parsed zero key/value pairs from %s's fresh-.env heredoc — the heredoc "+
			"marker (`cat > .env <<EOF` / `EOF`) drifted, or the parser did", installSh)
	}

	if got := pairs["AURA_PROFILE"]; got != string(config.ProfileSingleUserHardened) {
		t.Fatalf("heredoc AURA_PROFILE = %q, want %q — AURA_MUSR_ISOLATION requires a "+
			"strict profile (gateMultiUserRequiresStrictProfile is Fatal otherwise), so a "+
			"non-strict AURA_PROFILE here would boot-loop a fresh install that also ships "+
			"AURA_MUSR_ISOLATION=true", got, config.ProfileSingleUserHardened)
	}
	if got := pairs["AURA_MUSR_ISOLATION"]; got != "true" {
		t.Fatalf("heredoc AURA_MUSR_ISOLATION = %q, want \"true\" — a fresh install must "+
			"ship multi-identity provisioning on (D-01/ISO-01)", got)
	}
	if got := strings.TrimSpace(pairs["AURA_SANDBOX_IMAGE"]); got == "" {
		t.Fatal("heredoc AURA_SANDBOX_IMAGE is empty — a strict deployment's shell/file " +
			"tool surface routes through the per-identity sandbox and needs an image ref")
	}

	secrets := parseGeneratedSecrets(t, installSh)
	if len(secrets) == 0 {
		t.Fatalf("parsed zero ensure_generated_env_secret calls from %s — the parser or "+
			"the installer's secret-generation shape drifted", installSh)
	}

	// Apply every literal (non-bash-variable) heredoc pair first. A value like
	// `${pg_pw}` is an unresolved shell-variable REFERENCE in the source text (this test
	// never executes the script), not a real secret shape — those names are all also in
	// the generated-secrets list below, which overrides them with a properly shaped
	// stand-in matching what a REAL install actually writes.
	for key, value := range pairs {
		if strings.Contains(value, "${") {
			continue
		}
		t.Setenv(key, value)
	}
	for _, s := range secrets {
		t.Setenv(s.Key, s.Prefix+strings.Repeat("a", s.Bytes*2))
	}
	// AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT are not `ensure_generated_env_secret`
	// random secrets and not a static heredoc literal either: ensure_embed_provenance
	// (called from ensure_internal_env_secrets, same fresh-install path) derives them from
	// a LIVE HuggingFace HEAD request during a real install, so their value cannot be read
	// from static source text and this offline test must not make that network call.
	// gateDocumentRetrieval only checks their SHAPE under a strict profile (non-empty
	// revision, a lowercase-hex SHA-256 fingerprint) — a well-shaped stand-in proves the
	// gate is satisfiable by what ensure_embed_provenance actually writes.
	t.Setenv("AURA_EMBED_REVISION", strings.Repeat("a", 40))
	t.Setenv("AURA_EMBED_FINGERPRINT", strings.Repeat("ab", 32))

	cfg, err := config.LoadServe()
	if err != nil {
		t.Fatalf("config.LoadServe with the shipped fresh-install key set: %v", err)
	}
	var fatals []string
	for _, v := range cfg.ValidateProfile(config.ProfileSingleUserHardened) {
		if v.Sev == config.Fatal {
			fatals = append(fatals, fmt.Sprintf("%s: %s", v.Knob, v.Msg))
		}
	}
	if len(fatals) > 0 {
		t.Fatalf("the shipped fresh-install key set fails "+
			"ValidateProfile(single_user_hardened) with %d Fatal violation(s), so a fresh "+
			"install would boot-loop:\n  %s", len(fatals), strings.Join(fatals, "\n  "))
	}
}
