package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// The routed branches of fs_edit / fs_glob / fs_grep run only under a strict profile with a
// live box, and the docker_integration tier that exercises them contributes ZERO coverage
// (CLAUDE.md): it is absent from the gate's tag matrix, so every one of those statements
// counts as uncovered no matter how green that tier is.
//
// They do not need a daemon, though. usersandbox.Backend is an exported five-verb interface
// and SandboxRouter.Exec delegates straight to it, so a fake backend puts the whole routed
// path under test — and gives a SHARPER test than the container one: canned Exec output pins
// the exact command composed and the exact parsing of what comes back, which a live box can
// only observe end-to-end.

// fakeBox is a usersandbox.Backend plus the structural fileWriter capability the router's
// WriteFile needs. It records every exec and answers from a scripted responder.
type fakeBox struct {
	execs    []usersandbox.ExecRequest
	respond  func(cmd string) usersandbox.ExecResult
	written  map[string]string
	resolveE error
	execE    error
	writeE   error
}

func (f *fakeBox) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	if f.resolveE != nil {
		return usersandbox.BoxHandle{}, f.resolveE
	}
	return usersandbox.BoxHandle{ContainerID: "box-1", IdentityID: "id-1"}, nil
}

func (f *fakeBox) Exec(_ context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	f.execs = append(f.execs, req)
	if f.execE != nil {
		return usersandbox.ExecResult{}, f.execE
	}
	if f.respond == nil {
		return usersandbox.ExecResult{}, nil
	}
	return f.respond(req.Command), nil
}

func (f *fakeBox) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (f *fakeBox) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (f *fakeBox) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

// CopyFileIn satisfies the router's optional fileWriter capability (fs_edit's write-back).
func (f *fakeBox) CopyFileIn(_ context.Context, _ usersandbox.BoxHandle, boxPath string, content []byte, _ int64) error {
	if f.writeE != nil {
		return f.writeE
	}
	f.record(boxPath, content)
	return nil
}

// CopyFileInStream satisfies the router's optional fileStreamWriter capability (document_open's
// copy-in). It reproduces the real writer's declared-length contract — tar frames the entry with a
// size fixed before the body is read, so a source of any other length must FAIL rather than land a
// short file — because a fake that accepted any length would let a caller pass the wrong size and
// still go green.
func (f *fakeBox) CopyFileInStream(
	_ context.Context, _ usersandbox.BoxHandle, boxPath string, size int64, src io.Reader, _ int64,
) error {
	if f.writeE != nil {
		return f.writeE
	}
	content, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	if int64(len(content)) != size {
		return fmt.Errorf("source delivered %d bytes, not the declared %d", len(content), size)
	}
	f.record(boxPath, content)
	return nil
}

func (f *fakeBox) record(boxPath string, content []byte) {
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[boxPath] = string(content)
}

func routerWith(be usersandbox.Backend) *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(be, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: "aura-sandbox:latest", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128, IdleTTLSec: 1800,
	})
}

// boxSweepPrintf lifts the FORMAT operand out of the printf the sweep composes. The format is a
// double-quoted word carrying no double quote of its own, so the first quoted run after `printf `
// is all of it.
var boxSweepPrintf = regexp.MustCompile(`printf "([^"]*)"`)

// shPrintfEscapes applies POSIX printf(1) escape processing to a format operand: a backslash
// followed by up to THREE octal digits is ONE byte, taken greedily. Only octal escapes are modelled
// — they are the only kind the sweep format uses — and any other escape is a fatal surprise rather
// than a silent guess.
//
// Emulating the box instead of re-deriving the separator in Go is the entire point of this helper.
// A harness that pulls the token out of the command and rebuilds "\x00"+token+"\x00" computes the
// separator the PARSER expects, so it can never witness the producer emitting a different one:
// every test standing on it is self-consistent by construction, and blind to exactly the desync it
// claims to pin. Escape handling reproduced byte-for-byte against dash (the box shell), busybox
// ash, bash and GNU coreutils printf.
func shPrintfEscapes(t *testing.T, format string) string {
	t.Helper()
	octal := func(i int) bool { return i < len(format) && format[i] >= '0' && format[i] <= '7' }
	var b strings.Builder
	for i := 0; i < len(format); {
		if format[i] != '\\' {
			b.WriteByte(format[i])
			i++
			continue
		}
		if i++; !octal(i) {
			t.Fatalf("sweep printf format uses a non-octal escape this emulator does not model: %q", format)
		}
		var val byte
		for digits := 0; digits < 3 && octal(i); digits++ {
			val = val*8 + (format[i] - '0')
			i++
		}
		b.WriteByte(val)
	}
	return b.String()
}

