package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fs_edit, fs_grep and fs_glob reached the host filesystem under every profile while fs_read and
// fs_write were confined to the per-identity box — reads and writes fenced, edits and searches not
// (fix plan 2.5). These tests cover the daemon-free half of the fix: the routing decision with no
// router, the shared match/scan/framing logic both paths run, and the box path helpers. The
// container-gated half lives under docker_integration and contributes ZERO coverage, so anything
// testable without a daemon has to be tested here or it is not tested at all (CLAUDE.md).

// A nil Router must keep the host behaviour byte-for-byte: dev/local_trusted never reaches the box
// (SC-4). Route is nil-receiver-safe and returns routed=false, so these tools must still work.
func TestUnroutedFSToolsStillRunOnTheHost(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "app.go")
	if err := os.WriteFile(target, []byte("port := 8080\nname := \"aura\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)

	edit := &FSEdit{WorkspaceRoot: dir}
	if _, err := edit.Execute(ctx, json.RawMessage(
		`{"path":"app.go","old_string":"port := 8080","new_string":"port := 9090"}`,
	)); err != nil {
		t.Fatalf("unrouted fs_edit: %v", err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "port := 9090") {
		t.Errorf("unrouted fs_edit did not write the host file: %q", after)
	}

	glob := &FSGlob{WorkspaceRoot: dir}
	res, err := glob.Execute(ctx, json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("unrouted fs_glob: %v", err)
	}
	if !strings.Contains(res.Preview, "app.go") {
		t.Errorf("unrouted fs_glob missed the host file: %q", res.Preview)
	}

	grep := &FSGrep{WorkspaceRoot: dir}
	res, err = grep.Execute(ctx, json.RawMessage(`{"pattern":"9090"}`))
	if err != nil {
		t.Fatalf("unrouted fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "app.go:1") {
		t.Errorf("unrouted fs_grep missed the host match: %q", res.Preview)
	}
}

func TestApplyExactEditRules(t *testing.T) {
	tests := []struct {
		name    string
		content string
		args    fsEditArgs
		want    string
		wantN   int
		wantErr string
	}{
		{
			name:    "unique match replaced once",
			content: "a\nport := 8080\nb\n",
			args:    fsEditArgs{OldString: "port := 8080", NewString: "port := 9090"},
			want:    "a\nport := 9090\nb\n",
			wantN:   1,
		},
		{
			name:    "replace_all rewrites every occurrence",
			content: "x x x",
			args:    fsEditArgs{OldString: "x", NewString: "y", ReplaceAll: true},
			want:    "y y y",
			wantN:   3,
		},
		{
			name:    "ambiguous match is refused",
			content: "x x",
			args:    fsEditArgs{OldString: "x", NewString: "y"},
			wantErr: "not unique",
		},
		{
			name:    "absent match is refused",
			content: "abc",
			args:    fsEditArgs{OldString: "zzz", NewString: "y"},
			wantErr: "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, err := applyExactEdit(tt.content, tt.args, "/workspace/f.txt")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				// A refused edit must yield nothing writable: the box path passes this
				// straight to Router.WriteFile.
				if got != "" || n != 0 {
					t.Errorf("refused edit returned content %q / count %d", got, n)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyExactEdit: %v", err)
			}
			if got != tt.want || n != tt.wantN {
				t.Errorf("= %q/%d, want %q/%d", got, n, tt.want, tt.wantN)
			}
		})
	}
}

func TestBoxRootOrDefault(t *testing.T) {
	// A HOST workspace root must never leak into a box path — inside the box the workspace is
	// always mounted at /workspace.
	for _, tt := range []struct{ in, want string }{
		{"", boxWorkspaceRoot},
		{"   ", boxWorkspaceRoot},
		{"src", "/workspace/src"},
		{"./src/", "/workspace/src"},
		{"/etc", "/etc"},
		{"/workspace/deep/", "/workspace/deep"},
	} {
		if got := boxRootOrDefault(tt.in); got != tt.want {
			t.Errorf("boxRootOrDefault(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBoxSkippedPathPrunesDirectoriesNotFiles(t *testing.T) {
	skipped := []string{
		"node_modules/pkg/index.js",
		"vendor/x/y.go",
		"__pycache__/m.pyc",
		".git/config",
		"src/.hidden/f.txt",
	}
	for _, rel := range skipped {
		if !boxSkippedPath(rel) {
			t.Errorf("boxSkippedPath(%q) = false, want it pruned", rel)
		}
	}
	// skipWalkDir is a DIRECTORY rule: the host walk never drops a file for its own name, so a
	// dotfile or a file literally named node_modules must still be searchable.
	kept := []string{
		"src/main.go",
		".gitignore",
		"src/vendor.go",
		"node_modules",
	}
	for _, rel := range kept {
		if boxSkippedPath(rel) {
			t.Errorf("boxSkippedPath(%q) = true, want it kept", rel)
		}
	}
}

func TestParseBoxFileFrames(t *testing.T) {
	frame := func(pairs ...string) []byte {
		var b strings.Builder
		for i := 0; i+1 < len(pairs); i += 2 {
			b.WriteString("\x00" + pairs[i] + "\x00" + pairs[i+1])
		}
		return []byte(b.String())
	}

	files, truncated := parseBoxFileFrames(frame("./a.go", "package a\n", "./sub/b.go", "package b\n"), 100)
	if truncated {
		t.Error("complete stream reported truncated")
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	// "./" is stripped so the paths match what the host branch reports (filepath.Rel output).
	if files[0].Rel != "a.go" || files[1].Rel != "sub/b.go" {
		t.Errorf("paths = %q/%q, want a.go/sub/b.go", files[0].Rel, files[1].Rel)
	}
	if string(files[1].Content) != "package b\n" {
		t.Errorf("content = %q", files[1].Content)
	}

	// Content containing newlines and spaces must not be re-split — only NUL frames.
	files, _ = parseBoxFileFrames(frame("./x.txt", "one\ntwo three\n\nfour"), 100)
	if len(files) != 1 || string(files[0].Content) != "one\ntwo three\n\nfour" {
		t.Errorf("multiline content mangled: %#v", files)
	}

	// Pruned directories are dropped during decode, not surfaced to the caller.
	files, _ = parseBoxFileFrames(frame("./node_modules/p/i.js", "x", "./keep.go", "y"), 100)
	if len(files) != 1 || files[0].Rel != "keep.go" {
		t.Errorf("skip rules not applied during decode: %#v", files)
	}

	// The node cap stops the decode and says so.
	files, truncated = parseBoxFileFrames(frame("./a", "1", "./b", "2", "./c", "3"), 2)
	if len(files) != 2 || !truncated {
		t.Errorf("cap: files = %d truncated = %v, want 2/true", len(files), truncated)
	}

	// A stream cut mid-content (total budget hit) drops the partial file rather than reporting
	// it as fully read.
	files, truncated = parseBoxFileFrames([]byte("\x00./a.go\x00full\x00./b.go"), 100)
	if len(files) != 1 || files[0].Rel != "a.go" {
		t.Errorf("partial tail not dropped: %#v", files)
	}
	if !truncated {
		t.Error("stream cut mid-frame did not report truncation")
	}

	if files, truncated = parseBoxFileFrames(nil, 100); len(files) != 0 || truncated {
		t.Errorf("empty sweep = %#v/%v, want none/false", files, truncated)
	}
}

func TestGrepContentIsSharedByBothPaths(t *testing.T) {
	re := regexp.MustCompile(`po(rt|ol)`)
	var out []string
	grepContent(bytes.NewReader([]byte("no\n  port := 8080  \npool := 4\nnope\n")), "sub/app.go", re, 10, &out)

	want := []string{"sub/app.go:2: port := 8080", "sub/app.go:3: pool := 4"}
	if len(out) != len(want) {
		t.Fatalf("matches = %#v, want %#v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %q, want %q", i, out[i], want[i])
		}
	}

	// maxResults is a hard stop, so the box sweep cannot flood a turn.
	out = nil
	grepContent(bytes.NewReader([]byte("a\na\na\na\n")), "f", regexp.MustCompile("a"), 2, &out)
	if len(out) != 2 {
		t.Errorf("maxResults ignored: %#v", out)
	}
}
