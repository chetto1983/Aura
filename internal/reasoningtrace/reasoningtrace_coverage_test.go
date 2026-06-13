package reasoningtrace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// readRows parses the JSONL trace at p into one decoded map per line.
func readRows(t *testing.T, p string) []map[string]any {
	t.Helper()
	f, err := os.Open(p) // #nosec G304 -- test-controlled temp path.
	if err != nil {
		t.Fatalf("open trace %s: %v", p, err)
	}
	defer func() { _ = f.Close() }()
	var rows []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row not valid JSON %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func TestEnabled(t *testing.T) {
	truthy := []string{"1", "true", "yes", "on", "full", "TRUE", "On", " yes ", "Full"}
	for _, v := range truthy {
		t.Run("on/"+strings.TrimSpace(v), func(t *testing.T) {
			t.Setenv(Env, v)
			if !Enabled() {
				t.Fatalf("Enabled()=false for %q, want true", v)
			}
		})
	}
	falsy := []string{"", "0", "false", "no", "off", "nope", "2", "enable"}
	for _, v := range falsy {
		t.Run("off/"+v, func(t *testing.T) {
			t.Setenv(Env, v)
			if Enabled() {
				t.Fatalf("Enabled()=true for %q, want false", v)
			}
		})
	}
}

func TestPath(t *testing.T) {
	t.Run("env_override", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "custom-trace.jsonl")
		t.Setenv(fileEnv, want)
		if got := Path(); got != want {
			t.Fatalf("Path()=%q, want %q", got, want)
		}
	})
	t.Run("env_whitespace_trimmed", func(t *testing.T) {
		base := filepath.Join(t.TempDir(), "padded.jsonl")
		t.Setenv(fileEnv, "  "+base+"  ")
		if got := Path(); got != base {
			t.Fatalf("Path()=%q, want trimmed %q", got, base)
		}
	})
	t.Run("default_temp_when_unset", func(t *testing.T) {
		t.Setenv(fileEnv, "")
		got := Path()
		want := filepath.Join(os.TempDir(), "aura-reasoning-trace.jsonl")
		if got != want {
			t.Fatalf("Path()=%q, want default %q", got, want)
		}
	})
	t.Run("default_when_whitespace_only", func(t *testing.T) {
		t.Setenv(fileEnv, "   ")
		want := filepath.Join(os.TempDir(), "aura-reasoning-trace.jsonl")
		if got := Path(); got != want {
			t.Fatalf("Path()=%q, want default %q", got, want)
		}
	})
}

func TestMaxTraceBytes(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want int64
	}{
		{"unset_defaults", false, "", defaultMaxTraceBytes},
		{"empty_defaults", true, "", defaultMaxTraceBytes},
		{"whitespace_defaults", true, "   ", defaultMaxTraceBytes},
		{"positive_int", true, "4096", 4096},
		{"positive_with_padding", true, "  512  ", 512},
		{"zero_defaults", true, "0", defaultMaxTraceBytes},
		{"negative_defaults", true, "-1", defaultMaxTraceBytes},
		{"garbage_defaults", true, "notanint", defaultMaxTraceBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(maxBytesEnv, tc.val)
			} else {
				os.Unsetenv(maxBytesEnv)
			}
			if got := maxTraceBytes(); got != tc.want {
				t.Fatalf("maxTraceBytes()=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestRuneLen(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"héllo", 5}, // accented rune is one rune, two bytes
		{"日本語", 3},   // 3 runes, 9 bytes
		{"a😀b", 3},   // emoji is a single rune
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := RuneLen(tc.in); got != tc.want {
				t.Fatalf("RuneLen(%q)=%d, want %d", tc.in, got, tc.want)
			}
			if RuneLen(tc.in) > utf8.RuneCountInString(tc.in) {
				t.Fatalf("RuneLen exceeded byte-independent rune count for %q", tc.in)
			}
		})
	}
}

// TestRecord_DisabledIsNoop asserts the early return: with tracing off, Record
// must not create the destination file at all.
func TestRecord_DisabledIsNoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "0")
	t.Setenv(fileEnv, p)

	Record("stage", map[string]any{"k": "v"})

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("disabled Record created %s (err=%v), want no file", p, err)
	}
}

// TestRecord_WritesRowWithMetadata asserts the happy path emits one JSONL row
// carrying the ts/pid/stage envelope plus the caller's fields.
func TestRecord_WritesRowWithMetadata(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("planning", map[string]any{"detail": "hello", "n": 7})

	rows := readRows(t, p)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row["stage"] != "planning" {
		t.Errorf("stage=%v, want planning", row["stage"])
	}
	if _, ok := row["ts"].(string); !ok {
		t.Errorf("ts missing or non-string: %v", row["ts"])
	}
	if _, ok := row["pid"]; !ok {
		t.Errorf("pid missing")
	}
	if row["detail"] != "hello" {
		t.Errorf("detail=%v, want hello", row["detail"])
	}
	// JSON numbers decode to float64.
	if row["n"] != float64(7) {
		t.Errorf("n=%v, want 7", row["n"])
	}
}