// boxFrames renders the sweep output boxReadFiles parses, by running the box's printf semantics
// over the format the producer ACTUALLY composed. pairs are name/content, names relative to root
// unless already absolute (a single-file root is emitted under its own absolute path).
//
// Emulating rather than re-deriving is what lets these daemon-free tests observe a producer/parser
// separator disagreement at all — see shPrintfEscapes. The separator is the seam where a binary
// file used to shift every following frame onto the wrong path.
func boxFrames(t *testing.T, cmd, root string, pairs ...string) []byte {
	t.Helper()
	m := boxSweepPrintf.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("sweep command carries no printf format: %q", cmd)
	}
	before, after, ok := strings.Cut(m[1], "%s")
	if !ok {
		t.Fatalf("sweep printf format has no %%s conversion: %q", m[1])
	}
	prefix, suffix := shPrintfEscapes(t, before), shPrintfEscapes(t, after)
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		name := pairs[i]
		if !strings.HasPrefix(name, "/") {
			name = strings.TrimSuffix(root, "/") + "/" + name
		}
		b.WriteString(prefix + name + suffix + pairs[i+1])
	}
	return []byte(b.String())
}

func boxCtx(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

func TestSearchFilesContentRoutedSweepsTheBoxAndParsesFrames(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot,
			"sub/app.go", "alpha\nport := 8080\n",
			"node_modules/pkg/i.js", "port := 9999\n", // pruned during decode
			"readme.md", "no match here\n",
		)}
	}}
	res, err := (&SearchFiles{Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"port := \\d+"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "sub/app.go:2: port := 8080") {
		t.Errorf("match not reported with box-relative path + line: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "node_modules") {
		t.Errorf("pruned directory leaked into results: %q", res.Preview)
	}
	// The sweep targets the box workspace, which is the only root there is.
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "'/workspace'") {
		t.Fatalf("sweep command = %+v, want one exec rooted at /workspace", be.execs)
	}
	// The PATTERN must never be handed to the box — matching stays on Go's RE2, so handing the
	// pattern to the box's grep can never quietly swap the regexp dialect underneath the tool.
	if strings.Contains(be.execs[0].Command, "port :=") {
		t.Errorf("pattern was pushed into the box shell: %q", be.execs[0].Command)
	}
}

func TestSearchFilesContentRoutedReportsNoMatches(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot, "a.txt", "nothing relevant\n")}
	}}
	res, err := (&SearchFiles{Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"ZZZ"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "[no matches]") {
		t.Errorf("preview = %q, want the no-matches marker", res.Preview)
	}
}

