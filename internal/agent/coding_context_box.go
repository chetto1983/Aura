// Fetching the project snapshot from the per-identity sandbox box.
//
// coding_context.go holds the RULES; this holds the one place they get their facts. The
// split exists because the rules are Hermes's and portable, while where the files live is
// Aura's architecture and is not: the agent's shell runs in `aura-box-<identity>`, whose
// /workspace is a different volume from the aura process's (docker inspect, 2026-08-09),
// so every path the agent names has to be answered by the box or not at all.
//
// The probe is ONE `sh -c`. It runs at a voluntary termination -- the turn is already
// trying to end -- so a per-file exec (a docker exec round-trip costs tens of
// milliseconds, and the rules ask about ~28 paths per directory) would be seconds of
// latency for a question whose answer is usually "not a project".
package agent

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const (
	// projectProbeTimeout bounds the box exec. Short on purpose, for the reason
	// verificationReadTimeout is short: the probe runs on the TURN's own goroutine as it
	// tries to finish, so an unbounded one holds the turn open for as long as a wedged
	// dockerd takes to answer -- which is never. Losing the answer costs one nudge.
	projectProbeTimeout = 3 * time.Second

	// maxProjectProbeBytes bounds the whole probe output. The per-file cap already bounds
	// each content read; this bounds their sum, so a directory chain full of large
	// manifests cannot stream megabytes into a turn that is ending.
	maxProjectProbeBytes = 1 << 20

	// maxProjectCacheEntries bounds the per-process cache.
	maxProjectCacheEntries = 512
)

// projectContentFiles are the files whose CONTENT detectVerifyCommands reads. Every one of
// them is also a projectMarkers entry, so its existence is already probed.
var projectContentFiles = []string{"package.json", "pyproject.toml", "Makefile"}

// projectProbeFiles is every path, relative to a candidate directory, that the rules ask
// "is this a regular file" about. Derived from the same tables the rules read, so a marker
// or lockfile added there is probed here without a second edit -- the "two copies of what
// makes a project" failure this detector exists to avoid.
var projectProbeFiles = buildProjectProbeFiles()

func buildProjectProbeFiles() []string {
	probes := make([]string, 0, len(projectMarkers)+len(jsLockfiles)+2)
	probes = append(probes, projectMarkers...)
	for _, candidate := range jsLockfiles {
		probes = append(probes, candidate.lockfile)
	}
	return append(probes, "pytest.ini", "scripts/run_tests.sh")
}

// BoxProjectDetector answers ProjectFactsFor from the per-identity box. It is PROCESS-wide
// (the composition root builds one beside the evidence store) because it owns the cache;
// a turn binds it to its identity with ForIdentity.
type BoxProjectDetector struct {
	router *usersandbox.SandboxRouter

	mu    sync.Mutex
	cache map[string]ProjectFacts
}

// NewBoxProjectDetector builds the process-wide detector over the box router. A nil router
// yields a detector that finds nothing: SandboxRouter.Route is nil-receiver safe and denies,
// the probe fails open on that denial, and the policy then has nothing to say. It must NOT
// degrade to statting this process's filesystem -- that would answer about paths the box
// named using a filesystem it cannot see, which is the defect this type replaces.
func NewBoxProjectDetector(router *usersandbox.SandboxRouter) *BoxProjectDetector {
	return &BoxProjectDetector{router: router, cache: make(map[string]ProjectFacts)}
}

// ForIdentity binds the detector to one identity for one turn. The identity is not a
// parameter of ProjectFactsFor because that interface is Hermes's and single-tenant; here it
// decides WHICH box answers, so it is bound once per turn instead.
func (d *BoxProjectDetector) ForIdentity(identityID string) ProjectDetector {
	return boxIdentityDetector{shared: d, identityID: identityID}
}

type boxIdentityDetector struct {
	shared     *BoxProjectDetector
	identityID string
}

func (d boxIdentityDetector) ProjectFactsFor(cwd string) ProjectFacts {
	return d.shared.projectFactsFor(d.identityID, cwd)
}

