package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/scoring"
)

// DefaultInstallTimeoutSec bounds a git clone + copy install when the config
// supplies 0 (AURA_SKILL_INSTALL_TIMEOUT_SEC default, D-34).
const DefaultInstallTimeoutSec = 90

// lockfileName is the per-skill TOFU pin written at install with Aura's OWN
// CanonicalHash (D-13/D-15). It deliberately does NOT mirror the upstream
// skills-lock.json computedHash (locale/platform-sensitive); the value is Aura's
// canonical byte-sorted (relPath,bytes) sha256.
const lockfileName = "skills-lock.json"

// Installer clones a third-party skill repo natively (git, fixed-argv), strips
// symlinks at the copy boundary, strips always:true unconditionally (D-10), detects
// red flags for the install gate (D-13), validates at the write boundary, pins the
// canonical hash (TOFU), and lands the skill in pending/ + audit via the Writer. It
// NEVER activates — activation is the operator / ask_user resume path (11-05). It
// never runs npm/pip postinstall (D-14): the only transport is git clone, and
// bundled scripts only ever execute later via sandbox_exec.
type Installer struct {
	writer       *Writer
	gitBin       string // resolved git path (LookPath); "" → resolve lazily
	timeoutSec   int
	blocklist    []string
	bodyCapBytes int
}

// InstallerConfig configures an Installer. Writer is required (the pending+audit
// sink). GitBin is optional (LookPath resolves "git" when empty). TimeoutSec
// defaults to DefaultInstallTimeoutSec.
type InstallerConfig struct {
	Writer       *Writer
	GitBin       string
	TimeoutSec   int
	Blocklist    []string
	BodyCapBytes int
}

// NewInstaller builds an Installer from cfg.
func NewInstaller(cfg InstallerConfig) *Installer {
	to := cfg.TimeoutSec
	if to <= 0 {
		to = DefaultInstallTimeoutSec
	}
	return &Installer{
		writer:       cfg.Writer,
		gitBin:       cfg.GitBin,
		timeoutSec:   to,
		blocklist:    cfg.Blocklist,
		bodyCapBytes: cfg.BodyCapBytes,
	}
}

// InstallOpts modulates one Install call. AllowBlocklisted is the D-27 operator
// override the CLI passes ONLY after the gate has shown the operator which sequence
// matched; model-authored installs always pass false (hard reject, no escape).
type InstallOpts struct {
	AllowBlocklisted bool
	// Actor labels the install in the audit ledger ("model" / "cli").
	Actor AuditActor
	// SkillName selects the skill inside a multi-skill repo (the skills.sh catalog
	// `skillId`, e.g. "xlsx" inside anthropics/skills). Empty keeps the legacy
	// first-SKILL.md-found behavior — fine for single-skill repos, ambiguous for
	// catalogs (amendment #49: without the selector the D-35 catalog→install loop
	// staged an arbitrary skill from multi-skill repos).
	SkillName string
}

// RedFlag is one supply-chain red flag surfaced to the install gate (D-13). It is
// advisory — the install lands pending regardless; the gate shows the flags so a
// human approving the install sees the risk surface. Nothing is auto-run.
type RedFlag struct {
	Kind   string // "metadata_install", "tool_wildcard", "bundled_executable"
	Detail string
}

// InstallResult is the outcome of a successful Install: the skill landed in pending/
// with the canonical hash pinned, the always:true flag stripped if present, and the
// red flags collected for the gate. Status is StatusPendingApproval — install never
// self-activates (D-03/T-11-06-E2).
type InstallResult struct {
	Name           string
	Status         string
	ContentHash    string
	AlwaysStripped bool
	RedFlags       []RedFlag
	Tier           scoring.RiskTier
}

// ErrNoSkillFound is returned when a cloned repo contains no SKILL.md.
var ErrNoSkillFound = errors.New("no SKILL.md found in cloned repo")