// A binary file in the tree must not shift the frames that follow it onto the wrong path. The
// sweep used to be plain NUL-delimited, so a .png's interior NULs injected extra frames and — on
// an odd count — every following file's real matches were reported under another file's path. That
// is the two-filesystems document_open defect in miniature: content reported where it does not
// live. The per-call token makes the desync unrepresentable.
func TestSearchFilesContentFramingSurvivesBinaryContent(t *testing.T) {
	binary := "\x00\x89PNG\x00\x1a\x00" // an ODD number of NULs: the shape that shifted every later pair
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot,
			"img/logo.png", binary,
			"src/app.go", "port := 8080\n",
		)}
	}}
	res, err := (&SearchFiles{Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"port := 8080"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "src/app.go:1: port := 8080") {
		t.Fatalf("a binary file mis-attributed the following match: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "logo.png") {
		t.Errorf("binary file leaked into results: %q", res.Preview)
	}
}

// The appliance incident, as a command-shape assertion: the sweep must prune hidden and vendored
// directories INSIDE find, before the node budget counts them. Filtering after enumeration is what
// let /root/.cache's ~66k files exhaust the cap and return "[no matches]" for files that plainly
// existed. -mindepth 1 is part of the guarantee — without it an explicitly-targeted hidden root
// prunes itself away.
func TestBoxSweepsPruneAtTheProducer(t *testing.T) {
	sweeps := map[string]func(*fakeBox) error{
		"target=files": func(be *fakeBox) error {
			_, err := (&SearchFiles{Router: routerWith(be)}).
				Execute(boxCtx(t), json.RawMessage(`{"pattern":"**/*.go","target":"files"}`))
			return err
		},
		"target=content": func(be *fakeBox) error {
			_, err := (&SearchFiles{Router: routerWith(be)}).
				Execute(boxCtx(t), json.RawMessage(`{"pattern":"needle"}`))
			return err
		},
	}
	for tool, run := range sweeps {
		t.Run(tool, func(t *testing.T) {
			be := boxRespondingWith("")
			if err := run(be); err != nil {
				t.Fatalf("%s: %v", tool, err)
			}
			if len(be.execs) != 1 {
				t.Fatalf("%s: want one sweep exec, got %#v", tool, be.execs)
			}
			cmd := be.execs[0].Command
			for _, want := range []string{"-mindepth 1", "-name '.*'", "-name 'node_modules'", "-name 'vendor'", "-name '__pycache__'", "-type d -prune"} {
				if !strings.Contains(cmd, want) {
					t.Errorf("%s sweep is missing %q — a hidden subtree would burn the node budget: %q", tool, want, cmd)
				}
			}
		})
	}
}

// fs_grep's spec accepts a FILE as `path`. find -mindepth 1 prints nothing for a file start point,
// so the sweep carries a separate single-file arm; without it the tool answered "[no matches]" for
// a file it could read perfectly well.
func TestSearchFilesContentSearchesASingleFileRoot(t *testing.T) {
	const target = "/workspace/notes.txt"
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if !strings.Contains(cmd, "[ -f '"+target+"' ]") {
			t.Errorf("sweep has no single-file arm: %q", cmd)
		}
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, target, target, "alpha\nneedle here\n")}
	}}
	res, err := (&SearchFiles{Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"needle","path":"`+target+`"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	// A file handed in AS the root reports under its basename.
	if !strings.Contains(res.Preview, "notes.txt:2: needle here") {
		t.Fatalf("single-file root not searched: %q", res.Preview)
	}
}

func TestSearchFilesNamesListsBoxTreeAndPrunes(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		// The sweep is `find -printf '%T@ %p\n' | sort -rn`, so each line is "<mtime> <path>":
		// results come back newest-first, which is the ordering the tool promises.
		return usersandbox.ExecResult{Stdout: []byte(strings.Join([]string{
			"1786000004.0 /workspace/main.go",
			"1786000003.0 /workspace/sub/app.go",
			"1786000002.0 /workspace/vendor/dep/x.go", // pruned
			"1786000001.0 /workspace/notes.md",        // pattern miss
		}, "\n") + "\n")}
	}}
	res, err := (&SearchFiles{Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"**/*.go","target":"files"}`))
	if err != nil {
		t.Fatalf("routed fs_glob: %v", err)
	}
	for _, want := range []string{"main.go", "sub/app.go"} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("missing %q in %q", want, res.Preview)
		}
	}
	if strings.Contains(res.Preview, "vendor") {
		t.Errorf("vendor not pruned: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "notes.md") {
		t.Errorf("pattern miss listed: %q", res.Preview)
	}
}

func TestPatchRoutedReadsBoxAppliesEditAndWritesBack(t *testing.T) {
	var be *fakeBox
	be = &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		// patch re-hashes the file after writing it, so the fake has to answer BOTH execs:
		// the bounded read, and the verification that the bytes actually persisted.
		if strings.HasPrefix(cmd, "sha256sum ") {
			sum := sha256.Sum256([]byte(be.written["/workspace/app.go"]))
			return usersandbox.ExecResult{Stdout: []byte(hex.EncodeToString(sum[:]) + "  /workspace/app.go\n")}
		}
		if !strings.HasPrefix(cmd, "head -c ") {
			t.Errorf("patch must read through the bounded head -c exec, got %q", cmd)
		}
		return usersandbox.ExecResult{Stdout: []byte("port := 8080\nname := \"aura\"\n")}
	}}
	res, err := (&Patch{Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/workspace/app.go","old_string":"port := 8080","new_string":"port := 9090"}`))
	if err != nil {
		t.Fatalf("routed patch: %v", err)
	}
	// patch reports a unified diff, so the result itself shows both sides of the edit.
	if !strings.Contains(res.Preview, "port := 9090") || !strings.Contains(res.Preview, "port := 8080") {
		t.Errorf("result should be a diff showing the replacement, got %q", res.Preview)
	}
	got, ok := be.written["/workspace/app.go"]
	if !ok {
		t.Fatalf("nothing written back into the box: %+v", be.written)
	}
	if !strings.Contains(got, "port := 9090") || strings.Contains(got, "port := 8080") {
		t.Errorf("box write = %q, want the replacement applied", got)
	}
}

