package reasoningtrace

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	outboundredact "github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/chetto1983/aura/internal/tracesink"
)

// Env is the switch that enables JSONL reasoning trace output.
const Env = "AURA_REASONING_TRACE"
const fileEnv = "AURA_REASONING_TRACE_FILE"
const maxBytesEnv = "AURA_REASONING_TRACE_MAX_BYTES"
const encryptKeyEnv = "AURA_TRACE_ENCRYPT_KEY"

// defaultMaxTraceBytes caps the active JSONL trace before it rotates to a single
// .1 backup, so an always-on trace cannot grow the run dir without bound (M-06).
const defaultMaxTraceBytes int64 = 8 << 20 // 8 MiB

// defaultMaxFieldBytes caps individual trace fields before env-secret redaction.
// AURA_REASONING_TRACE=full is an explicit operator debug mode. Full records
// still apply this cap and are persisted only through the encrypted sink.
const defaultMaxFieldBytes = 4096

var (
	mu                 sync.Mutex
	optionalSuppressed atomic.Bool
	fullSinkWarned     atomic.Bool
)

// SetOptionalSuppressed lets the bounded disk observer pause debug trace files
// without changing the operator's configured trace mode.
func SetOptionalSuppressed(suppressed bool) {
	optionalSuppressed.Store(suppressed)
}

// Enabled reports whether reasoning tracing is enabled for the current process.
func Enabled() bool {
	if optionalSuppressed.Load() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(Env))) {
	case "1", "true", "yes", "on", "summary", "full":
		return true
	default:
		return false
	}
}

// FullEnabled reports whether the operator requested detailed trace payloads.
// The payload is still redacted and is persisted only as encrypted records.
func FullEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(Env)), "full")
}

// Path returns the operator-selected JSONL trace path, or a temp-file default.
func Path() string {
	if p := strings.TrimSpace(os.Getenv(fileEnv)); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "aura-reasoning-trace.jsonl")
}

// traceRowCap returns a non-negative map-capacity hint for the trace row,
// saturating to maxInt rather than wrapping if fieldCount+4 ever overflowed (it
// cannot in practice — fieldCount is a live map len() — but an unbounded add
// feeding make() is an arithmetic-safety footgun CodeQL flags).
func traceRowCap(fieldCount int) int {
	const fixed = 4
	if fieldCount < 0 {
		return fixed
	}
	if fieldCount > math.MaxInt-fixed {
		return math.MaxInt
	}
	return fieldCount + fixed
}

// Record writes a JSONL row with redacted fields when tracing is enabled.
func Record(stage string, fields map[string]any) {
	if !Enabled() {
		return
	}
	row := make(map[string]any, traceRowCap(len(fields)))
	row["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	row["pid"] = os.Getpid()
	row["stage"] = redactString(capTraceString(stage))
	for k, v := range fields {
		row[k] = redactValueForKey(k, v)
	}
	line, err := json.Marshal(row)
	if err != nil {
		slog.Warn("reasoning trace: marshal failed", "stage", stage, "err", err)
		return
	}
	// Values received one exact-match pass before marshal. Apply the outbound
	// credential-pattern pass exactly once to the serialized persistence row.
	line = []byte(outboundredact.String(string(line)))
	p := Path()
	mu.Lock()
	defer mu.Unlock()
	rotateIfOversized(p)
	if FullEnabled() {
		sink, err := tracesink.New(p, os.Getenv(encryptKeyEnv))
		if err != nil {
			warnFullSinkOnce("reasoning trace: encrypted sink unavailable; record dropped", p, err)
			return
		}
		if err := sink.Write(line); err != nil {
			warnFullSinkOnce("reasoning trace: encrypted write failed; record dropped", p, err)
		}
		return
	}
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

func warnFullSinkOnce(message, path string, err error) {
	if fullSinkWarned.CompareAndSwap(false, true) {
		slog.Warn(message, "path", path, "err", err)
	}
}

// maxTraceBytes is the active-file rotation threshold: AURA_REASONING_TRACE_MAX_BYTES
// when it parses to a positive int, else the 8 MiB default.
func maxTraceBytes() int64 {
	if v := strings.TrimSpace(os.Getenv(maxBytesEnv)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxTraceBytes
}

// rotateIfOversized renames an over-cap trace to a single .1 backup so the active
// file restarts empty (M-06). Best-effort under the caller's lock: a failed rotate
// just keeps appending. os.Remove precedes Rename because a Windows Rename cannot
// replace an existing target.
func rotateIfOversized(p string) {
	fi, err := os.Stat(p)
	if err != nil || fi.Size() < maxTraceBytes() {
		return
	}
	backup := p + ".1"
	_ = os.Remove(backup)
	_ = os.Rename(p, backup)
}

// RuneLen returns the number of UTF-8 runes in s for trace-size metadata.
func RuneLen(s string) int {
	return utf8.RuneCountInString(s)
}

func redactValue(v any) any {
	switch x := v.(type) {
	case string:
		return redactString(capTraceString(x))
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

func redactValueForKey(key string, v any) any {
	if !FullEnabled() && defaultPrivateField(key) {
		return traceValueSummary(v)
	}
	return redactValue(v)
}

func defaultPrivateField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "history", "messages", "user", "prompt", "input":
		return true
	default:
		return false
	}
}

func traceValueSummary(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		raw = fmt.Appendf(nil, "%T", v)
	}
	sum := sha256.Sum256(raw)
	out := map[string]any{
		"redacted": true,
		"type":     fmt.Sprintf("%T", v),
		"bytes":    len(raw),
		"sha256":   fmt.Sprintf("%x", sum[:]),
	}
	rv := reflect.ValueOf(v)
	if rv.IsValid() {
		switch rv.Kind() {
		case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
			out["count"] = rv.Len()
		}
	}
	return out
}

func capTraceString(s string) string {
	if len(s) <= defaultMaxFieldBytes {
		return s
	}
	const marker = "\n[trace field truncated]"
	limit := defaultMaxFieldBytes - len(marker)
	if limit <= 0 {
		limit = defaultMaxFieldBytes
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

func redactString(s string) string {
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if !ok || len(value) < 8 {
			continue
		}
		if !secret.IsSecretEnvVar(name, value) {
			continue
		}
		s = strings.ReplaceAll(s, value, "[REDACTED_"+strings.ToUpper(name)+"]")
	}
	return s
}
