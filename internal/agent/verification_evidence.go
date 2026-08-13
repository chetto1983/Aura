// Coding verification evidence: classifying a shell command as proof.
//
// A Go port of the classification half of NousResearch/hermes-agent
// `agent/verification_evidence.py` (MIT, read at commit 9d4ef04ed). The storage half
// is migration 0094 + verification_evidence_store.go; this file is pure and does no
// I/O beyond the two path-containment checks the original also performs.
//
// The module is deliberately PASSIVE, in the original's words: it never decides to run
// a suite, never blocks completion, and never upgrades a targeted check into "repo
// green". It answers one question — is this command, with this exit code, evidence
// that this workspace was verified?
//
// Two deliberate deviations from the source, both recorded rather than hidden:
//
//   - `_split_segment_tokens` is called twice upstream, with posix=True and posix=False,
//     so ad-hoc scripts written with Windows backslash paths still match. Aura runs only
//     on Linux in a container (its shell tool executes inside the per-identity sandbox),
//     so a backslash path cannot reach this code and the second pass would be a branch no
//     input can take.
//   - `_find_subsequence` is NOT ported. It has exactly one occurrence in the hermes
//     tree — its own definition — so it is dead there; matching is done by the prefix
//     comparison in findCanonicalMatch.
package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/shlex"
)

const (
	maxOutputSummaryChars = 2000
	adHocKind             = "ad_hoc"
	adHocCanonical        = "ad-hoc verification script"
)

// adHocScriptNamePrefixes are the filename prefixes that make a throwaway script count
// as verification. The nudge in verification_stop.go asks for exactly this prefix, so
// the two must change together.
var adHocScriptNamePrefixes = []string{"aura-verify-", "aura-ad-hoc-"}

// shellSplit separates the command list operators. Only the separators matter here:
// each segment is tokenised on its own, and a command that verifies is evidence whether
// it ran first, last, or after an unrelated cd.
var shellSplit = regexp.MustCompile(`\s*(?:&&|\|\||;)\s*`)

// VerificationCommandEvidence is a classified command result worth recording.
type VerificationCommandEvidence struct {
	Command          string
	CanonicalCommand string
	Kind             string
	Scope            string
	Status           string
	ExitCode         int
	CWD              string
	Root             string
	SessionID        string
	OutputSummary    string
}

func splitSegmentTokens(command string) [][]string {
	segments := make([][]string, 0, 2)
	for _, segment := range shellSplit.Split(strings.TrimSpace(command), -1) {
		if segment == "" {
			continue
		}
		tokens, err := shlex.Split(segment)
		if err != nil || len(tokens) == 0 {
			continue
		}
		segments = append(segments, tokens)
	}
	return segments
}

func cleanToken(token string) string {
	token = strings.TrimSpace(token)
	for strings.HasPrefix(token, "./") {
		token = token[2:]
	}
	return token
}

func canonicalTokens(canonical string) []string {
	tokens, err := shlex.Split(canonical)
	if err != nil {
		return nil
	}
	cleaned := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token != "" {
			cleaned = append(cleaned, cleanToken(token))
		}
	}
	return cleaned
}

// stripCommandPrefix removes harmless command prefixes before matching canonical
// commands, so `env FOO=1 go test ./...` is the same evidence as `go test ./...`.
func stripCommandPrefix(tokens []string) []string {
	remaining := tokens
	if len(remaining) > 0 && remaining[0] == "env" {
		remaining = remaining[1:]
	}
	for len(remaining) > 0 && strings.Contains(remaining[0], "=") && !strings.HasPrefix(remaining[0], "-") {
		remaining = remaining[1:]
	}
	for len(remaining) > 0 {
		switch remaining[0] {
		case "command", "time", "noglob":
			remaining = remaining[1:]
			continue
		}
		break
	}
	return remaining
}