func TestDurableArtifactAcceptedCapture(t *testing.T) {
	t.Run("write_file", func(t *testing.T) {
		var be *fakeBox
		be = &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
			if strings.HasPrefix(cmd, "sha256sum ") {
				sum := sha256.Sum256([]byte(be.written["/workspace/report.md"]))
				return usersandbox.ExecResult{Stdout: []byte(hex.EncodeToString(sum[:]) + "  /workspace/report.md\n")}
			}
			return usersandbox.ExecResult{}
		}}
		res, err := (&WriteFile{Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
			`{"path":"/workspace/report.md","content":"durable"}`))
		if err != nil {
			t.Fatalf("write_file: %v", err)
		}
		assertDurableArtifactEvidence(t, res, "/workspace/report.md", "write")
	})

	t.Run("patch", func(t *testing.T) {
		var be *fakeBox
		be = &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
			if strings.HasPrefix(cmd, "sha256sum ") {
				sum := sha256.Sum256([]byte(be.written["/workspace/report.md"]))
				return usersandbox.ExecResult{Stdout: []byte(hex.EncodeToString(sum[:]) + "  /workspace/report.md\n")}
			}
			return usersandbox.ExecResult{Stdout: []byte("before\n")}
		}}
		res, err := (&Patch{Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
			`{"mode":"replace","path":"/workspace/report.md","old_string":"before","new_string":"after"}`))
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		assertDurableArtifactEvidence(t, res, "/workspace/report.md", "patch")
	})
}

func assertDurableArtifactEvidence(t *testing.T, res ToolResult, path, operation string) {
	t.Helper()
	if res.Meta == nil {
		t.Fatal("successful durable filesystem mutation has nil Meta")
	}
	evidence, ok := (*res.Meta)[MetaDurableArtifact].(DurableArtifactEvidence)
	if !ok {
		t.Fatalf("durable artifact evidence = %#v, want typed DurableArtifactEvidence", (*res.Meta)[MetaDurableArtifact])
	}
	if evidence.Path != path || evidence.Operation != operation || evidence.ActorRunID == "" || evidence.ActorRole == "" {
		t.Fatalf("durable artifact evidence = %+v", evidence)
	}
}

func TestPatchRoutedRefusesTheSkillsMount(t *testing.T) {
	be := &fakeBox{}
	_, err := (&Patch{Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/skills/calc/calc.py","old_string":"a","new_string":"b"}`))
	if err == nil || !strings.Contains(err.Error(), "skills") {
		t.Fatalf("err = %v, want a refusal naming the skills mount", err)
	}
	if len(be.execs) != 0 {
		t.Errorf("the fence must refuse BEFORE touching the box, got %+v", be.execs)
	}
}

// A box failure must DENY, never silently fall back to the host (D-09/GATE-01). Each routed
// tool is checked at both seams that can fail: resolving the box, and the exec/write itself.
func TestRoutedFileToolsFailClosedOnBoxFailure(t *testing.T) {
	deny := func(t *testing.T, res ToolResult, err error, tool string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: a box failure is a DENY result, not a Go error: %v", tool, err)
		}
		var payload map[string]string
		if uerr := json.Unmarshal([]byte(res.Preview), &payload); uerr != nil {
			t.Fatalf("%s: deny preview is not json: %q", tool, res.Preview)
		}
		if payload["error"] != "sandbox_unavailable" || payload["tool"] != tool {
			t.Fatalf("%s: payload = %+v, want sandbox_unavailable for this tool", tool, payload)
		}
		if !strings.Contains(payload["message"], "NOT run on the host") {
			t.Errorf("%s: the deny must say the host was not used: %q", tool, payload["message"])
		}
	}

	for _, tc := range []struct {
		name string
		be   *fakeBox
	}{
		{"box cannot be resolved", &fakeBox{resolveE: fmt.Errorf("dockerd down")}},
		{"exec fails inside the box", &fakeBox{execE: fmt.Errorf("exec refused")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := routerWith(tc.be)
			ctx := boxCtx(t)

			res, err := (&SearchFiles{Router: r}).Execute(ctx, json.RawMessage(`{"pattern":"needle"}`))
			deny(t, res, err, "search_files")

			res, err = (&SearchFiles{Router: r}).Execute(ctx, json.RawMessage(`{"pattern":"**/*.go","target":"files"}`))
			deny(t, res, err, "search_files")

			res, err = (&Patch{Router: r}).Execute(ctx, json.RawMessage(
				`{"path":"/workspace/app.go","old_string":"a","new_string":"b"}`))
			deny(t, res, err, "patch")
		})
	}

	// The write-back seam: the read succeeds, the box refuses the write.
	be := &fakeBox{
		respond: func(string) usersandbox.ExecResult {
			return usersandbox.ExecResult{Stdout: []byte("a\n")}
		},
		writeE: fmt.Errorf("copy refused"),
	}
	res, err := (&Patch{Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/workspace/app.go","old_string":"a","new_string":"b"}`))
	deny(t, res, err, "patch")
}
