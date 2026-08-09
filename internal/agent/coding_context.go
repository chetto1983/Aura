// Project detection: which directory is a workspace, and how it verifies itself.
//
// A Go port of the detector NousResearch/hermes-agent `agent/coding_context.py` (MIT,
// commit 9d4ef04ed) exposes as `project_facts_for`. Only that path is ported: the file's
// other 700-odd lines build a workspace snapshot for the system prompt, which Aura's
// prompt builder already does its own way and which the ledger never reads.
//
// What is NOT the original's is the filesystem these rules read, and that difference is
// the whole reason this file has the shape it does. Hermes exports TERMINAL_CWD only from
// `tools/environments/local.py`, so its coding context is a local-filesystem feature and
// `project_facts_for` can stat directly. Aura's agent shell runs INSIDE the per-identity
// sandbox box (usersandbox/router.go: there is no host arm on any profile), and the box
// mounts a different volume at /workspace than the aura process does -- verified by
// docker inspect on 2026-08-09: `aura` mounts `aura_aura-workspace`, `aura-box-<identity>`
// mounts `aura-box-<identity>`. Statting the aura process's own filesystem therefore
// answers about paths the agent never names, and answered "not a project" every time.
//
// So the rules below never touch an OS. They read a projectSnapshot -- the answers to the
// only questions they ask -- and coding_context_box.go fetches that snapshot from the box
// in one exec. Cheap by design, in the original's words: stat calls plus reads of a couple
// of small files.
package agent

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
)

const (
	maxVerifyCommands = 8
	maxFactFileBytes  = 256 * 1024
	// markerRootMaxDepth bounds the walk up from cwd so a manifest in the workspace
	// root counts from a subdirectory without climbing to /.
	markerRootMaxDepth = 6
	// maxAncestorProbeDepth bounds the .git walk, which the original leaves unbounded
	// because it can stat lazily. The snapshot has to be fetched BEFORE the walk runs,
	// so the chain of directories to probe must be finite; 16 segments is far past any
	// real box path (the workspace is mounted at /workspace) and keeps the probe small.
	maxAncestorProbeDepth = 16
)

// projectMarkers are the files that make a directory a project root.
var projectMarkers = []string{
	"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt",
	"package.json", "tsconfig.json", "deno.json",
	"Cargo.toml", "go.mod", "pom.xml", "build.gradle", "build.gradle.kts",
	"Gemfile", "composer.json", "mix.exs", "pubspec.yaml",
	"CMakeLists.txt", "Makefile", "Dockerfile",
	"AGENTS.md", "CLAUDE.md", ".cursorrules",
}

