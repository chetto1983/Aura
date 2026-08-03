package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// The daemon-free half of the fs_* tools: the routing decision with no box, the shared
// match/scan/framing logic, and the box path helpers. The container-gated half lives under
// docker_integration and contributes ZERO coverage, so anything testable without a daemon has to
// be tested here or it is not tested at all (CLAUDE.md).

// THE CONTAINMENT INVARIANT, in executable form. This replaces TestUnroutedFSToolsStillRunOnTheHost,
// which pinned the exact opposite: a router-less fs_edit used to rewrite a HOST file. That arm is
// gone, so a tool with no reachable box must DENY — and must leave a seeded host tree byte-for-byte
// untouched while doing it. If a future change re-introduces a host fallback, this is what trips.
func TestFSToolsDenyWhenTheBoxIsUnreachable(t *testing.T) {
	ctx := WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)

	denies := func(t *testing.T, tool, preview string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: an unreachable box is a DENY result, not a Go error: %v", tool, err)
		}
		var payload map[string]string
		if uerr := json.Unmarshal([]byte(preview), &payload); uerr != nil {
			t.Fatalf("%s: deny preview is not json: %q", tool, preview)
		}
		if payload["error"] != "sandbox_unavailable" || payload["tool"] != tool {
			t.Fatalf("%s: payload = %+v, want sandbox_unavailable for this tool", tool, payload)
		}
	}

	// Both shapes of "no box": a tool built with no Router at all (the pool-free CLI/manifest
	// wiring) and a router whose backend cannot resolve one.
	for _, tc := range []struct {
		name   string
		router func() *usersandbox.SandboxRouter
	}{
		{"nil router", func() *usersandbox.SandboxRouter { return nil }},
		{"backend cannot resolve", func() *usersandbox.SandboxRouter {
			return routerWith(&fakeBox{resolveE: errors.New("dockerd down")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "app.go")
			const original = "port := 8080\nname := \"aura\"\n"
			if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}

			res, err := (&FSRead{Router: tc.router()}).Execute(ctx, json.RawMessage(
				`{"path":`+strconv.Quote(target)+`}`))
			denies(t, "fs_read", res.Preview, err)

			res, err = (&FSWrite{Router: tc.router()}).Execute(ctx, json.RawMessage(
				`{"path":`+strconv.Quote(filepath.Join(dir, "new.txt"))+`,"content":"LEAKED"}`))
			denies(t, "fs_write", res.Preview, err)

			res, err = (&FSEdit{Router: tc.router()}).Execute(ctx, json.RawMessage(
				`{"path":`+strconv.Quote(target)+`,"old_string":"port := 8080","new_string":"port := 9090"}`))
			denies(t, "fs_edit", res.Preview, err)

			res, err = (&FSGlob{Router: tc.router()}).Execute(ctx, json.RawMessage(
				`{"pattern":"**/*.go","path":`+strconv.Quote(dir)+`}`))
			denies(t, "fs_glob", res.Preview, err)

			res, err = (&FSGrep{Router: tc.router()}).Execute(ctx, json.RawMessage(
				`{"pattern":"8080","path":`+strconv.Quote(dir)+`}`))
			denies(t, "fs_grep", res.Preview, err)

			after, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != original {
				t.Fatalf("a denied fs_edit mutated the HOST file — containment breached: %q", after)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("a denied fs_write created host files: %d entries, want only the seeded one", len(entries))
			}
		})
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

func TestBoxPathArg(t *testing.T) {
	// A HOST workspace root must never leak into a box path — inside the box the workspace is
	// always mounted at /workspace. A relative path resolves here, explicitly, rather than
	// depending on the Docker backend happening to set /workspace as the container WORKDIR.
	for _, tt := range []struct{ in, want string }{
		{"", boxWorkspaceRoot},
		{"   ", boxWorkspaceRoot},
		{"src", "/workspace/src"},
		{"./src/", "/workspace/src"},
		{"/etc", "/etc"},
		{"/workspace/deep/", "/workspace/deep"},
		{"~user/x", "/workspace/~user/x"}, // a named home was literal on the host arm too
	} {
		got, err := boxPathArg("fs_read", tt.in)
		if err != nil {
			t.Errorf("boxPathArg(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("boxPathArg(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	// "~" is REFUSED, not resolved. The host arm expanded it against the aura container's home;
	// taking it literally would put the file at /workspace/~/notes.txt — real, and where nobody
	// looks. Neither write path can expand it either (tar copy-in runs no shell).
	for _, in := range []string{"~", "~/notes.txt", "  ~/deep/x  "} {
		if _, err := boxPathArg("fs_write", in); err == nil {
			t.Errorf("boxPathArg(%q) = nil error, want a refusal naming /workspace", in)
		}
	}
}

// The five tools must refuse "~" identically, and refuse it BEFORE the route — it is the model's
// own argument error, not a sandbox outage, and nothing may reach the box.
func TestFSToolsRefuseTildePaths(t *testing.T) {
	ctx := WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
	for _, tc := range []struct{ tool, args string }{
		{"fs_read", `{"path":"~/notes.txt"}`},
		{"fs_write", `{"path":"~/notes.txt","content":"x"}`},
		{"fs_edit", `{"path":"~/notes.txt","old_string":"a","new_string":"b"}`},
		{"fs_glob", `{"pattern":"*.go","path":"~/src"}`},
		{"fs_grep", `{"pattern":"needle","path":"~/src"}`},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			be := &fakeBox{}
			r := routerWith(be)
			tools := map[string]Tool{
				"fs_read": &FSRead{Router: r}, "fs_write": &FSWrite{Router: r},
				"fs_edit": &FSEdit{Router: r}, "fs_glob": &FSGlob{Router: r}, "fs_grep": &FSGrep{Router: r},
			}
			_, err := tools[tc.tool].Execute(ctx, json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), "/workspace") {
				t.Fatalf("err = %v, want a refusal pointing at /workspace", err)
			}
			if len(be.execs) != 0 || len(be.written) != 0 {
				t.Errorf("the refusal must land before the box: execs=%#v writes=%#v", be.execs, be.written)
			}
		})
	}
}

func TestBoxRelPath(t *testing.T) {
	for _, tt := range []struct {
		root, abs, want string
		ok              bool
	}{
		{"/workspace", "/workspace/sub/a.go", "sub/a.go", true},
		{"/workspace/", "/workspace/a.go", "a.go", true},
		// A file handed in AS the root reports under its basename (the host walk said ".").
		// Only the sweep's single-file arm can produce this: find -type f never prints a
		// directory start point, so a directory root never reaches it.
		{"/workspace/notes.txt", "/workspace/notes.txt", "notes.txt", true},
		// Anything outside the sweep root is dropped rather than reported at a made-up path.
		{"/workspace", "/etc/passwd", "", false},
		{"/workspace", "/workspacex/a.go", "", false},
	} {
		got, ok := boxRelPath(tt.root, tt.abs)
		if ok != tt.ok || got != tt.want {
			t.Errorf("boxRelPath(%q, %q) = %q/%v, want %q/%v", tt.root, tt.abs, got, ok, tt.want, tt.ok)
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
	const sep = "\x00TESTTOKEN\x00"
	frame := func(pairs ...string) []byte {
		var b strings.Builder
		for i := 0; i+1 < len(pairs); i += 2 {
			b.WriteString(sep + boxWorkspaceRoot + "/" + pairs[i] + sep + pairs[i+1])
		}
		return []byte(b.String())
	}
	parse := func(stdout []byte, nodeCap int) ([]boxFile, bool) {
		return parseBoxFileFrames(stdout, sep, boxWorkspaceRoot, nodeCap)
	}

	files, truncated := parse(frame("a.go", "package a\n", "sub/b.go", "package b\n"), 100)
	if truncated {
		t.Error("complete stream reported truncated")
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	// Names arrive absolute and are re-based on the sweep root, matching what the deleted host
	// branch reported (filepath.Rel output).
	if files[0].Rel != "a.go" || files[1].Rel != "sub/b.go" {
		t.Errorf("paths = %q/%q, want a.go/sub/b.go", files[0].Rel, files[1].Rel)
	}
	if string(files[1].Content) != "package b\n" {
		t.Errorf("content = %q", files[1].Content)
	}

	// Content containing newlines and spaces must not be re-split — only whole separators.
	files, _ = parse(frame("x.txt", "one\ntwo three\n\nfour"), 100)
	if len(files) != 1 || string(files[0].Content) != "one\ntwo three\n\nfour" {
		t.Errorf("multiline content mangled: %#v", files)
	}

	// THE FRAMING INVARIANT: a binary file's interior NULs must not shift the pairs that follow it.
	// Under the old bare-NUL delimiter an odd NUL count re-paired every later frame, so a real text
	// match was reported under another file's path.
	files, _ = parse(frame("logo.png", "\x00\x89PNG\x00\x1a\x00", "app.go", "port := 8080\n"), 100)
	if len(files) != 2 || files[1].Rel != "app.go" || string(files[1].Content) != "port := 8080\n" {
		t.Errorf("binary content desynced the frames: %#v", files)
	}

	// Pruned directories are dropped during decode too (the producer prunes first; this is the
	// second line of defence for a Backend whose find lacks the predicate).
	files, _ = parse(frame("node_modules/p/i.js", "x", "keep.go", "y"), 100)
	if len(files) != 1 || files[0].Rel != "keep.go" {
		t.Errorf("skip rules not applied during decode: %#v", files)
	}

	// The node cap stops the decode and says so.
	files, truncated = parse(frame("a", "1", "b", "2", "c", "3"), 2)
	if len(files) != 2 || !truncated {
		t.Errorf("cap: files = %d truncated = %v, want 2/true", len(files), truncated)
	}

	// A stream cut mid-content (total budget hit) drops the partial file rather than reporting
	// it as fully read.
	files, truncated = parse([]byte(sep+"/workspace/a.go"+sep+"full"+sep+"/workspace/b.go"), 100)
	if len(files) != 1 || files[0].Rel != "a.go" {
		t.Errorf("partial tail not dropped: %#v", files)
	}
	if !truncated {
		t.Error("stream cut mid-frame did not report truncation")
	}

	// A single-file sweep root: the one frame names the root itself and reports as its basename.
	files, _ = parseBoxFileFrames([]byte(sep+"/workspace/notes.txt"+sep+"body\n"), sep, "/workspace/notes.txt", 100)
	if len(files) != 1 || files[0].Rel != "notes.txt" {
		t.Errorf("single-file root decoded as %#v", files)
	}

	if files, truncated = parse(nil, 100); len(files) != 0 || truncated {
		t.Errorf("empty sweep = %#v/%v, want none/false", files, truncated)
	}
}

// The framing token must be fresh per call and free of shell metacharacters: it is pasted into a
// printf FORMAT string inside the box, and a token that could collide across calls (or contain a %
// or a quote) would put the desync it prevents straight back. Being metacharacter-free is NOT the
// same as being safe next to a backslash escape — that half of the contract belongs to
// boxFrameEmitter and is pinned by TestBoxFrameEmitterAndParserAgreeOnEveryToken.
func TestBoxFrameSeparatorIsFreshAndSafe(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		token, sep := boxFrameSeparator()
		if seen[token] {
			t.Fatalf("frame token repeated across calls: %q", token)
		}
		seen[token] = true
		if sep != "\x00"+token+"\x00" {
			t.Fatalf("separator %q does not wrap the token in NULs", sep)
		}
		if strings.ContainsAny(token, "%'\"\\ $`\x00") || token == "" {
			t.Fatalf("token %q is not safe inside a single-quoted printf format", token)
		}
	}
}

// The bytes the box EMITS must be the bytes parseBoxFileFrames splits on, for every token
// crypto/rand.Text can draw — not merely for the 26 symbols in 32 that begin with a letter.
// rand.Text draws from the RFC 4648 base32 alphabet, so 6 of its 32 symbols are digits, and a digit
// landing first is precisely what an under-specified "\0" octal escape swallows (see
// boxFrameEmitter). Driving every possible leading symbol turns that into a deterministic assertion
// instead of an 18.75%-per-call flake.
//
// The producer side is EMULATED through the box's printf semantics, never re-derived from the
// token: a harness that rebuilds the parser's own separator cannot fail this way, which is how the
// defect survived a suite that looked like it covered the framing.
func TestBoxFrameEmitterAndParserAgreeOnEveryToken(t *testing.T) {
	const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // crypto/rand.Text's alphabet
	for _, lead := range base32Alphabet {
		token := string(lead) + "MZQVWXKH"
		t.Run(token, func(t *testing.T) {
			stdout := boxFrames(t, boxFrameEmitter(token, 4096), boxWorkspaceRoot,
				"a.go", "package a\n", "sub/b.go", "package b\n")
			// The parser's half of the contract, restated: sep wraps the token in NULs
			// (TestBoxFrameSeparatorIsFreshAndSafe pins that boxFrameSeparator builds exactly this).
			files, truncated := parseBoxFileFrames(stdout, "\x00"+token+"\x00", boxWorkspaceRoot, 100)
			if truncated {
				t.Error("complete stream reported truncated")
			}
			if len(files) != 2 {
				t.Fatalf("decoded %d files from the box's own bytes, want 2 — the separator the box "+
					"EMITS is not the one the parser splits on", len(files))
			}
			if files[0].Rel != "a.go" || string(files[0].Content) != "package a\n" ||
				files[1].Rel != "sub/b.go" || string(files[1].Content) != "package b\n" {
				t.Errorf("frames decoded as %#v", files)
			}
		})
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