// projectFactsFor answers from the cache, or probes the identity's box once.
func (d *BoxProjectDetector) projectFactsFor(identityID, cwd string) ProjectFacts {
	// No identity means no box to ask: Route would fall back to the seeded `local` box,
	// which is a different tenant's filesystem, so refusing is the only safe answer.
	if d == nil || identityID == "" || strings.TrimSpace(cwd) == "" {
		return ProjectFacts{}
	}
	chain := ancestors(cwd)
	key := identityID + "\x00" + chain[0]
	if facts, ok := d.cached(key); ok {
		return facts
	}
	snap, ok := d.probe(identityID, chain)
	if !ok {
		// Fail open, and do NOT cache: a dockerd hiccup must not pin "not a project"
		// onto a workspace for the life of the process.
		return ProjectFacts{}
	}
	facts := projectFactsFrom(snap, cwd)
	d.store(key, facts)
	return facts
}

// cached and store guard the per-(identity, cwd) memo.
//
// Caching for the process lifetime is safe because of WHAT is cached: the project's
// structure -- which manifests exist and what verify commands they declare -- which
// changes on the timescale of a repository, not of a turn. What is NOT cached is the only
// thing that changes per turn: whether the workspace has fresh passing evidence, which the
// gate reads from Postgres on every ask (LedgerAdapter.VerificationStatusFor). So a stale
// entry can at worst name a verify command list that has just changed, or miss a project
// created mid-session; it can never make an unverified workspace read as verified.
//
// The memo is keyed on cwd rather than on the resolved root because cwd is what callers
// pass, and one turn end asks about the same cwd at least twice (the gate's snapshot, then
// its status read). Two identities never share an entry: the key carries the identity, and
// a box is per identity.
func (d *BoxProjectDetector) cached(key string) (ProjectFacts, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	facts, ok := d.cache[key]
	return facts, ok
}

func (d *BoxProjectDetector) store(key string, facts ProjectFacts) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Bounded by starting over rather than by evicting one entry at a time: the map only
	// grows when the agent names a directory nobody has asked about before, so reaching
	// the cap at all means the working set changed, and rebuilding it costs one exec per
	// live cwd. An LRU here would be machinery for a case that does not arise.
	if len(d.cache) >= maxProjectCacheEntries {
		clear(d.cache)
	}
	d.cache[key] = facts
}

// probe runs the one exec and decodes it. ok=false means the box could not answer, which
// every caller must read as "say nothing" rather than as "not a project".
func (d *BoxProjectDetector) probe(identityID string, chain []string) (projectSnapshot, bool) {
	// Rooted in context.Background(), not in the turn's ctx, for the reason
	// verificationReadContext gives: the probe happens AT a voluntary termination, where a
	// wallclock trip may already have made the turn ctx Done. Detached is not unbounded,
	// and projectProbeTimeout is the whole difference between the two.
	ctx, cancel := context.WithTimeout(
		identityctx.WithIdentityID(context.Background(), identityID), projectProbeTimeout)
	defer cancel()

	handle, err := d.router.Route(ctx)
	if err != nil {
		slog.Debug("project detector: box unreachable", "error", err, "identity_id", identityID)
		return projectSnapshot{}, false
	}
	token := rand.Text()
	res, err := d.router.Exec(ctx, handle, usersandbox.ExecRequest{Command: projectProbeCommand(token, chain)})
	if err != nil {
		slog.Debug("project detector: probe exec failed", "error", err, "identity_id", identityID)
		return projectSnapshot{}, false
	}
	// The exit code is deliberately not consulted: the probe's last command is a `[ -f ]`
	// test for a file that is usually absent, so a nonzero status is the NORMAL outcome.
	// Whether the box answered is decided by whether the output parses.
	return parseProjectProbe(res.Stdout, "\x00"+token+"\x00", chain)
}

