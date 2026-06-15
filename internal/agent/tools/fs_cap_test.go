package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AG-014/D-05: fs_read of a file over AURA_FS_MAX_READ_BYTES returns a clean
// error with a paging hint and never loads the whole file into memory.
func TestFSReadOverCapReturnsPagingHint(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "64")
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(p, []byte(strings.Repeat("a\n", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-cap", "call-cap")

	_, err := (&FSRead{}).Execute(ctx, mustJSON(t, fsReadArgs{Path: p}))
	if err == nil {
		t.Fatal("fs_read over cap err = nil, want clean over-cap error")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "offset") || !strings.Contains(low, "limit") {
		t.Fatalf("over-cap error must suggest offset/limit paging: %v", err)
	}
}

// Under-cap reads are unaffected.
func TestFSReadUnderCapUnaffected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "1048576")
	dir := t.TempDir()
	p := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-cap2", "call-cap2")
	res, err := (&FSRead{}).Execute(ctx, mustJSON(t, fsReadArgs{Path: p}))
	if err != nil {
		t.Fatalf("under-cap read: %v", err)
	}
	if !strings.Contains(res.Preview, "hello") {
		t.Fatalf("under-cap read content lost: %q", res.Preview)
	}
}

// AG-014: fs_write over the cap is rejected before writing.
func TestFSWriteOverCapRejected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "16")
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	ctx := ctxWith(t, "sess-cap-w", "call-cap-w")
	_, err := (&FSWrite{}).Execute(ctx, mustJSON(t, fsWriteArgs{Path: p, Content: strings.Repeat("x", 64)}))
	if err == nil {
		t.Fatal("fs_write over cap err = nil, want rejection")
	}
	if _, statErr := os.Stat(p); statErr == nil {
		t.Fatal("fs_write over cap still created the file; want no write")
	}
}

// AG-014: fs_edit on a file over the cap is rejected before loading it.
func TestFSEditOverCapRejected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "32")
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(p, []byte(strings.Repeat("abcd", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-cap-e", "call-cap-e")
	_, err := (&FSEdit{}).Execute(ctx, mustJSON(t, fsEditArgs{Path: p, OldString: "abcd", NewString: "wxyz"}))
	if err == nil {
		t.Fatal("fs_edit over cap err = nil, want rejection")
	}
}

// AG-045: fs_edit writes atomically (temp + rename) — after a successful edit the
// file content is fully replaced with no partial/truncated intermediate.
func TestFSEditAtomicReplacesContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "code.go")
	if err := os.WriteFile(p, []byte("port := 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-atomic", "call-atomic")
	if _, err := (&FSEdit{}).Execute(ctx, mustJSON(t, fsEditArgs{Path: p, OldString: "8080", NewString: "9090"})); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read after edit: %v", err)
	}
	if string(got) != "port := 9090\n" {
		t.Fatalf("edit content = %q, want port := 9090", string(got))
	}
	// No temp turds left behind in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".aura-tmp") {
			t.Fatalf("atomic edit left a temp file: %s", e.Name())
		}
	}
}

func TestFSMaxReadBytesDefault(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "")
	if got := fsMaxReadBytes(); got != defaultFSMaxReadBytes {
		t.Fatalf("default fs cap = %d, want %d (10 MiB)", got, defaultFSMaxReadBytes)
	}
}