// equivalentNeedles returns the command spellings equivalent to the detected canonical
// command. Without this a project whose canonical command is `pytest` would refuse to
// recognise the `uv run pytest` the agent actually typed.
func equivalentNeedles(needle []string) [][]string {
	candidates := [][]string{needle}
	if len(needle) >= 3 && needle[1] == "run" {
		switch needle[0] {
		case "npm", "pnpm", "yarn", "bun":
			candidates = append(candidates, []string{needle[0], needle[2]})
		}
	}
	if len(needle) == 1 && strings.Contains(needle[0], "/") {
		candidates = append(candidates, []string{"bash", needle[0]}, []string{"sh", needle[0]})
	}
	if len(needle) == 1 && needle[0] == "pytest" {
		candidates = append(candidates,
			[]string{"python", "-m", "pytest"},
			[]string{"python3", "-m", "pytest"},
			[]string{"uv", "run", "pytest"},
			[]string{"poetry", "run", "pytest"},
			[]string{"pipenv", "run", "pytest"},
		)
	}
	return candidates
}

// findCanonicalMatch returns the canonical command and the arguments that followed it,
// for the first of the project's commands detected in any segment.
func findCanonicalMatch(command string, canonicalCommands []string) (string, []string, bool) {
	segments := splitSegmentTokens(command)
	for _, canonical := range canonicalCommands {
		needle := canonicalTokens(canonical)
		if len(needle) == 0 {
			continue
		}
		for _, tokens := range segments {
			candidateTokens := stripCommandPrefix(tokens)
			for _, candidate := range equivalentNeedles(needle) {
				if hasTokenPrefix(candidateTokens, candidate) {
					return canonical, candidateTokens[len(candidate):], true
				}
			}
		}
	}
	return "", nil, false
}

// hasTokenPrefix compares the RAW typed tokens against the CLEANED needle, which is
// what the original does (`candidate_tokens[:len(candidate)] == candidate`, where only
// the needle passed through _clean_token). The asymmetry is real and load-bearing: a
// canonical command written `./scripts/test.sh` matches a typed `scripts/test.sh`, and
// NOT the other way round. Cleaning both sides here -- which the first draft of this
// port did -- silently widened the match and made `go test ./...` swallow its own
// argument, turning a targeted run into a full one.
func hasTokenPrefix(tokens, prefix []string) bool {
	if len(prefix) == 0 || len(tokens) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if tokens[i] != want {
			return false
		}
	}
	return true
}

