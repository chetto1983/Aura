package tools

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	envShellMaxTimeoutMs = "AURA_SHELL_MAX_TIMEOUT_MS"
	envShellOutputBufCap = "AURA_SHELL_OUTPUT_BUF_CAP"
	// envShellDestructivePatterns overrides the on-by-default conservative
	// ADVISORY destructive-command set (B-10): a comma-separated list of RE2
	// patterns REPLACES the default; UNSET or EMPTY keeps the defaults (gate
	// ACTIVE) and only an explicit "off" disables the gate (D-12).
	// Advisory, not a security boundary — see defaultDestructivePatterns.
	envShellDestructivePatterns = "AURA_SHELL_DESTRUCTIVE_PATTERNS"

	defaultShellMaxTimeout = 10 * time.Minute
	defaultShellOutputCap  = 1 << 20
	shellRedacted          = "[REDACTED]"
)

func effectiveShellTimeout(defaultTimeout time.Duration, requestedMs int64) time.Duration {
	timeout := defaultTimeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}
	if requestedMs > 0 {
		timeout = time.Duration(requestedMs) * time.Millisecond
	}
	maxTimeout := shellMaxTimeout()
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func shellMaxTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv(envShellMaxTimeoutMs))
	if v == "" {
		return defaultShellMaxTimeout
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultShellMaxTimeout
	}
	return time.Duration(n) * time.Millisecond
}

func shellOutputBufCap() int {
	v := strings.TrimSpace(os.Getenv(envShellOutputBufCap))
	if v == "" {
		return defaultShellOutputCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultShellOutputCap
	}
	return n
}

func destructiveShellMatch(command string) (bool, error) {
	patterns, err := destructiveShellPatterns()
	if err != nil {
		return false, err
	}
	for _, re := range patterns {
		if re.MatchString(command) {
			return true, nil
		}
	}
	return false, nil
}

// defaultDestructivePatterns is the conservative, on-by-default ADVISORY set
// (B-10 / AP-15). It is NOT a security boundary: the matching is regex over the
// raw command line, so a determined model can phrase around it (e.g. via a
// variable, a heredoc, or an alias) — the real boundary is the host/sandbox
// policy and the operator's own privileges. Its job is to put an obvious
// "are you sure?" approval pause in front of the handful of commands that wipe a
// disk or fork-bomb a machine, so an accidental destructive call does not run
// unattended. Kept narrow on purpose: each pattern targets a whole-tree / device
// wipe, NOT an ordinary `rm file.txt`, so legitimate work is never gated. The
// operator can replace the set with AURA_SHELL_DESTRUCTIVE_PATTERNS, or disable
// the gate entirely with an explicit "off" (an empty value keeps the defaults).
var defaultDestructivePatterns = []*regexp.Regexp{
	// rm -rf / (and -fr, optional sudo, root or top-level path) — whole-tree wipe.
	regexp.MustCompile(`(?i)\brm\s+(-[a-z]*\s+)*-?[rf]*\s*-?[rf]+[a-z]*\s+(--no-preserve-root\s+)?/(\s|$|\w)`),
	// mkfs.* — formatting a filesystem onto a device.
	regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`),
	// dd writing to a raw block device.
	regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/[a-z]`),
	// Classic fork bomb.
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
	// Redirect (truncate) into a raw block device.
	regexp.MustCompile(`>\s*/dev/(sd|nvme|hd|vd|xvd|disk)[a-z0-9]*`),
}

// destructiveShellPatterns returns the active ADVISORY pattern set (B-10). The
// gate is ON BY DEFAULT: an UNSET *or EMPTY* AURA_SHELL_DESTRUCTIVE_PATTERNS
// yields the conservative built-in set (defaultDestructivePatterns), so a clearly
// destructive command trips the approval pause without the operator opting in —
// copying .env.example (commented line) therefore never disables the gate (D-12).
// A non-empty value REPLACES the default (no merge); only an explicit, case-
// insensitive "off" is the opt-out that disables the gate. The leaf stays
// profile-agnostic — the production "forbid off" check lives in the config
// validator, which reads the raw env value (it does NOT branch this function).
func destructiveShellPatterns() ([]*regexp.Regexp, error) {
	raw, set := os.LookupEnv(envShellDestructivePatterns)
	raw = strings.TrimSpace(raw)
	if !set || raw == "" {
		return defaultDestructivePatterns, nil
	}
	if strings.EqualFold(raw, "off") {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]*regexp.Regexp, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		re, err := regexp.Compile(part)
		if err != nil {
			return nil, fmt.Errorf("%s: compile %q: %w", envShellDestructivePatterns, part, err)
		}
		out = append(out, re)
	}
	return out, nil
}

var shellSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization\s*[:=]\s*[^\r\n]+`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-+/=]+`),
	regexp.MustCompile(`sk-(or-)?[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`(AKIA|ASIA|AROA|AIDA|ANPA|ANVA|AIAA)[0-9A-Z]{16}`),
	regexp.MustCompile(`(?i)"(password|api[_-]?key|token|secret)"\s*:\s*"[^"]{4,}"`),
	regexp.MustCompile(`(?i)(password|api[_-]?key|token|secret)\s*[:=]\s*("?)[^"\s&]+`),
}

var shellCredentialURLPattern = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^:/?#\s@]+:)[^@/?#\s]+(@)`)

func redactModelPreview(s string) string {
	s = shellCredentialURLPattern.ReplaceAllString(s, "${1}***${2}")
	for _, re := range shellSecretPatterns {
		s = re.ReplaceAllString(s, shellRedacted)
	}
	return s
}
