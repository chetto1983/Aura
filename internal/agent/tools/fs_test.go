package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// These pin the fs_* tools' USER-VISIBLE behaviour — line windows, binary rejection, the exact
// edit rules, glob/grep matching — over the fakeBox harness rather than a host temp tree. The
// tools have one filesystem now (the caller's box), so a canned box response is what a real call
// sees; it also pins the exact command composed, which a live container could only observe
// end-to-end. The docker_integration tier that runs a real box contributes ZERO coverage
// (CLAUDE.md), so anything provable without a daemon has to be proved here.

// boxRespondingWith answers every exec with stdout and records the calls.
func boxRespondingWith(stdout string) *fakeBox {
	return &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: []byte(stdout)}
	}}
}

func TestFSReadReturnsContentAndWindow(t *testing.T) {
	be := boxRespondingWith("l1\nl2\nl3\nl4\n")
	tool := &FSRead{Router: routerWith(be)}
	ctx := ctxWith(t, "sess-fs", "call-fs")

	res, err := tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: "/workspace/f.txt"}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(res.Preview, "l1") || !strings.Contains(res.Preview, "l4") {
		t.Fatalf("full read missing lines: %q", res.Preview)
	}
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "'/workspace/f.txt'") {
		t.Fatalf("read must go through a bounded box exec on the box path: %#v", be.execs)
	}

	res, err = tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: "/workspace/f.txt", Offset: 2, Limit: 2}))
	if err != nil {
		t.Fatalf("windowed read: %v", err)
	}
	if strings.Contains(res.Preview, "l1") || !strings.Contains(res.Preview, "l2") || !strings.Contains(res.Preview, "l3") || strings.Contains(res.Preview, "l4") {
		t.Fatalf("window [2,2) wrong: %q", res.Preview)
	}
}

// A relative path resolves against the box workspace, not against whatever directory the box
// image happens to set as its WORKDIR. That default is a Docker-backend detail no other Backend
// owes, and it is the only resolution rule left.
func TestFSReadResolvesRelativePathsAgainstBoxWorkspace(t *testing.T) {
	be := boxRespondingWith("body\n")
	if _, err := (&FSRead{Router: routerWith(be)}).
		Execute(ctxWith(t, "sess-rel", "call-rel"), mustJSON(t, fsReadArgs{Path: "sub/f.txt"})); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "'/workspace/sub/f.txt'") {
		t.Fatalf("relative path not resolved under /workspace: %#v", be.execs)
	}
}

func TestFSReadRejectsBinaryContent(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: []byte{'P', 'K', 0x03, 0x04, 0x00, 'x'}}
	}}
	tool := &FSRead{Router: routerWith(be)}
	ctx := ctxWith(t, "sess-fs-binary", "call-fs-binary")

	if _, err := tool.Execute(ctx, mustJSON(t, fsReadArgs{Path: "/workspace/book.xlsx"})); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("fs_read(binary): err = %v, want binary-file rejection", err)
	}
}

// A non-zero exit from the box read (missing file, directory, unreadable) surfaces as a clean
// tool error, not as a sandbox outage — the model can self-correct on the first and not the second.
func TestFSReadMissingFileIsAToolErrorNotAnOutage(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{ExitCode: 1, Stderr: []byte("head: cannot open '/workspace/nope': No such file")}
	}}
	_, err := (&FSRead{Router: routerWith(be)}).
		Execute(ctxWith(t, "sess-miss", "call-miss"), mustJSON(t, fsReadArgs{Path: "/workspace/nope"}))
	if err == nil || !strings.Contains(err.Error(), "No such file") {
		t.Fatalf("err = %v, want the box's own read error surfaced", err)
	}
}

func TestFSWriteCreatesFile(t *testing.T) {
	be := &fakeBox{}
	ctx := ctxWith(t, "sess-fs", "call-fs")

	if _, err := (&FSWrite{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsWriteArgs{Path: "sub/nested/out.txt", Content: "hello"})); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Parent directories are the daemon's job on copy-in; the tool's contract is the path.
	if got, ok := be.written["/workspace/sub/nested/out.txt"]; !ok || got != "hello" {
		t.Fatalf("file not written into the box: %#v", be.written)
	}
}

func TestFSEditUniqueAndAmbiguous(t *testing.T) {
	ctx := ctxWith(t, "sess-fs", "call-fs")
	const original = "a := 1\nb := 1\n"

	// Ambiguous: "1" appears twice → error without replace_all, and nothing is written back.
	be := boxRespondingWith(original)
	_, err := (&FSEdit{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsEditArgs{Path: "/workspace/f.go", OldString: "1", NewString: "2"}))
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("want not-unique error, got %v", err)
	}
	if len(be.written) != 0 {
		t.Fatalf("a refused edit must write nothing: %#v", be.written)
	}

	// Unique edit.
	be = boxRespondingWith(original)
	if _, err := (&FSEdit{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsEditArgs{Path: "/workspace/f.go", OldString: "a := 1", NewString: "a := 9"})); err != nil {
		t.Fatalf("unique edit: %v", err)
	}
	if got := be.written["/workspace/f.go"]; got != "a := 9\nb := 1\n" {
		t.Fatalf("edit wrong: %q", got)
	}

	// Not found.
	be = boxRespondingWith(original)
	_, err = (&FSEdit{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsEditArgs{Path: "/workspace/f.go", OldString: "zzz", NewString: "y"}))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
	if len(be.written) != 0 {
		t.Fatalf("a refused edit must write nothing: %#v", be.written)
	}
}

// The empty-old_string guard runs BEFORE the route, so it never reaches the box at all — no exec,
// no write, and the same error whether or not a box is available.
func TestFSEditRejectsEmptyOldString(t *testing.T) {
	ctx := ctxWith(t, "sess-fs-empty-old", "call-fs")

	for _, replaceAll := range []bool{false, true} {
		be := boxRespondingWith("abc\n")
		_, err := (&FSEdit{Router: routerWith(be)}).Execute(ctx, mustJSON(t, fsEditArgs{
			Path:       "/workspace/f.txt",
			OldString:  "",
			NewString:  "x",
			ReplaceAll: replaceAll,
		}))
		if err == nil || !strings.Contains(err.Error(), "old_string must be non-empty") {
			t.Fatalf("replace_all=%v: err = %v, want empty old_string rejection", replaceAll, err)
		}
		if len(be.execs) != 0 || len(be.written) != 0 {
			t.Fatalf("replace_all=%v: the guard must refuse before touching the box: execs=%#v writes=%#v",
				replaceAll, be.execs, be.written)
		}
	}
}

func TestFSGrepFindsMatches(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot,
			"a.go", "package x\nfunc Target() {}\n",
			"b.txt", "nothing here\n",
		)}
	}}
	ctx := ctxWith(t, "sess-fs", "call-fs")

	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "func Tar", Glob: "*.go"}))
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
	be := boxRespondingWith("/workspace/cmd/app/main.go\n/workspace/readme.md\n")
	ctx := ctxWith(t, "sess-fs", "call-fs")

	res, err := (&FSGlob{Router: routerWith(be)}).Execute(ctx, mustJSON(t, fsGlobArgs{Pattern: "**/*.go"}))
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
