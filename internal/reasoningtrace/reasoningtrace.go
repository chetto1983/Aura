package reasoningtrace

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Env is the switch that enables JSONL reasoning trace output.
const Env = "AURA_REASONING_TRACE"
const fileEnv = "AURA_REASONING_TRACE_FILE"

var mu sync.Mutex

// Enabled reports whether reasoning tracing is enabled for the current process.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(Env))) {
	case "1", "true", "yes", "on", "full":
		return true
	default:
		return false
	}
}

// Path returns the operator-selected JSONL trace path, or a temp-file default.
func Path() string {
	if p := strings.TrimSpace(os.Getenv(fileEnv)); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "aura-reasoning-trace.jsonl")
}

// Record writes a JSONL row with redacted fields when tracing is enabled.
func Record(stage string, fields map[string]any) {
	if !Enabled() {
		return
	}
	row := make(map[string]any, len(fields)+4)
	row["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	row["pid"] = os.Getpid()
	row["stage"] = stage
	for k, v := range fields {
		row[k] = redactValue(v)
	}
	line, err := json.Marshal(row)
	if err != nil {
		slog.Warn("reasoning trace: marshal failed", "stage", stage, "err", err)
		return
	}
	line = []byte(redactString(string(line)))
	p := Path()
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		slog.Warn("reasoning trace: mkdir failed", "path", p, "err", err)
		return
	}
	// #nosec G304 -- AURA_REASONING_TRACE_FILE is an operator-controlled debug destination.
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		slog.Warn("reasoning trace: open failed", "path", p, "err", err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Warn("reasoning trace: write failed", "path", p, "err", err)
	}
}

// RuneLen returns the number of UTF-8 runes in s for trace-size metadata.
func RuneLen(s string) int {
	return utf8.RuneCountInString(s)
}

func redactValue(v any) any {
	switch x := v.(type) {
	case string:
		return redactString(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = redactValue(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = redactValue(v)
		}
		return out
	default:
		return v
	}
}

func redactString(s string) string {
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if !ok || len(value) < 8 {
			continue
		}
		upper := strings.ToUpper(name)
		if !strings.Contains(upper, "KEY") &&
			!strings.Contains(upper, "TOKEN") &&
			!strings.Contains(upper, "PASSWORD") &&
			!strings.Contains(upper, "SECRET") {
			continue
		}
		s = strings.ReplaceAll(s, value, "[REDACTED_"+upper+"]")
	}
	return s
}
