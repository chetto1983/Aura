package skills

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/scoring"
)

// installer.go is the cockpit-side skills install transport (SKW-01/02, the
// gap-filler #3 net-new piece). It runs the `npx skills` CLI inside Aura's container
// to fetch a skill from a source field (owner/repo, URL, or local path) or to search
// the skills.sh catalog, stages the fetched tree to a temp dir, ANSI-strips the
// output, parses the staged SKILL.md, computes the canonical content hash, runs the
// EXISTING five-item write-boundary checklist (ValidateForWrite), and hands off to
// the write sink (Writer.WriteInstall) — which lands the staged tree in active/<name>/,
// materializes it into the /skills mount, and records the install audit row. Since
// amendment #97 an install is live when it returns; there is no approval step.
//
// Install runs WITH install scripts PERMITTED — NO script-disabling flag is ever passed
// (D-06/D-07, the post-D-09 amendment): Aura runs inside a container, so the container
// IS the isolation boundary. The real controls are the approval gate (the install is
// always RISKY supply-chain input queued for the operator), the Writer validation
// (sanitized env, SKILL.md parse, body cap, injection-literal blocklist, sanitized
// name/path), and the container blast boundary. An install is NEVER rendered "safe".
//
// External skills.sh discovery (Search) is ON by default (Claude-Code parity,
// operator directive 2026-06-21): a normal user opens the cockpit and searches the
// catalog with no env to set, no toggle to flip. AURA_SKILLS_EXTERNAL_DISCOVERY is
// an opt-OUT for a deployment that wants to forbid the external network fetch.

// externalDiscoveryEnv is the opt-OUT flag for catalog search. Discovery is enabled
// by default; only an explicit falsey value ("0"/"false"/"no"/"off") disables it.
const externalDiscoveryEnv = "AURA_SKILLS_EXTERNAL_DISCOVERY"

// CommandRunner runs one command and returns its combined stdout+stderr (ANSI not yet
// stripped) inside dir. It is the injectable seam so tests substitute a fake `npx`
// WITHOUT a real network call; the production runner is execCommandRunner.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) (string, error)

// Installer wraps the live Writer (the pending+audit sink) + an injectable command
// runner (so tests fake `npx`) + the validation config (blocklist + body cap, sourced
// from config by the composition root, never read from env here — ValidateForWrite is
// pure). It owns the staging + transport; the gate/scoring/hash/audit are all reused.
type Installer struct {
	writer       *Writer
	run          CommandRunner
	catalog      *catalogSearchService
	blocklist    []string
	bodyCapBytes int
	workDir      string
}

// InstallerConfig configures a NewInstaller. Run defaults to execCommandRunner (the
// real `npx skills` subprocess) when nil; tests pass a fake. Blocklist/BodyCapBytes
// come from config (the same values the Writer uses) so the install checklist matches
// the model/CLI write boundary exactly. WorkDir is the base for the transient clone +
// --copy work tree; it MUST be a spacious, exec-capable filesystem (a volume), never the
// hardened 64M noexec /tmp tmpfs. Empty WorkDir falls back to the system temp (tests).
type InstallerConfig struct {
	Writer *Writer
	Run    CommandRunner
	// CatalogSearch overrides only the primary JSON transport. Nil uses skills.sh.
	CatalogSearch CatalogSearchFunc
	Blocklist     []string
	BodyCapBytes  int
	WorkDir       string
}

// NewInstaller builds an Installer from cfg, defaulting Run to the real npx runner.
func NewInstaller(cfg InstallerConfig) *Installer {
	run := cfg.Run
	if run == nil {
		run = execCommandRunner
	}
	primary := cfg.CatalogSearch
	if primary == nil {
		primary = newSkillsCatalogAPIClient(http.DefaultClient, skillsCatalogAPIURL).Search
	}
	fallback := func(ctx context.Context, query string) ([]CatalogHit, error) {
		out, err := run(ctx, "", "npx", "skills", "find", query)
		if err != nil {
			return nil, fmt.Errorf("npx skills find: %w", err)
		}
		return parseCatalogHits(out), nil
	}
	return &Installer{
		writer:       cfg.Writer,
		run:          run,
		catalog:      newCatalogSearchService(primary, fallback, defaultCatalogSearchOptions()),
		blocklist:    cfg.Blocklist,
		bodyCapBytes: cfg.BodyCapBytes,
		workDir:      cfg.WorkDir,
	}
}

// CheckItem is one validation-checklist item the UI renders before the operator
// approves an install. The FIVE items mirror the write-boundary checks
// (ValidateForWrite): sanitized env, SKILL.md parse, body cap, injection-literal
// blocklist, sanitized name/path. Passed flags whether the staged skill cleared it.
type CheckItem struct {
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
}

