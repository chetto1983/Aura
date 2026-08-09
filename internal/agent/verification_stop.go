// Turn-end verification guard for coding edits.
//
// A Go port of NousResearch/hermes-agent `agent/verification_stop.py` (MIT),
// read at commit 9d4ef04ed. Structure, names and comments follow the original
// so the two can be diffed; what changed is noted at each point where Aura's
// facts differ from Hermes's.
//
// This file is intentionally policy-only. It never runs checks itself; it turns
// a passive verification ledger into a bounded follow-up when the model tries to
// finish immediately after editing code without fresh evidence.
//
// It plugs into the seam llm_agent.go already has for exactly this shape: at the
// no-tool-calls terminal, gateCompletion may veto, append a user-role nudge and
// continue the loop. That gate fires only on a turn that mutated host state and
// spends an LLM critic call to decide; this one is deterministic, costs no model
// call, and reads evidence the turn already produced.
//
// The ledger the snapshot reads (hermes `agent/verification_evidence.py`, 649
// lines of SQLite) and the project detector it consults (`agent/coding_context.py`,
// 916 lines) are NOT ported here. They are expressed as the interface below so
// this policy can be tested and reviewed on its own, and so a later port has a
// contract to satisfy instead of a shape to guess.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxChangedPathsInNudge = 8

// nonCodeVerifyExtensions are non-code file extensions whose edits carry no
// verifiable runtime behavior: documentation, prose, and data/markup that no
// test/build exercises. When a turn touches ONLY these, verify-on-stop has
// nothing to check, so the nudge is suppressed -- a SKILL.md or README edit must
// never demand a verification script. A turn that edits any non-listed path (a
// real source/code/config file) still nudges.
var nonCodeVerifyExtensions = map[string]struct{}{
	".md": {}, ".markdown": {}, ".mdx": {}, ".rst": {}, ".txt": {}, ".text": {},
	".adoc": {}, ".asciidoc": {}, ".org": {}, ".log": {}, ".csv": {}, ".tsv": {},
}

// nonCodeVerifyFilenames are filenames (case-insensitive, extension-less or
// otherwise) that are pure prose even without a recognized doc extension.
var nonCodeVerifyFilenames = map[string]struct{}{
	"license": {}, "licence": {}, "notice": {}, "authors": {},
	"contributors": {}, "changelog": {}, "codeowners": {},
}

// VerificationStatus is the ledger's answer for one session+workspace. Status is
// the state string ("passed", "unverified", "not_applicable", ...); Evidence is
// nil when the ledger has none.
type VerificationStatus struct {
	Status   string
	Evidence *VerificationEvidence
}

// VerificationEvidence is the last observed verification run. CanonicalCommand is
// preferred over Command when both are present, mirroring the original's
// `evidence.get("canonical_command") or evidence.get("command")`.
type VerificationEvidence struct {
	CanonicalCommand string
	Command          string
	OutputSummary    string
}

// ProjectFacts is the detector's answer for one directory. Found=false means the
// directory is not a project root the detector recognises, which the original
// expresses as a falsy `facts`.
type ProjectFacts struct {
	Found bool
	// Root is the project directory the detector settled on. The ledger keys its
	// state on ROOT, not on cwd, so a suite run from a subdirectory and one run from
	// the top are the same workspace.
	Root           string
	VerifyCommands []string
}

// VerificationLedger is the pair of Hermes modules this policy consults. Both are
// unported; this is the contract they have to satisfy, not a reimplementation of
// them. A nil ledger disables the nudge, which is the Go equivalent of the
// original's ImportError guard returning None.
type VerificationLedger interface {
	ProjectFactsFor(cwd string) ProjectFacts
	VerificationStatusFor(sessionID, cwd string) VerificationStatus
}

// isNonCodePath reports whether a changed path is documentation/prose with
// nothing to verify.
func isNonCodePath(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	suffix := strings.ToLower(filepath.Ext(raw))
	if _, ok := nonCodeVerifyExtensions[suffix]; ok {
		return true
	}
	if suffix == "" {
		if _, ok := nonCodeVerifyFilenames[strings.ToLower(filepath.Base(raw))]; ok {
			return true
		}
	}
	return false
}

// filterVerifiablePaths drops documentation/prose paths and keeps paths that
// could have verifiable behavior.
func filterVerifiablePaths(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" && !isNonCodePath(p) {
			kept = append(kept, p)
		}
	}
	return kept
}

