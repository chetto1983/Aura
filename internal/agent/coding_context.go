// Project detection: which directory is a workspace, and how it verifies itself.
//
// A Go port of the detector NousResearch/hermes-agent `agent/coding_context.py` (MIT,
// commit 9d4ef04ed) exposes as `project_facts_for`. Only that path is ported: the file's
// other 700-odd lines build a workspace snapshot for the system prompt, which Aura's
// prompt builder already does its own way and which the ledger never reads.
//
// Cheap by design, in the original's words: stat calls plus reads of a couple of small
// files. It runs once per candidate directory at the end of a turn, so it must not walk
// a tree or shell out.
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// readSmall reads a small text file, or returns "". Never raises, never reads a huge
// file -- a 40 MB generated Makefile must not be pulled into memory at turn end.
func readSmall(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFactFileBytes {
		return ""
	}
	// #nosec G304 -- path is always filepath.Join(root, <constant marker name>): the
	// caller never passes model-supplied text, and the size check above already
	// bounds what a symlinked marker could drag in.
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// gitRoot returns the nearest ancestor containing .git, or "".
func gitRoot(cwd string) string {
	current := filepath.Clean(cwd)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// markerRoot returns the nearest ancestor that looks like a project root, or "".
//
// The temp directory is skipped for the reason the original gives: a stray manifest in
// a shared world-writable root, left by any process, must not flip every workspace under
// it into a project. The original skips $HOME for the same reason -- a Makefile in the
// home directory is user config, not a project signal -- and that check is kept.
func markerRoot(cwd string) string {
	tempRoot := filepath.Clean(os.TempDir())
	home, _ := os.UserHomeDir()
	home = filepath.Clean(home)

	current := filepath.Clean(cwd)
	for range markerRootMaxDepth {
		if current != tempRoot && (home == "." || current != home) {
			for _, marker := range projectMarkers {
				if isFile(filepath.Join(current, marker)) {
					return current
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
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
func detectVerifyCommands(root string) []string {
	verify := make([]string, 0, maxVerifyCommands)

	if isFile(filepath.Join(root, "scripts", "run_tests.sh")) {
		verify = append(verify, "scripts/run_tests.sh")
	}

	if isFile(filepath.Join(root, "package.json")) {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		// A malformed package.json yields no scripts rather than an error: the
		// detector is best-effort and a broken manifest is not this code's problem.
		_ = json.Unmarshal([]byte(readSmall(filepath.Join(root, "package.json"))), &manifest)
		manager := "npm"
		for _, candidate := range jsLockfiles {
			if isFile(filepath.Join(root, candidate.lockfile)) {
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

	if isFile(filepath.Join(root, "pytest.ini")) ||
		strings.Contains(readSmall(filepath.Join(root, "pyproject.toml")), "[tool.pytest") {
		verify = append(verify, "pytest")
	}

	if makefile := readSmall(filepath.Join(root, "Makefile")); makefile != "" {
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

// FilesystemProjectDetector answers ProjectFactsFor from the real filesystem. It is the
// half of VerificationLedger that does not touch Postgres, split out so the ledger's
// two questions can be answered by two things that have nothing to do with each other.
type FilesystemProjectDetector struct{}

// ProjectFactsFor returns the facts for cwd, with Found=false outside a workspace --
// the Go shape of the original returning None.
func (FilesystemProjectDetector) ProjectFactsFor(cwd string) ProjectFacts {
	if strings.TrimSpace(cwd) == "" {
		return ProjectFacts{}
	}
	root := gitRoot(cwd)
	if root == "" {
		root = markerRoot(cwd)
	}
	if root == "" {
		return ProjectFacts{}
	}
	return ProjectFacts{Found: true, Root: root, VerifyCommands: detectVerifyCommands(root)}
}
