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
	envShellMaxTimeoutMs        = "AURA_SHELL_MAX_TIMEOUT_MS"
	envShellOutputBufCap        = "AURA_SHELL_OUTPUT_BUF_CAP"
	envShellDestructivePatterns = "AURA_SHELL_DESTRUCTIVE_PATTERNS"

	defaultShellMaxTimeout = 10 * time.Minute
	defaultShellOutputCap  = 1 << 20
	shellRedacted          = "[REDACTED]"
)

func secretEnvKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "auth", "bearer", "credential"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

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

func destructiveShellPatterns() ([]*regexp.Regexp, error) {
	raw := strings.TrimSpace(os.Getenv(envShellDestructivePatterns))
	if raw == "" {
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

func redactModelPreview(s string) string {
	for _, re := range shellSecretPatterns {
		s = re.ReplaceAllString(s, shellRedacted)
	}
	return s
}