// ErrInvalidRepoURL rejects a repo URL before it crosses into the git argv. A repo
// URL is the ONLY interpolated value (the rest of the argv is fixed) — it must look
// like an https/ssh/git URL or a scp-style host:path, and must not start with "-"
// (no option injection) or contain a NUL.
var ErrInvalidRepoURL = errors.New("invalid skill repo URL")

// Install clones repoURL natively, copies the skill (symlink-stripped), strips
// always:true, detects red flags, validates, pins the canonical hash, and lands the
// skill in pending/ + audit. The clone uses a FIXED argv (LookPath-resolved git,
// --depth 1 --single-branch -c core.autocrlf=false) with GIT_TERMINAL_PROMPT=0 so it
// never hangs on auth (T-11-06-T3); repoURL is validated before the call (the only
// interpolated value). It NEVER activates — that is the operator / resume path.
func (in *Installer) Install(ctx context.Context, repoURL string, opts InstallOpts) (InstallResult, error) {
	if in.writer == nil {
		return InstallResult{}, fmt.Errorf("installer: no writer configured")
	}
	repoURL = normalizeRepoShorthand(repoURL)
	if err := validateRepoURL(repoURL); err != nil {
		return InstallResult{}, err
	}

	gitBin, err := in.resolveGit()
	if err != nil {
		return InstallResult{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, in.installTimeout())
	defer cancel()

	scratch, err := os.MkdirTemp("", "aura-skill-clone-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("installer: scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	cloneDir := filepath.Join(scratch, "clone")

	if err := runClone(runCtx, gitBin, repoURL, cloneDir); err != nil {
		return InstallResult{}, err
	}

	skillSrc, err := locateSkillDir(cloneDir, opts.SkillName)
	if err != nil {
		return InstallResult{}, err
	}

	fm, body, err := readSkill(skillSrc)
	if err != nil {
		return InstallResult{}, err
	}

	// D-10: strip always:true UNCONDITIONALLY on third-party install. A stripped
	// always-on skill is re-enabled only via `aura skills always <name>`.
	alwaysStripped := fm.Always
	fm.Always = false

	redFlags := detectRedFlags(fm, skillSrc)

	// Validate at the write boundary (operator path may override the blocklist only
	// after the gate, D-27; model path passes false). The on-disk dir must match the
	// declared name too (the installer knows the cloned dir name).
	if err := ValidateNameAgainstDir(fm, filepath.Base(skillSrc)); err != nil {
		return InstallResult{}, fmt.Errorf("installer %q: %w", fm.Name, err)
	}
	if err := ValidateForWrite(fm, body, in.blocklist, in.bodyCapBytes, opts.AllowBlocklisted); err != nil {
		return InstallResult{}, fmt.Errorf("installer %q: %w", fm.Name, err)
	}

	// Copy the skill tree into a sibling staging dir, stripping symlinks (spike 005 /
	// T-11-06-T2), so the canonical hash and the pending write see a symlink-free tree.
	staged := filepath.Join(scratch, "staged", fm.Name)
	if err := stageSkillTree(skillSrc, staged, fm, body); err != nil {
		return InstallResult{}, err
	}

	hash, err := CanonicalHash(staged)
	if err != nil {
		return InstallResult{}, fmt.Errorf("installer %q: canonical hash: %w", fm.Name, err)
	}
	if err := writeLockfile(staged, hash); err != nil {
		return InstallResult{}, fmt.Errorf("installer %q: lockfile: %w", fm.Name, err)
	}

	tier := scoring.ComputeSkillTier(scoring.SkillInstall, body)

	// Land in pending/ + audit (gate_taken=false) via the Writer — the install is a
	// gated mutation; activation is the operator/resume path, NOT here (D-03).
	status, err := in.writer.WriteInstallPending(runCtx, fm, body, staged, hash, opts.Actor)
	if err != nil {
		return InstallResult{}, fmt.Errorf("installer %q: stage pending: %w", fm.Name, err)
	}

	return InstallResult{
		Name:           fm.Name,
		Status:         status,
		ContentHash:    hash,
		AlwaysStripped: alwaysStripped,
		RedFlags:       redFlags,
		Tier:           tier,
	}, nil
}

func (in *Installer) installTimeout() time.Duration {
	return time.Duration(in.timeoutSec) * time.Second
}

func (in *Installer) resolveGit() (string, error) {
	if in.gitBin != "" {
		return in.gitBin, nil
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("installer: git not found on PATH: %w", err)
	}
	return gitBin, nil
}

// runClone executes the FIXED-argv shallow clone. The argv is constant except for
// repoURL (validated by the caller) and the scratch dest; core.autocrlf=false keeps
// the working tree byte-identical across platforms (spike 004b — the autocrlf trap),
// and GIT_TERMINAL_PROMPT=0 prevents an auth-prompt hang (T-11-06-T3).
func runClone(ctx context.Context, gitBin, repoURL, dest string) error {
	// #nosec G204 -- gitBin is LookPath-resolved; the argv is FIXED (clone flags are
	// constants); repoURL is the ONLY interpolated value and is validated by
	// validateRepoURL (no leading "-", no NUL, URL/scp shape) before this call.
	cmd := exec.CommandContext(ctx, gitBin,
		"clone", "--depth", "1", "--single-branch", "-c", "core.autocrlf=false",
		"--", repoURL, dest)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("installer: git clone %q: %w: %s", repoURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateRepoURL rejects a repo URL that could inject a git option or break the
// argv. repoURL is the only model/operator-supplied value crossing into the clone;
// the "--" separator in runClone is the structural defense, this is the value guard.
func validateRepoURL(repoURL string) error {
	if strings.TrimSpace(repoURL) == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRepoURL)
	}
	if strings.ContainsRune(repoURL, 0) {
		return fmt.Errorf("%w: contains NUL", ErrInvalidRepoURL)
	}
	if strings.HasPrefix(repoURL, "-") {
		return fmt.Errorf("%w: must not start with '-' (option injection)", ErrInvalidRepoURL)
	}
	// Accept https/http/ssh/git scheme URLs and scp-style host:path. Reject anything
	// that is neither (a bare local path is not a recognized remote skill source).
	switch {
	case strings.HasPrefix(repoURL, "https://"),
		strings.HasPrefix(repoURL, "http://"),
		strings.HasPrefix(repoURL, "git://"),
		strings.HasPrefix(repoURL, "ssh://"),
		strings.HasPrefix(repoURL, "file://"):
		return nil
	case strings.Contains(repoURL, "@") && strings.Contains(repoURL, ":"):
		// scp-style: user@host:path
		return nil
	default:
		return fmt.Errorf("%w: %q is not an https/ssh/git/file URL or scp host:path", ErrInvalidRepoURL, repoURL)
	}
}

// locateSkillDir finds the skill directory inside a cloned repo: the repo root if it
// holds a SKILL.md, else the first immediate or second-level subdir that does. A repo
// with no SKILL.md is ErrNoSkillFound.
// normalizeRepoShorthand expands the skills.sh catalog `source` shorthand
// ("owner/repo", exactly two path segments, no scheme/host) into the canonical
// https://github.com/owner/repo URL (amendment #49: the catalog hands the model
// shorthands; rejecting them broke the install leg). Anything that is not a
// plain two-segment shorthand passes through untouched for validateRepoURL.
func normalizeRepoShorthand(repoURL string) string {
	s := strings.TrimSpace(repoURL)
	if s == "" || strings.ContainsAny(s, "@: \t") || strings.Contains(s, "://") {
		return repoURL
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return repoURL
	}
	for _, p := range parts {
		// A leading '-' or '.' is never a valid GitHub owner/repo — and expanding
		// it would launder validateRepoURL's option-injection guard ('-' prefix).
		if p[0] == '-' || p[0] == '.' {
			return repoURL
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return repoURL
			}
		}
	}
	return "https://github.com/" + s
}

// locateSkillDir finds the skill directory inside a cloned repo. With a non-empty
// name it returns ONLY the depth-1/depth-2 subdir whose base name matches and holds
// a SKILL.md (the multi-skill catalog case — anthropics/skills/<name>/SKILL.md);
// a named miss is a loud ErrNoSkillFound, never a silent fallback to the wrong
// skill. With an empty name it keeps the legacy first-found walk.
func locateSkillDir(cloneDir, name string) (string, error) {
	if name != "" {
		if fileExists(filepath.Join(cloneDir, "SKILL.md")) && filepath.Base(cloneDir) == name {
			return cloneDir, nil
		}
		for _, depth1 := range readSubdirs(cloneDir) {
			d1 := filepath.Join(cloneDir, depth1)
			if depth1 == name && fileExists(filepath.Join(d1, "SKILL.md")) {
				return d1, nil
			}
			for _, depth2 := range readSubdirs(d1) {
				d2 := filepath.Join(d1, depth2)
				if depth2 == name && fileExists(filepath.Join(d2, "SKILL.md")) {
					return d2, nil
				}
			}
		}
		return "", fmt.Errorf("%w: no skill named %q in repo", ErrNoSkillFound, name)
	}
	if fileExists(filepath.Join(cloneDir, "SKILL.md")) {
		return cloneDir, nil
	}
	// One or two levels down (anthropics/skills layout: skills/<name>/SKILL.md).
	for _, depth1 := range readSubdirs(cloneDir) {
		d1 := filepath.Join(cloneDir, depth1)
		if fileExists(filepath.Join(d1, "SKILL.md")) {
			return d1, nil
		}
		for _, depth2 := range readSubdirs(d1) {
			d2 := filepath.Join(d1, depth2)
			if fileExists(filepath.Join(d2, "SKILL.md")) {
				return d2, nil
			}
		}
	}
	return "", ErrNoSkillFound
}

// readSubdirs returns the names of immediate subdirectories of dir (skipping .git),
// best-effort (a read error yields no entries — locateSkillDir then fails cleanly).
func readSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != ".git" {
			out = append(out, e.Name())
		}
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// readSkill parses the SKILL.md frontmatter + body from a skill dir.
func readSkill(skillDir string) (Frontmatter, string, error) {
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md")) // #nosec G304 -- skillDir is under the installer's own scratch clone (operator/installer-controlled)
	if err != nil {
		return Frontmatter{}, "", fmt.Errorf("read SKILL.md: %w", err)
	}
	return parseFrontmatter(raw)
}

// detectRedFlags collects the D-13 supply-chain red flags for the install gate: a
// metadata.*.install[] block (a postinstall hook Aura never runs), a wildcard in
// allowed-tools (a skill claiming every tool), and the count of bundled executable
// files (scripts that only ever run via sandbox_exec). Nothing is auto-run; the gate
// surfaces these so a human approving the install sees the risk surface.
func detectRedFlags(fm Frontmatter, skillDir string) []RedFlag {
	var flags []RedFlag

	if hasInstallBlock(fm.Metadata) {
		flags = append(flags, RedFlag{
			Kind:   "metadata_install",
			Detail: "frontmatter metadata declares an install[] block (a postinstall hook — Aura never runs it; bundled scripts only run via sandbox_exec)",
		})
	}
	for _, tool := range fm.AllowedTools {
		if strings.Contains(tool, "*") {
			flags = append(flags, RedFlag{
				Kind:   "tool_wildcard",
				Detail: fmt.Sprintf("allowed-tools contains a wildcard %q (the skill claims broad tool access)", tool),
			})
			break
		}
	}
	if n := countBundledExecutables(skillDir); n > 0 {
		flags = append(flags, RedFlag{
			Kind:   "bundled_executable",
			Detail: fmt.Sprintf("%d bundled executable/script file(s) (run only via sandbox_exec, never auto-executed)", n),
		})
	}
	return flags
}

// hasInstallBlock reports whether the metadata map carries any install[] block at the
// top level or one level down (metadata.<key>.install[]). It is a structural scan —
// the presence of the key is the red flag, not its contents.
func hasInstallBlock(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if _, ok := metadata["install"]; ok {
		return true
	}
	for _, v := range metadata {
		if sub, ok := v.(map[string]any); ok {
			if _, ok := sub["install"]; ok {
				return true
			}
		}
	}
	return false
}

// countBundledExecutables counts files under skillDir whose extension marks them as
// executable scripts/binaries (.sh/.py/.js/.rb/.pl/.ps1) plus any file with a unix
// executable bit. Symlinks are skipped (Lstat-no-follow). This is advisory only.
func countBundledExecutables(skillDir string) int {
	count := 0
	_ = walkRegularFilesNoSymlinks(skillDir, func(path string, info os.FileInfo) {
		if isExecutableScript(path) || info.Mode().Perm()&0o111 != 0 {
			count++
		}
	})
	return count
}

// isExecutableScript reports whether path's extension marks it as a script/binary the
// gate should flag as bundled-executable.
func isExecutableScript(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".py", ".js", ".mjs", ".cjs", ".rb", ".pl", ".ps1", ".bat", ".cmd", ".exe":
		return true
	default:
		return false
	}
}