func kindForCommand(canonical string) string {
	lowered := strings.ToLower(canonical)
	if containsAny(lowered, "lint", "eslint", "ruff") {
		return "lint"
	}
	if containsAny(lowered, "typecheck", "tsc", "mypy", "pyright", "ty") {
		return "typecheck"
	}
	if strings.Contains(lowered, "build") {
		return "build"
	}
	if containsAny(lowered, "fmt", "format") {
		return "format"
	}
	if strings.Contains(lowered, "check") && !strings.Contains(lowered, "test") {
		return "check"
	}
	return "test"
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// looksLikeTarget reports whether an argument names a specific file, directory or test
// selector. It is what separates `go test ./internal/agent` from `go test ./...`.
func looksLikeTarget(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	if strings.ContainsAny(arg, `/\`) || strings.Contains(arg, "::") {
		return true
	}
	for _, extension := range []string{".py", ".js", ".jsx", ".ts", ".tsx", ".rs", ".go", ".java"} {
		if strings.HasSuffix(arg, extension) {
			return true
		}
	}
	for _, prefix := range []string{"test_", "tests", "spec", "__tests__"} {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

// scopeForArgs is the guard against a targeted run being read back as repo-green.
func scopeForArgs(args []string) string {
	if slices.ContainsFunc(args, looksLikeTarget) {
		return "targeted"
	}
	return "full"
}

func isUnderTempDir(token string) bool {
	if token == "" || strings.HasPrefix(token, "-") {
		return false
	}
	if !filepath.IsAbs(token) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(token)
	if err != nil {
		resolved = filepath.Clean(token)
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tempRoot = filepath.Clean(os.TempDir())
	}
	return resolved == tempRoot || strings.HasPrefix(resolved, tempRoot+string(os.PathSeparator))
}

func isUnderRoot(token, root string) bool {
	if root == "" {
		return false
	}
	path := filepath.Clean(token)
	rootPath := filepath.Clean(root)
	return path == rootPath || strings.HasPrefix(path, rootPath+string(os.PathSeparator))
}

// isTempScriptPath demands all THREE conditions the original demands: the prefixed
// name, residence under the temp directory, and NOT being under the project root. A
// script inside the repo is source code, not a disposable check, however it is named.
func isTempScriptPath(token, root string) bool {
	name := filepath.Base(token)
	prefixed := slices.ContainsFunc(adHocScriptNamePrefixes, func(prefix string) bool {
		return strings.HasPrefix(name, prefix)
	})
	return prefixed && isUnderTempDir(token) && !isUnderRoot(token, root)
}

// adHocScriptArgs returns the arguments after a disposable verification script, or
// ok=false when the segment does not invoke one.
func adHocScriptArgs(tokens []string, root string) ([]string, bool) {
	candidateTokens := stripCommandPrefix(tokens)
	if len(candidateTokens) == 0 {
		return nil, false
	}
	command := candidateTokens[0]
	if isTempScriptPath(command, root) {
		return candidateTokens[1:], true
	}
	switch command {
	case "python", "python3", "node", "bash", "sh", "ruby", "perl":
		for index, token := range candidateTokens[1:] {
			if token == "--" {
				continue
			}
			if isTempScriptPath(token, root) {
				return candidateTokens[index+2:], true
			}
			if !strings.HasPrefix(token, "-") {
				return nil, false
			}
		}
	}
	return nil, false
}

func findAdHocMatch(command, root string) ([]string, bool) {
	for _, tokens := range splitSegmentTokens(command) {
		if args, ok := adHocScriptArgs(tokens, root); ok {
			return args, true
		}
	}
	return nil, false
}

// summarizeOutput keeps the head and the tail and says how much it dropped between
// them: a failure's cause is usually at the top and its verdict at the bottom.
func summarizeOutput(output string) string {
	text := strings.TrimSpace(output)
	if len(text) <= maxOutputSummaryChars {
		return text
	}
	head := maxOutputSummaryChars / 3
	tail := maxOutputSummaryChars - head
	return text[:head] +
		"\n... [" + strconv.Itoa(len(text)-maxOutputSummaryChars) + " chars omitted] ...\n" +
		text[len(text)-tail:]
}

// ClassifyVerificationCommandInput is one finished shell command to judge.
type ClassifyVerificationCommandInput struct {
	Command   string
	CWD       string
	Root      string
	SessionID string
	ExitCode  int
	Output    string
	// VerifyCommands are the project's own canonical commands, from the detector.
	// EMPTY is meaningful: it is the only case in which a disposable ad-hoc script
	// can stand in as evidence, exactly as the original gates it.
	VerifyCommands []string
}

// ClassifyVerificationCommand returns the evidence a command constitutes, or ok=false
// when it is not verification at all. A failing command IS evidence -- with status
// "failed" -- because the ledger records what was proved, not only what passed.
func ClassifyVerificationCommand(input ClassifyVerificationCommandInput) (VerificationCommandEvidence, bool) {
	if strings.TrimSpace(input.Command) == "" {
		return VerificationCommandEvidence{}, false
	}
	canonical, trailingArgs, matched := findCanonicalMatch(input.Command, input.VerifyCommands)
	isAdHoc := false
	if !matched && len(input.VerifyCommands) == 0 {
		if args, ok := findAdHocMatch(input.Command, input.Root); ok {
			canonical, trailingArgs, matched, isAdHoc = adHocCanonical, args, true, true
		}
	}
	if !matched {
		return VerificationCommandEvidence{}, false
	}

	kind, scope := kindForCommand(canonical), scopeForArgs(trailingArgs)
	if isAdHoc {
		// An ad-hoc script proves what it happens to cover and nothing more, so it can
		// never be full scope however its arguments read.
		kind, scope = adHocKind, "targeted"
	}
	status := "failed"
	if input.ExitCode == 0 {
		status = "passed"
	}
	root := input.Root
	if strings.TrimSpace(root) == "" {
		root = input.CWD
	}
	sessionID := input.SessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	return VerificationCommandEvidence{
		Command:          input.Command,
		CanonicalCommand: canonical,
		Kind:             kind,
		Scope:            scope,
		Status:           status,
		ExitCode:         input.ExitCode,
		CWD:              input.CWD,
		Root:             root,
		SessionID:        sessionID,
		OutputSummary:    summarizeOutput(input.Output),
	}, true
}