// jsLockfiles map a lockfile to the package manager that wrote it, in priority order:
// the first one present decides how a package.json script is invoked.
var jsLockfiles = []struct{ lockfile, manager string }{
	{"pnpm-lock.yaml", "pnpm"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"},
	{"yarn.lock", "yarn"}, {"package-lock.json", "npm"},
}

// verifyTargets are the package.json scripts and Makefile targets worth surfacing.
var verifyTargets = []string{"test", "tests", "lint", "typecheck", "check", "build", "fmt", "format"}

// ProjectDetector answers "is this directory a project, and how does it verify itself".
// Both halves of the ledger consult one: the gate's read half (LedgerAdapter) and the
// hook's write half (VerificationHook). It is an interface because the answer comes from
// the per-identity box, which a unit test has no way to grow.
type ProjectDetector interface {
	ProjectFactsFor(cwd string) ProjectFacts
}

// projectSnapshot is one filesystem's answers to the only questions the rules below ask.
// Paths are POSIX and absolute, because the filesystem being described is always the box's.
type projectSnapshot struct {
	// files holds every probed path that is a REGULAR file. The value is the content for
	// the few files whose content a rule reads, and "" for the existence-only probes --
	// and for a content file over maxFactFileBytes, which readSmall refused to read for
	// the same reason: a 40 MB generated Makefile must not be pulled in at turn end.
	files map[string]string
	// gitDirs holds the directories carrying a .git entry. It is separate from files
	// because .git is the one probe that is not "is this a regular file": a normal clone
	// has a directory, a worktree or submodule has a gitfile.
	gitDirs map[string]bool
	// tempDir and homeDir are the box's, not this process's. Empty means unknown.
	tempDir string
	homeDir string
}

func (s projectSnapshot) isFile(p string) bool {
	_, ok := s.files[p]
	return ok
}

func (s projectSnapshot) content(p string) string { return s.files[p] }

// ancestors returns cwd and each of its parents, nearest first. It is the chain both the
// probe (which fetches answers for it) and the two root rules (which read them back) walk,
// so neither can disagree with the other about which directories were considered.
func ancestors(cwd string) []string {
	current := path.Clean(cwd)
	chain := make([]string, 0, maxAncestorProbeDepth)
	for range maxAncestorProbeDepth {
		chain = append(chain, current)
		parent := path.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return chain
}

// gitRoot returns the nearest ancestor containing .git, or "".
func gitRoot(snap projectSnapshot, chain []string) string {
	for _, dir := range chain {
		if snap.gitDirs[dir] {
			return dir
		}
	}
	return ""
}

// markerRoot returns the nearest ancestor that looks like a project root, or "".
//
// The temp directory is skipped for the reason the original gives: a stray manifest in
// a shared world-writable root, left by any process, must not flip every workspace under
// it into a project. The original skips $HOME for the same reason -- a Makefile in the
// home directory is user config, not a project signal -- and that check is kept.
func markerRoot(snap projectSnapshot, chain []string) string {
	if len(chain) > markerRootMaxDepth {
		chain = chain[:markerRootMaxDepth]
	}
	for _, dir := range chain {
		if dir == snap.tempDir || (snap.homeDir != "" && dir == snap.homeDir) {
			continue
		}
		for _, marker := range projectMarkers {
			if snap.isFile(path.Join(dir, marker)) {
				return dir
			}
		}
	}
	return ""
}

// makeTarget matches a Makefile rule at the start of a line, which is how the original
// decides `make test` is available.
func makeTarget(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*:`)
}

// detectVerifyCommands returns the canonical ways this project checks itself, in the
// original's order: a run_tests.sh wrapper first, then package.json scripts through the
// package manager its lockfile names, then pytest, then Makefile targets.
func detectVerifyCommands(snap projectSnapshot, root string) []string {
	verify := make([]string, 0, maxVerifyCommands)

	if snap.isFile(path.Join(root, "scripts/run_tests.sh")) {
		verify = append(verify, "scripts/run_tests.sh")
	}

	if snap.isFile(path.Join(root, "package.json")) {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		// A malformed package.json yields no scripts rather than an error: the
		// detector is best-effort and a broken manifest is not this code's problem.
		_ = json.Unmarshal([]byte(snap.content(path.Join(root, "package.json"))), &manifest)
		manager := "npm"
		for _, candidate := range jsLockfiles {
			if snap.isFile(path.Join(root, candidate.lockfile)) {
				manager = candidate.manager
				break
			}
		}
		for _, target := range verifyTargets {
			if _, ok := manifest.Scripts[target]; ok {
				verify = append(verify, manager+" run "+target)
			}
		}
	}

	if snap.isFile(path.Join(root, "pytest.ini")) ||
		strings.Contains(snap.content(path.Join(root, "pyproject.toml")), "[tool.pytest") {
		verify = append(verify, "pytest")
	}

	if makefile := snap.content(path.Join(root, "Makefile")); makefile != "" {
		for _, target := range verifyTargets {
			if makeTarget(target).MatchString(makefile) {
				verify = append(verify, "make "+target)
			}
		}
	}

	if len(verify) > maxVerifyCommands {
		verify = verify[:maxVerifyCommands]
	}
	return slices.Clip(verify)
}

// projectFactsFrom applies the rules to a snapshot, with Found=false outside a workspace
// -- the Go shape of the original returning None.
func projectFactsFrom(snap projectSnapshot, cwd string) ProjectFacts {
	if strings.TrimSpace(cwd) == "" {
		return ProjectFacts{}
	}
	chain := ancestors(cwd)
	root := gitRoot(snap, chain)
	if root == "" {
		root = markerRoot(snap, chain)
	}
	if root == "" {
		return ProjectFacts{}
	}
	return ProjectFacts{Found: true, Root: root, VerifyCommands: detectVerifyCommands(snap, root)}
}