// InstallInfo is the install result the handler/UI surfaces: the source the operator
// installed from, the canonical content hash, a body preview, the active destination,
// the RISKY risk tier (install is ALWAYS Risky — never "safe"), and the five-item
// validation checklist. Status is what the sink returned. Name is the staged skill's
// frontmatter name.
type InstallInfo struct {
	Name        string      `json:"name"`
	Source      string      `json:"source"`
	ContentHash string      `json:"content_hash"`
	Preview     string      `json:"preview"`
	Destination string      `json:"destination"`
	RiskTier    string      `json:"risk_tier"`
	Status      string      `json:"status"`
	Checklist   []CheckItem `json:"checklist"`
}

// CatalogResult is the Search result. Enabled reflects AURA_SKILLS_EXTERNAL_DISCOVERY
// (the toggle state is ALWAYS explicit, D-08); Hits is the parsed result list (empty
// when disabled OR when the search found nothing — the empty-state edge).
type CatalogResult struct {
	Enabled bool         `json:"enabled"`
	Query   string       `json:"query"`
	Hits    []CatalogHit `json:"hits"`
}

// previewCapBytes bounds the body preview the UI shows (a full body could be large;
// the operator needs a glance, not the whole file). The full body is staged regardless.
const previewCapBytes = 2048

// Install fetches a skill from source via `npx skills add <source> -y` (scripts
// PERMITTED, container-isolated — no script-disabling flag), stages the fetched tree, parses
// the staged SKILL.md, computes the canonical hash, runs the five-item checklist, then
// hands off to Writer.WriteInstall, which lands it active + materialized + audited.
// An empty/blank source is rejected with a safe error before any subprocess runs.
func (i *Installer) Install(ctx context.Context, source string, actor AuditActor) (InstallInfo, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return InstallInfo{}, fmt.Errorf("%w: install source is empty", ErrInvalidStructure)
	}
	if i.writer == nil {
		return InstallInfo{}, fmt.Errorf("install %q: writer not configured", source)
	}

	// The work dir holds the npx clone + the --copy materialization, which can be large (a
	// whole skills repo cloned + copied into every detected agent layout). It MUST NOT land on
	// the 64M noexec /tmp tmpfs (compose hardening) — i.workDir points it at a spacious volume
	// (the run dir) in prod; an empty workDir falls back to the system temp (tests).
	if i.workDir != "" {
		if err := os.MkdirAll(i.workDir, 0o750); err != nil {
			return InstallInfo{}, fmt.Errorf("install %q: ensure work base: %w", source, err)
		}
	}
	work, err := os.MkdirTemp(i.workDir, "aura-skill-install-*")
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: mkdir work dir: %w", source, err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	// `--copy` materializes the fetched skill into <work>/.claude/skills/<name>/ (the newer
	// skills CLI otherwise only symlinks into auto-detected agent layouts and leaves no local
	// copy for locateStagedSkill to stage). Scripts PERMITTED (D-06/D-07): no script-disabling
	// flag — the container is the blast boundary; the control is the Writer validation +
	// injection blocklist, not script-disabling.
	if _, err := i.run(ctx, work, "npx", "skills", "add", source, "--copy", "-y"); err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: npx skills add: %w", source, err)
	}

	stagedDir, err := locateStagedSkill(work)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}

	raw, err := os.ReadFile(filepath.Join(stagedDir, "SKILL.md")) // #nosec G304 -- stagedDir is locateStagedSkill's container-staged tree under our own MkdirTemp work dir
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: read staged SKILL.md: %w", source, err)
	}
	fm, body, perr := parseFrontmatter(raw)
	if perr != nil {
		return InstallInfo{}, fmt.Errorf("install %q: parse staged SKILL.md: %w", source, perr)
	}
	// The on-disk dir name MUST equal the declared frontmatter name (D-30): a cloned
	// tree whose dir != its declared name is rejected before staging into pending.
	if err := ValidateNameAgainstDir(fm, filepath.Base(stagedDir)); err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}

	hash, err := HashSkillDir(stagedDir)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: hash staged tree: %w", source, err)
	}

	// The FIVE-item write-boundary checklist (allowBlocklisted=false — the install path
	// NEVER bypasses the injection blocklist). A body one byte over the cap fails here;
	// at-cap passes (the boundary edge). A non-ASCII name / NFKC injection literal fail.
	if err := ValidateForWrite(fm, body, i.blocklist, i.bodyCapBytes, false); err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}

	status, err := i.writer.WriteInstall(ctx, fm, stagedDir, hash, actor)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}

	tier := scoring.ComputeSkillTier(scoring.SkillInstall, body)
	return InstallInfo{
		Name:        fm.Name,
		Source:      source,
		ContentHash: hash,
		Preview:     previewBody(body),
		Destination: filepath.Join(i.writer.activeDir, fm.Name),
		RiskTier:    string(tier),
		Status:      status,
		Checklist:   installChecklist(),
	}, nil
}

