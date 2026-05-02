// Package setup hosts the first-run wizard. It runs before the bot when
// TelegramToken is blank, mints the token + initial provider config, and
// hands control back to cmd/aura/main.go to continue the normal startup
// path. The wizard is a server-rendered HTML form (no SPA dependency)
// because the dashboard SPA's API requires a bearer token that doesn't
// exist yet on a fresh install.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeDotEnvKey upserts key=value in the .env file at path. If the file
// or key is missing, it's appended. Comments and unrelated keys are
// preserved. Atomic write via temp-file rename so a crash never leaves a
// half-written .env.
//
// Values are quoted with double quotes when they contain spaces, '#', or
// '"' so a token with shell-special characters round-trips through
// loadDotEnv (cmd/aura/main.go).
func writeDotEnvKey(path, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("setup: empty .env key")
	}
	encoded := encodeDotEnvValue(value)

	var existing []string
	replaced := false
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		// Default token buffer caps at 64K; bump for safety on large files.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				existing = append(existing, line)
				continue
			}
			k, _, ok := strings.Cut(trimmed, "=")
			if ok && strings.TrimSpace(k) == key {
				existing = append(existing, key+"="+encoded)
				replaced = true
				continue
			}
			existing = append(existing, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("setup: read .env: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("setup: open .env: %w", err)
	}

	if !replaced {
		existing = append(existing, key+"="+encoded)
	}

	out := strings.Join(existing, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, ".env.*.tmp")
	if err != nil {
		return fmt.Errorf("setup: temp .env: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("setup: write .env: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("setup: close .env: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("setup: rename .env: %w", err)
	}
	return nil
}

// encodeDotEnvValue mirrors the parsing semantics in cmd/aura/main.go's
// loadDotEnv: surrounding double/single quotes are stripped on read, so
// we only quote when a quote is needed for round-trip correctness.
func encodeDotEnvValue(v string) string {
	if v == "" {
		return ""
	}
	needsQuote := strings.ContainsAny(v, " \t#\"'\n\r")
	if !needsQuote {
		return v
	}
	// Escape backslashes + double quotes inside the string, then wrap.
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
