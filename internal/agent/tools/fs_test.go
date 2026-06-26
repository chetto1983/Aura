package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFSReadReturnsContentAndWindow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("l1\nl2\nl3\nl4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-fs", "call-fs")
	tool := &FSRead{}

	res, err := tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: p}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(res.Preview, "l1") || !strings.Contains(res.Preview, "l4") {
		t.Fatalf("full read missing lines: %q", res.Preview)
	}

	res, err = tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: p, Offset: 2, Limit: 2}))
	if err != nil {
		t.Fatalf("windowed read: %v", err)
	}
	if strings.Contains(res.Preview, "l1") || !strings.Contains(res.Preview, "l2") || !strings.Contains(res.Preview, "l3") || strings.Contains(res.Preview, "l4") {
		t.Fatalf("window [2,2) wrong: %q", res.Preview)
	}
}

func TestFSReadRejectsBinaryContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.xlsx")
	if err := os.WriteFile(p, []byte{'P', 'K', 0x03, 0x04, 0x00, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-fs-binary", "call-fs-binary")
	tool := &FSRead{}

	if _, err := tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: p})); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("fs_read(binary): err = %v, want binary-file rejection", err)
	}
}

func TestFSWriteCreatesFileAndDirs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "nested", "out.txt")
	ctx := ctxWith(t, "sess-fs", "call-fs")

	if _, err := (&FSWrite{}).Execute(ctx, mustJSON(t, fsWriteArgs{Path: p, Content: "hello"})); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "hello" {
		t.Fatalf("file not written: got %q err %v", got, err)
	}
}

func TestFSWriteAtomicOverwriteAndMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	ctx := ctxWith(t, "sess-fs-atomic", "call-fs")
	tool := &FSWrite{}

	// Atomic create: no leftover temp marker, content correct, 0o644 mode.
	if _, err := tool.Execute(ctx, mustJSON(t, fsWriteArgs{Path: p, Content: "first"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "first" {
		t.Fatalf("create content: got %q err %v", got, err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not honor Unix permission bits (Stat reports 0666), so the
	// exact-mode assertion only holds on POSIX.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o644 {
		t.Fatalf("create mode = %o, want 0644", perm)
	}

	// Overwrite an existing file replaces it fully (no append) and keeps mode.
	if _, err := tool.Execute(ctx, mustJSON(t, fsWriteArgs{Path: p, Content: "second-longer"})); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err = os.ReadFile(p)
	if err != nil || string(got) != "second-longer" {
		t.Fatalf("overwrite content: got %q err %v", got, err)
	}
	info, err = os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o644 {
		t.Fatalf("overwrite mode = %o, want 0644", perm)
	}

	// The atomic temp file must not linger in the target directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".aura-tmp-") {
			t.Fatalf("leftover temp file after atomic write: %s", e.Name())
		}
	}
}

func TestFSEditUniqueAndAmbiguous(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	if err := os.WriteFile(p, []byte("a := 1\nb := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-fs", "call-fs")
	tool := &FSEdit{}

	// Ambiguous: "1" appears twice → error without replace_all.
	if _, err := tool.Execute(ctx, mustJSON(t, fsEditArgs{Path: p, OldString: "1", NewString: "2"})); err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("want not-unique error, got %v", err)
	}
	// Unique edit.
	if _, err := tool.Execute(ctx, mustJSON(t, fsEditArgs{Path: p, OldString: "a := 1", NewString: "a := 9"})); err != nil {
		t.Fatalf("unique edit: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "a := 9\nb := 1\n" {
		t.Fatalf("edit wrong: %q", got)
	}
	// Not found.
	if _, err := tool.Execute(ctx, mustJSON(t, fsEditArgs{Path: p, OldString: "zzz", NewString: "y"})); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

func TestFSEditRejectsEmptyOldString(t *testing.T) {
	dir := t.TempDir()
	ctx := ctxWith(t, "sess-fs-empty-old", "call-fs")
	tool := &FSEdit{}

	tests := []struct {
		name       string
		content    string
		replaceAll bool
	}{
		{name: "non-empty-file", content: "abc\n"},
		{name: "non-empty-file-replace-all", content: "abc\n", replaceAll: true},
		{name: "empty-file", content: ""},
		{name: "empty-file-replace-all", content: "", replaceAll: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, tt.name+".txt")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := tool.Execute(ctx, mustJSON(t, fsEditArgs{
				Path:       p,
				OldString:  "",
				NewString:  "x",
				ReplaceAll: tt.replaceAll,
			}))
			if err == nil || !strings.Contains(err.Error(), "old_string must be non-empty") {
				t.Fatalf("err = %v, want empty old_string rejection", err)
			}
			got, readErr := os.ReadFile(p)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tt.content {
				t.Fatalf("file mutated after rejected empty old_string: got %q want %q", got, tt.content)
			}
		})
	}
}

func TestFSGrepFindsMatches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package x\nfunc Target() {}\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "nothing here\n")
	ctx := ctxWith(t, "sess-fs", "call-fs")

	res, err := (&FSGrep{}).Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "func Tar", Path: dir, Glob: "*.go"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(res.Preview, "a.go:2:") || !strings.Contains(res.Preview, "Target") {
		t.Fatalf("grep missing hit: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "b.txt") {
		t.Fatalf("glob filter leaked non-go file: %q", res.Preview)
	}
}

func TestFSGlobMatchesRecursive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "cmd", "app", "main.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "readme.md"), "# hi\n")
	ctx := ctxWith(t, "sess-fs", "call-fs")

	res, err := (&FSGlob{}).Execute(ctx, mustJSON(t, fsGlobArgs{Pattern: "**/*.go", Path: dir}))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(res.Preview, "cmd/app/main.go") {
		t.Fatalf("glob missing nested match: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "readme.md") {
		t.Fatalf("glob matched non-go file: %q", res.Preview)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