// TestRecord_AppendsAcrossCalls confirms each enabled call appends a new line
// rather than truncating prior content.
func TestRecord_AppendsAcrossCalls(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("a", map[string]any{"i": 1})
	Record("b", map[string]any{"i": 2})
	Record("c", nil) // nil fields map is valid: ranges over nothing.

	rows := readRows(t, p)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0]["stage"] != "a" || rows[1]["stage"] != "b" || rows[2]["stage"] != "c" {
		t.Fatalf("unexpected stage order: %v", []any{rows[0]["stage"], rows[1]["stage"], rows[2]["stage"]})
	}
}

// TestRecord_MarshalFailureIsSwallowed drives the json.Marshal error branch:
// an unmarshalable value (func) passes redactValue's default case unchanged,
// then fails Marshal. Record must warn-and-return without writing a row.
func TestRecord_MarshalFailureIsSwallowed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("bad", map[string]any{"fn": func() {}})

	// No file is created because the marshal failure returns before OpenFile.
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("marshal-failure Record created %s (err=%v), want no file", p, err)
	}
}

// TestRecord_OpenFailureIsSwallowed drives the os.OpenFile error branch: when the
// target path is itself a directory, the O_WRONLY open fails and Record must
// warn-and-return without panicking.
func TestRecord_OpenFailureIsSwallowed(t *testing.T) {
	dir := t.TempDir()
	// Point the trace at an existing directory: MkdirAll(Dir(p)) succeeds, but
	// OpenFile(p, O_WRONLY) on a directory fails.
	p := filepath.Join(dir, "iamdir")
	if err := os.Mkdir(p, 0o750); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("stage", map[string]any{"k": "v"}) // must not panic

	// The directory still exists and was not replaced by a regular file.
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("target %s is no longer a directory", p)
	}
}

// TestRecord_RedactsSecretsInValuesAndKeys verifies redactValue's recursive
// reach across nested slices/maps and redactString's whole-line pass: a secret
// env value appearing anywhere in the row is replaced by its placeholder.
func TestRecord_RedactsSecretsInValuesAndKeys(t *testing.T) {
	const secret = "supersecretvalue123"
	t.Setenv("MYAPP_API_KEY", secret)
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("auth", map[string]any{
		"flat":   "token=" + secret,
		"nested": map[string]any{"inner": secret},
		"list":   []any{"prefix-" + secret, "clean"},
	})

	raw, err := os.ReadFile(p) // #nosec G304 -- test temp path.
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, secret) {
		t.Fatalf("raw secret leaked into trace: %s", got)
	}
	if !strings.Contains(got, "[REDACTED_MYAPP_API_KEY]") {
		t.Fatalf("redaction placeholder missing: %s", got)
	}
	// The clean list element survives untouched.
	rows := readRows(t, p)
	list, ok := rows[0]["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("list field malformed: %v", rows[0]["list"])
	}
	if list[1] != "clean" {
		t.Errorf("non-secret list element altered: %v", list[1])
	}
}

// TestRedactValue exercises the type switch directly, including the passthrough
// default branch and recursion through slices and maps.
func TestRedactValue(t *testing.T) {
	const secret = "anothersecretvalue999"
	t.Setenv("SVC_ACCESS_TOKEN", secret)

	t.Run("string_redacted", func(t *testing.T) {
		got := redactValue("bearer " + secret)
		if s := got.(string); strings.Contains(s, secret) {
			t.Fatalf("string not redacted: %q", s)
		}
	})
	t.Run("slice_recursed", func(t *testing.T) {
		got := redactValue([]any{secret, "ok", 42}).([]any)
		if len(got) != 3 {
			t.Fatalf("slice len=%d, want 3", len(got))
		}
		if strings.Contains(got[0].(string), secret) {
			t.Fatalf("slice element not redacted: %v", got[0])
		}
		if got[1] != "ok" || got[2] != 42 {
			t.Fatalf("non-secret slice elements altered: %v", got)
		}
	})
	t.Run("map_recursed", func(t *testing.T) {
		got := redactValue(map[string]any{"key": secret, "n": 1}).(map[string]any)
		if strings.Contains(got["key"].(string), secret) {
			t.Fatalf("map value not redacted: %v", got["key"])
		}
		if got["n"] != 1 {
			t.Fatalf("non-secret map value altered: %v", got["n"])
		}
	})
	t.Run("default_passthrough", func(t *testing.T) {
		// int, bool, nil and other types fall through unchanged.
		if redactValue(99) != 99 {
			t.Errorf("int passthrough altered")
		}
		if redactValue(true) != true {
			t.Errorf("bool passthrough altered")
		}
		if redactValue(nil) != nil {
			t.Errorf("nil passthrough altered")
		}
		f := 3.14
		if redactValue(f) != f {
			t.Errorf("float passthrough altered")
		}
	})
}

// TestRedactString_SkipsShortAndNonSensitive confirms the guard clauses: values
// under 8 bytes and env names lacking a sensitive keyword are not substituted.
func TestRedactString_SkipsShortAndNonSensitive(t *testing.T) {
	t.Setenv("MYAPP_API_KEY", "short")           // value < 8 bytes -> skipped
	t.Setenv("MYAPP_HOSTNAME", "plainhostvalue") // name not sensitive -> skipped

	in := "log line short and plainhostvalue together"
	if got := redactString(in); got != in {
		t.Fatalf("redactString altered a non-secret line: %q", got)
	}
}