// sessionIsMessagingSurface reports whether this turn is delivered over a human
// messaging channel.
//
// Verify-on-stop defaults ON for the interactive surfaces and programmatic
// callers, and OFF on a conversational platform (Telegram, WhatsApp, ...) where
// the verification narrative reaches a human as chat noise.
//
// Aura's agent package does not carry the delivery surface: InvocationContext has
// no channel field and the runner does not thread one in. So this reports a local
// surface, which keeps verify-on-stop enabled -- the SAME fallback the original
// takes when its gateway package is unreachable ("there is no messaging channel
// to be on"). Wiring the real surface is what makes the Telegram default correct;
// until then this is honest about defaulting on rather than pretending to know.
func sessionIsMessagingSurface() bool { return false }

// verifyOnStopEnabled reports whether edit -> verify-before-finish is enabled.
//
// Precedence: an explicit AURA_AGENT_VERIFY_ON_STOP_ENABLED env var wins, then an
// explicit configured value. The default is surface-aware "auto": ON for
// interactive surfaces and programmatic callers, OFF for conversational messaging
// surfaces. An explicit bool forces the behavior in either direction; a missing or
// unrecognized value falls back to the surface-aware default.
//
// The env var is named for Aura's catalog convention (AURA_<DOMAIN>_<UNIT>, _ENABLED
// for booleans) rather than carried over as HERMES_VERIFY_ON_STOP.
func verifyOnStopEnabled(configured string) bool {
	if env, ok := os.LookupEnv("AURA_AGENT_VERIFY_ON_STOP_ENABLED"); ok {
		return !isFalseToken(env)
	}
	switch token := strings.ToLower(strings.TrimSpace(configured)); token {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return !sessionIsMessagingSurface()
}

func isFalseToken(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// candidateCWDs maps changed paths to the directories to ask the detector about,
// de-duplicated and in first-seen order. A path that is itself a directory is
// used as-is; anything else contributes its parent.
//
// The original resolves each candidate against the filesystem; this does not.
// Resolution answers a question about the host, and a policy module that touches
// the disk cannot be tested without one -- the caller passes paths it already
// resolved when it recorded them.
func candidateCWDs(paths []string) []string {
	candidates := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		candidate := filepath.Dir(raw)
		if strings.HasSuffix(raw, string(os.PathSeparator)) || strings.HasSuffix(raw, "/") {
			candidate = filepath.Clean(raw)
		}
		if _, dup := seen[candidate]; dup {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	return candidates
}

// verificationSnapshot returns the status and facts for the first edited
// workspace needing proof, or ok=false when no candidate directory is a project
// the detector recognises. A workspace whose status is already "passed" is
// remembered but keeps being searched past, so a turn that edited two projects
// nudges for the one that still needs proof rather than the first one seen.
func verificationSnapshot(
	ledger VerificationLedger,
	sessionID string,
	changedPaths []string,
) (VerificationStatus, ProjectFacts, bool) {
	if ledger == nil {
		return VerificationStatus{}, ProjectFacts{}, false
	}
	var (
		firstStatus VerificationStatus
		firstFacts  ProjectFacts
		haveFirst   bool
	)
	for _, cwd := range candidateCWDs(changedPaths) {
		facts := ledger.ProjectFactsFor(cwd)
		if !facts.Found {
			continue
		}
		status := ledger.VerificationStatusFor(sessionID, cwd)
		if !haveFirst {
			firstStatus, firstFacts, haveFirst = status, facts, true
		}
		if statusState(status) != "passed" {
			return status, facts, true
		}
	}
	return firstStatus, firstFacts, haveFirst
}

func statusState(status VerificationStatus) string {
	if strings.TrimSpace(status.Status) == "" {
		return "unverified"
	}
	return status.Status
}

// formatChangedPaths renders at most maxChangedPathsInNudge paths and says how
// many were elided, so a hundred-file turn cannot inflate the nudge.
func formatChangedPaths(paths []string) string {
	shown := paths
	if len(shown) > maxChangedPathsInNudge {
		shown = shown[:maxChangedPathsInNudge]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		lines = append(lines, "- `"+path+"`")
	}
	if remaining := len(paths) - len(shown); remaining > 0 {
		lines = append(lines, fmt.Sprintf("- ... and %d more", remaining))
	}
	return strings.Join(lines, "\n")
}

const maxStatusSummary = 1200

// statusDetail renders the state plus, when the ledger has evidence, the last
// command and a bounded slice of its output.
func statusDetail(status VerificationStatus) string {
	state := statusState(status)
	if status.Evidence == nil {
		return state
	}
	command := status.Evidence.CanonicalCommand
	if strings.TrimSpace(command) == "" {
		command = status.Evidence.Command
	}
	summary := strings.TrimSpace(status.Evidence.OutputSummary)
	parts := []string{state}
	if strings.TrimSpace(command) != "" {
		parts = append(parts, "last command `"+command+"`")
	}
	if summary != "" {
		if len(summary) > maxStatusSummary {
			summary = strings.TrimRight(summary[:maxStatusSummary], " \t\n") + "\n... [truncated]"
		}
		parts = append(parts, "last output:\n"+summary)
	}
	return strings.Join(parts, "\n")
}

// VerifyOnStopRequest is the call the loop makes at a voluntary termination.
// Attempts is how many nudges this run has already issued; MaxAttempts bounds
// them so the gate can never wedge a turn.
type VerifyOnStopRequest struct {
	Ledger       VerificationLedger
	SessionID    string
	ChangedPaths []string
	Attempts     int
	MaxAttempts  int
	// Guidance is the optional shipped coding guidance appended to the nudge,
	// paid for only when this evidence gate actually fires (hermes
	// `agent/verify_hooks.py:CODING_VERIFY_GUIDANCE`).
	Guidance string
	// TempDir names where an ad-hoc verification script may be written when the
	// project has no canonical command. The caller supplies it rather than this
	// module reading the host, for the reason candidateCWDs gives.
	TempDir string
}

// BuildVerifyOnStopNudge returns a synthetic follow-up when edited code lacks
// fresh verification, and ok=false when there is nothing to say: no verifiable
// path was touched, the attempt budget is spent, no edited directory is a known
// project, or the ledger already says the workspace passed.
func BuildVerifyOnStopNudge(request VerifyOnStopRequest) (string, bool) {
	// Drop documentation/prose paths (markdown, skills, README, LICENSE, ...) --
	// they carry no verifiable behavior, so a turn that touched only those has
	// nothing to verify and must not nudge.
	paths := dedupeSorted(filterVerifiablePaths(request.ChangedPaths))
	if len(paths) == 0 || request.Attempts >= request.MaxAttempts {
		return "", false
	}

	status, facts, ok := verificationSnapshot(request.Ledger, request.SessionID, paths)
	if !ok {
		return "", false
	}
	if statusState(status) == "passed" {
		return "", false
	}

	addendum := ""
	if guidance := strings.TrimSpace(request.Guidance); guidance != "" {
		addendum = "\n\n" + guidance
	}

	// The prefix is a constant because the loop injects this as a USER-role message
	// and isAgentNudge has to recognise it (llm_agent_verification.go).
	return verifyOnStopNudgePrefix + ", but the workspace does not have " +
		"fresh passing verification evidence yet.\n\n" +
		"Verification status: " + statusDetail(status) + "\n\n" +
		"Changed paths:\n" + formatChangedPaths(paths) + "\n\n" +
		commandInstruction(facts.VerifyCommands, request.TempDir) +
		" If verification is not possible, explain the concrete blocker instead of " +
		"claiming the work is fully verified." + addendum + "]", true
}

// commandInstruction names the project's own verification commands when it has
// them, and otherwise asks for a disposable script -- the original's two branches,
// unchanged in substance.
func commandInstruction(verifyCommands []string, tempDir string) string {
	commands := make([]string, 0, len(verifyCommands))
	for _, command := range verifyCommands {
		if trimmed := strings.TrimSpace(command); trimmed != "" {
			commands = append(commands, trimmed)
		}
	}
	if len(commands) == 0 {
		if strings.TrimSpace(tempDir) == "" {
			tempDir = os.TempDir()
		}
		// realpath, as the original does: a symlinked temp directory named in the
		// nudge sends the agent's later file operations somewhere else than where
		// it wrote. Best-effort -- an unresolvable path is still better named than
		// omitted.
		if resolved, err := filepath.EvalSymlinks(tempDir); err == nil {
			tempDir = resolved
		}
		return "No canonical test/lint/build command was detected. Create a focused " +
			"temporary verification script under `" + tempDir + "` using an OS-safe " +
			"temporary path with an `aura-verify-` filename prefix, run it against the " +
			"changed behavior, clean it up when possible, and summarize it explicitly " +
			"as ad-hoc verification rather than suite green."
	}
	shown := commands
	suffix := ""
	if len(shown) > 3 {
		shown, suffix = shown[:3], ", ..."
	}
	quoted := make([]string, 0, len(shown))
	for _, command := range shown {
		quoted = append(quoted, "`"+command+"`")
	}
	return "Run the relevant verification command now (" + strings.Join(quoted, ", ") + suffix +
		"), read any failure, repair the code, and summarize what passed."
}

func dedupeSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