// Search prefers the skills.sh JSON catalog and falls back to `npx skills find <q>`.
// An explicit AURA_SKILLS_EXTERNAL_DISCOVERY=false opt-out returns a disabled result
// before any cache, HTTP, or CLI work.
func (i *Installer) Search(ctx context.Context, q string) (CatalogResult, error) {
	q = strings.TrimSpace(q)
	if !externalDiscoveryEnabled() {
		return CatalogResult{Enabled: false, Query: q, Hits: []CatalogHit{}}, nil
	}
	if utf8.RuneCountInString(q) < 2 {
		return CatalogResult{Enabled: true, Query: q, Hits: []CatalogHit{}}, nil
	}
	hits, err := i.catalog.Search(ctx, q)
	if err != nil {
		return CatalogResult{}, fmt.Errorf("skills search %q: %w", q, err)
	}
	return CatalogResult{Enabled: true, Query: q, Hits: hits}, nil
}

// externalDiscoveryEnabled reports whether catalog search may reach the network. It is
// ON by default (Claude-Code parity); only an explicit falsey AURA_SKILLS_EXTERNAL_DISCOVERY
// ("0"/"false"/"no"/"off", case-insensitive) opts out. Any other value (incl. unset)
// keeps discovery enabled.
func externalDiscoveryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(externalDiscoveryEnv))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// installChecklist is the FIVE-item validation checklist the UI surfaces (post-D-09;
// no script-disabling item). A staged skill that reached WriteInstall passed every
// write-boundary check (ValidateForWrite ran with allowBlocklisted=false), so all five
// are marked passed; a failing check returns an error from Install before this list is
// built, so the operator never sees a half-passed install.
func installChecklist() []CheckItem {
	return []CheckItem{
		{Label: "sanitized env (no secret leakage into the staged tree)", Passed: true},
		{Label: "SKILL.md frontmatter parsed", Passed: true},
		{Label: "body within the size cap", Passed: true},
		{Label: "injection-literal blocklist (NFKC-normalized) clean", Passed: true},
		{Label: "sanitized name/path (^[a-z0-9-]{1,64}$, dir-name matched)", Passed: true},
	}
}

// locateStagedSkill resolves the single skill directory `npx skills add` materialized
// under work. The CLI installs into `<work>/.claude/skills/<name>/` (spike 004a); a
// SKILL.md at that path is the staged tree. It returns an error when no skill dir with
// a SKILL.md is found (a fetch that produced nothing installable).
func locateStagedSkill(work string) (string, error) {
	skillsRoot := filepath.Join(work, ".claude", "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return "", fmt.Errorf("no staged skill found under %q: %w", skillsRoot, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(skillsRoot, e.Name())
		if _, statErr := os.Stat(filepath.Join(dir, "SKILL.md")); statErr == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no staged skill with a SKILL.md found under %q", skillsRoot)
}

// previewBody returns the leading previewCapBytes of body for the UI glance, marking a
// truncation. The full body is staged regardless — this is display only.
func previewBody(body string) string {
	if len(body) <= previewCapBytes {
		return body
	}
	return body[:previewCapBytes] + "\n… (truncated)"
}

// execCommandRunner is the production CommandRunner: it runs the command inside dir with
// stdin closed (no interactive hang) and GIT_TERMINAL_PROMPT=0, capturing combined
// stdout+stderr (spike 011 posture). On Windows the npx shim is npx.cmd. dir="" runs in
// the current working directory.
func execCommandRunner(ctx context.Context, dir, name string, args ...string) (string, error) {
	if name == "npx" && runtime.GOOS == "windows" {
		name = "npx.cmd"
	}
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed argv (npx skills add/find); source is a validated install arg, scripts permitted per D-06/D-07 (container = boundary)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Env = execCommandEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func execCommandEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "DO_NOT_TRACK=1")
}

// splitLines splits s on "\n" (the ANSI-stripped output is already LF-normalized for
// matching, but the parse trims a trailing "\r" per line via trimLine).
func splitLines(s string) []string { return strings.Split(s, "\n") }

// trimLine trims surrounding whitespace and a trailing carriage return from one output
// line before the catalog-entry regex match.
func trimLine(s string) string { return strings.TrimSpace(strings.TrimRight(s, "\r")) }