// projectProbeCommand renders the single `sh -c` the probe runs.
//
// Two sections, in one pass over the same directory list. The first emits one line per
// directory: a '0'/'1' per projectProbeFiles entry, then one for .git. The second emits
// $HOME, ${TMPDIR:-/tmp}, and each content file's bytes, every field delimited by
// NUL-token-NUL.
//
// Nothing echoes a PATH back. The directories are known here, the shell answers in the same
// order, and the decoder zips the two -- so a cwd containing a newline (these paths come
// from the model's own tool arguments) cannot desync the parse, and the delimiter never has
// to be chosen to avoid colliding with a filename.
//
// The token rides printf's %s ARGUMENT, never its format string. That sidesteps the octal
// hazard documented at tools.boxFrameEmitter, where a "\0" escape immediately before a
// base32 token absorbs the token's first character whenever it is a digit.
func projectProbeCommand(token string, chain []string) string {
	dirs := shellQuoteAll(chain)
	return fmt.Sprintf(
		"{ t=%s; "+
			"for d in %s; do for f in %s; do if [ -f \"$d/$f\" ]; then printf 1; else printf 0; fi; done; "+
			"if [ -e \"$d/.git\" ]; then printf 1; else printf 0; fi; printf '\\n'; done; "+
			"printf '\\000%%s\\000' \"$t\"; printf '%%s' \"${HOME:-}\"; "+
			"printf '\\000%%s\\000' \"$t\"; printf '%%s' \"${TMPDIR:-/tmp}\"; "+
			"for d in %s; do for f in %s; do printf '\\000%%s\\000' \"$t\"; "+
			"[ -f \"$d/$f\" ] && head -c %d -- \"$d/$f\"; done; done; } 2>/dev/null | head -c %d",
		tools.ShellQuoteArg(token),
		dirs, shellQuoteAll(projectProbeFiles),
		dirs, shellQuoteAll(projectContentFiles),
		maxFactFileBytes+1, maxProjectProbeBytes,
	)
}

func shellQuoteAll(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, tools.ShellQuoteArg(value))
	}
	return strings.Join(quoted, " ")
}

// parseProjectProbe decodes the probe output. ok=false means the output is not the probe's
// -- a truncated flag block, a shell that never ran -- and the caller says nothing.
func parseProjectProbe(stdout []byte, separator string, chain []string) (projectSnapshot, bool) {
	fields := strings.Split(string(stdout), separator)
	// fields[0] is the flag block, then $HOME, then $TMPDIR, then one field per
	// (directory, content file) pair in emission order.
	const fixedFields = 3
	if len(fields) < fixedFields {
		return projectSnapshot{}, false
	}
	lines := strings.Split(strings.TrimSuffix(fields[0], "\n"), "\n")
	if len(lines) != len(chain) {
		return projectSnapshot{}, false
	}

	snap := projectSnapshot{
		files:   make(map[string]string, len(chain)*2),
		gitDirs: make(map[string]bool, len(chain)),
	}
	if home := strings.TrimSpace(fields[1]); home != "" {
		snap.homeDir = path.Clean(home)
	}
	if temp := strings.TrimSpace(fields[2]); temp != "" {
		snap.tempDir = path.Clean(temp)
	}

	for i, dir := range chain {
		flags := lines[i]
		if len(flags) != len(projectProbeFiles)+1 {
			return projectSnapshot{}, false
		}
		for j, probe := range projectProbeFiles {
			if flags[j] == '1' {
				snap.files[path.Join(dir, probe)] = ""
			}
		}
		if flags[len(projectProbeFiles)] == '1' {
			snap.gitDirs[dir] = true
		}
	}

	next := fixedFields
	for _, dir := range chain {
		for _, name := range projectContentFiles {
			if next >= len(fields) {
				// The overall byte budget cut the stream short. The directories already
				// decoded stand; the rest simply have no content, which reads as an
				// empty file -- fail-quiet, exactly like readSmall's own size refusal.
				return snap, true
			}
			body := fields[next]
			next++
			file := path.Join(dir, name)
			// Content for a path the flags did not report as a regular file is not a
			// file's content; and over the cap it is what readSmall refused to read.
			if _, isFile := snap.files[file]; !isFile || len(body) > maxFactFileBytes {
				continue
			}
			snap.files[file] = body
		}
	}
	return snap, true
}