// walkRegularFilesNoSymlinks recurses skillDir (manual os.ReadDir + Lstat, never
// filepath.WalkDir, never descending a symlink) and calls fn for each regular file.
func walkRegularFilesNoSymlinks(dir string, fn func(path string, info os.FileInfo)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, lerr := os.Lstat(path)
		if lerr != nil {
			return lerr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		switch {
		case info.IsDir():
			if err := walkRegularFilesNoSymlinks(path, fn); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			fn(path, info)
		}
	}
	return nil
}

// stageSkillTree copies the cloned skill tree into staged (symlink-stripped via the
// established copyTreeNoSymlinks), then REWRITES the SKILL.md from the parsed
// frontmatter+body so the always:true strip (D-10) is persisted on disk before the
// hash + pending write. The rewrite uses the same skillFileBytes renderer the writer
// uses, so a create and an install of identical content hash identically.
func stageSkillTree(src, staged string, fm Frontmatter, body string) error {
	if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
		return fmt.Errorf("installer: mkdir staging parent: %w", err)
	}
	if err := os.RemoveAll(staged); err != nil {
		return fmt.Errorf("installer: clear staging: %w", err)
	}
	if err := os.MkdirAll(staged, 0o750); err != nil {
		return fmt.Errorf("installer: mkdir staging: %w", err)
	}
	if err := copyTreeNoSymlinks(src, staged); err != nil {
		return fmt.Errorf("installer: copy skill tree: %w", err)
	}
	// Rewrite SKILL.md with the always-stripped frontmatter (D-10).
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), skillFileBytes(fm, body), 0o600); err != nil {
		return fmt.Errorf("installer: rewrite SKILL.md: %w", err)
	}
	return nil
}

// writeLockfile writes the per-skill TOFU pin (skills-lock.json) with Aura's OWN
// CanonicalHash (D-13/D-15) — NOT the upstream computedHash. It is a minimal JSON
// object: {"computedHash": "<algo>:<hex>", "algo": "<algo>"}. The lockfile is written
// into the staged tree AFTER the hash is computed, so it is itself excluded from the
// pin (the hash hashed the tree without it).
func writeLockfile(staged, hash string) error {
	body := fmt.Sprintf("{\n  \"computedHash\": %q,\n  \"algo\": %q\n}\n", hash, canonicalHashAlgo)
	return os.WriteFile(filepath.Join(staged, lockfileName), []byte(body), 0o600)
}
