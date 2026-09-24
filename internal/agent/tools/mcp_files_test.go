package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	pathpkg "path"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// mcpTurnCtx is a context the way LlmAgent.Run leaves it: a request id and a cleanup.
func mcpTurnCtx(t *testing.T) (context.Context, *TurnCleanup) {
	t.Helper()
	return WithTurnCleanup(WithRequestID(t.Context(), "req-1"))
}

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// listWritten answers the `ls -1A -- '<dir>'` a sink's listing command ends with, from what
// the box holds, one name a line, the way the real box would. Any other command gets an
// empty answer.
func (f *fakeBox) listWritten(cmd string) usersandbox.ExecResult {
	_, dir, ok := strings.Cut(cmd, "ls -1A -- '")
	if !ok {
		return usersandbox.ExecResult{}
	}
	dir = strings.TrimSuffix(dir, "' 2>/dev/null")
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for p := range f.written {
		if pathpkg.Dir(p) == dir {
			names = append(names, pathpkg.Base(p))
		}
	}
	return usersandbox.ExecResult{Stdout: []byte(strings.Join(names, "\n"))}
}

func TestMCPFileSinkWritesEachFileWhereTheTurnRemovesIt(t *testing.T) {
	be := &fakeBox{}
	ctx, cleanup := mcpTurnCtx(t)
	pdf, png := []byte("%PDF-1.7"), []byte("\x89PNG")

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "Fattura settembre è.pdf", MIMEType: "application/pdf", Data: pdf},
		{MIMEType: "image/png", Data: png},
	})

	want := []mcp.FileOutcome{
		{Path: "/workspace/mcp-files/req-1/aura-pim/Fattura settembre è.pdf", Name: "Fattura settembre è.pdf", MIMEType: "application/pdf", SizeBytes: 8, SHA256: hexSum(pdf)},
		{Path: "/workspace/mcp-files/req-1/aura-pim/file.png", Name: "file.png", MIMEType: "image/png", SizeBytes: 4, SHA256: hexSum(png)},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("outcomes = %+v\nwant %+v", out, want)
	}
	if wantWritten := map[string]string{want[0].Path: string(pdf), want[1].Path: string(png)}; !reflect.DeepEqual(be.written, wantWritten) {
		t.Fatalf("written = %v, want %v", be.written, wantWritten)
	}
	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if last := be.execs[len(be.execs)-1].Command; last != "rm -rf -- '/workspace/mcp-files/req-1'" {
		t.Fatalf("turn cleanup ran %q", last)
	}
}

// An unclean exit (an updater restart, an OOM kill) skips the turn's own removal, and nothing
// else removes a turn directory, so the exec that lists a call's directory first sweeps the
// stale ones. The turn's own directory is never swept, however long the turn has run.
func TestMCPFileSinkSweepsTheTurnDirectoriesAnUncleanExitLeftBehind(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	(&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "aura-pim", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	want := "find '/workspace/mcp-files' -mindepth 1 -maxdepth 1 -type d -mmin +1440 ! -path '/workspace/mcp-files/req-1' -exec rm -rf -- {} + 2>/dev/null; " +
		"ls -1A -- '/workspace/mcp-files/req-1/aura-pim' 2>/dev/null"
	if len(be.execs) == 0 || be.execs[0].Command != want {
		t.Fatalf("the listing exec = %v\nwant [%q]", be.execs, want)
	}
}

func TestMCPFileSinkNeverOverwritesAFileOfTheSameName(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if strings.Contains(cmd, "ls -1A ") {
			return usersandbox.ExecResult{Stdout: []byte("report.pdf\nreport-2.pdf\n")}
		}
		return usersandbox.ExecResult{}
	}}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "report.pdf", MIMEType: "application/pdf", Data: []byte("a")},
		{Name: "image001.png", MIMEType: "image/png", Data: []byte("b")},
		{Name: "image001.png", MIMEType: "image/png", Data: []byte("c")},
	})

	got := []string{out[0].Name, out[1].Name, out[2].Name}
	if want := []string{"report-3.pdf", "image001.png", "image001-2.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	dir := "/workspace/mcp-files/req-1/aura-pim/"
	wantWritten := map[string]string{dir + "report-3.pdf": "a", dir + "image001.png": "b", dir + "image001-2.png": "c"}
	if !reflect.DeepEqual(be.written, wantWritten) {
		t.Fatalf("written = %v, want %v", be.written, wantWritten)
	}
}

func TestMCPFileSinkConcurrentCallsOfATurnNeverShareAName(t *testing.T) {
	const calls = 8
	be := &fakeBox{}
	be.respond = func(cmd string) usersandbox.ExecResult {
		res := be.listWritten(cmd)
		// The listing-to-write window a second call of the turn can land in: without the
		// sink's lock all eight calls are inside it at once.
		time.Sleep(5 * time.Millisecond)
		return res
	}
	ctx, _ := mcpTurnCtx(t)
	sink := &MCPFileSink{Router: routerWith(be)}

	outcomes := make([]mcp.FileOutcome, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Go(func() {
			part := mcp.FilePart{Name: "image001.png", MIMEType: "image/png", Data: fmt.Appendf(nil, "attachment %d", i)}
			outcomes[i] = sink.Materialize(ctx, "aura-pim", []mcp.FilePart{part})[0]
		})
	}
	wg.Wait()

	paths := map[string]bool{}
	for i, o := range outcomes {
		if got, want := be.written[o.Path], fmt.Sprintf("attachment %d", i); o.Path == "" || got != want {
			t.Errorf("call %d: %q holds %q, want %q (outcome %+v)", i, o.Path, got, want, o)
		}
		paths[o.Path] = true
	}
	if len(paths) != calls || len(be.written) != calls {
		t.Fatalf("%d calls got %d distinct paths and left %d files in the box: %v", calls, len(paths), len(be.written), be.written)
	}
}

var mcpFileNameCases = []struct{ name, mime, want string }{
	{"Relazione finale è.docx", "", "Relazione finale è.docx"},
	{"../../.bashrc", "", "bashrc"},
	{`C:\fakepath\Contratto.xlsx`, "", "Contratto.xlsx"},
	{"a\x00b\nc.txt", "", "abc.txt"},
	{"..", "image/jpeg", "file.jpg"},
	{"", "image/png", "file.png"},
	{"scan", "application/pdf", "scan.pdf"},
	{"note", "text/plain; charset=utf-8", "note.txt"},
	{"blob", "application/x-unknown", "blob"},
	{". .pdf", "", "pdf"},
	{" .env", "", "env"},
	{"notes.txt  ", "", "notes.txt"},
	// An extension longer than maxMCPExtensionBytes is part of the name, so a cut takes it too.
	{strings.Repeat("a", 250) + "." + strings.Repeat("b", 20), "", strings.Repeat("a", 200)},
	// A cut that lands after a space must not leave it at the end of the stem, before the
	// extension or as the last character of a name without one.
	{strings.Repeat("a", 195) + " " + strings.Repeat("b", 20) + ".pdf", "", strings.Repeat("a", 195) + ".pdf"},
	{strings.Repeat("a", 199) + " " + strings.Repeat("b", 10), "", strings.Repeat("a", 199)},
}

func TestMCPFileNameIsOneSafeComponent(t *testing.T) {
	for _, c := range mcpFileNameCases {
		got := mcpFileName(c.name, c.mime)
		if got != c.want {
			t.Errorf("mcpFileName(%q, %q) = %q, want %q", c.name, c.mime, got, c.want)
		}
		if err := documents.ValidateStagedFileName(got); err != nil {
			t.Errorf("mcpFileName(%q) = %q, which the document_open rule refuses: %v", c.name, got, err)
		}
	}
	long := mcpFileName(strings.Repeat("à", 150)+".pdf", "application/pdf")
	if len(long) > maxMCPFileNameBytes || !strings.HasSuffix(long, ".pdf") || !utf8.ValidString(long) {
		t.Fatalf("a 304-byte name became %d bytes %q", len(long), long)
	}
}

// FuzzMCPFileNameIsAStagedFileName holds mcpFileName to the rule it claims to follow:
// whatever a server names a file, the result is one name document_open would accept.
func FuzzMCPFileNameIsAStagedFileName(f *testing.F) {
	for _, c := range mcpFileNameCases {
		f.Add(c.name, c.mime)
	}
	f.Add(strings.Repeat("à", 150)+".pdf", "application/pdf")
	f.Fuzz(func(t *testing.T, name, mimeType string) {
		got := mcpFileName(name, mimeType)
		if err := documents.ValidateStagedFileName(got); err != nil {
			t.Fatalf("mcpFileName(%q, %q) = %q, which the document_open rule refuses: %v", name, mimeType, got, err)
		}
		if got == "" || strings.ContainsAny(got, `/\`) || strings.ContainsFunc(got, unicode.IsControl) ||
			len(got) > maxMCPFileNameBytes || !utf8.ValidString(got) {
			t.Fatalf("mcpFileName(%q, %q) = %q is not one short printable component", name, mimeType, got)
		}
	})
}

func TestMCPPathSegment(t *testing.T) {
	for in, want := range map[string]string{"aura-pim": "aura-pim", "../evil": "_evil", "": "_", "è": "_"} {
		if got := mcpPathSegment(in); got != want {
			t.Errorf("mcpPathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMCPFileSinkRefusesAFileOverTheCapAndKeepsTheRest(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{
		{Name: "huge.bin", Data: make([]byte, mcp.MaxFileBytes+1)},
		{Name: "small.txt", MIMEType: "text/plain", Data: []byte("ok")},
	})

	if out[0].NotMaterialized != "26214401 bytes exceeds the 26214400-byte file cap" || out[0].Path != "" {
		t.Fatalf("oversized outcome = %+v", out[0])
	}
	if out[1].Path != "/workspace/mcp-files/req-1/s/small.txt" {
		t.Fatalf("the small file must still be written: %+v", out[1])
	}
}

// A file the sink refuses on its own for being over the file cap is not in the total the
// call cap is checked against, however large it is: the call's other files still go in.
// The bridge stops reading links by this same arithmetic (mcp.FilePart.CallBytes).
func TestMCPFileSinkDoesNotCountAFileItRefusedAloneTowardTheCallCap(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{
		{Name: "huge.bin", Data: make([]byte, mcp.MaxCallFileBytes+1)},
		{Name: "small.txt", MIMEType: "text/plain", Data: []byte("ok")},
	})

	if out[0].NotMaterialized != mcp.FileCapExceeded(mcp.MaxCallFileBytes+1) || out[0].Path != "" {
		t.Fatalf("oversized outcome = %+v", out[0])
	}
	if out[1].Path != "/workspace/mcp-files/req-1/s/small.txt" {
		t.Fatalf("the small file must still be written, not refused for the call cap: %+v", out[1])
	}
}

func TestMCPFileSinkRefusesACallOverTheCapWithoutTouchingTheBox(t *testing.T) {
	be := &fakeBox{}
	ctx, cleanup := mcpTurnCtx(t)
	twenty := make([]byte, 20<<20)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{
		{Name: "a", Data: twenty}, {Name: "b", Data: twenty}, {Name: "c", Data: twenty},
	})

	for _, o := range out {
		if o.NotMaterialized != "the call's files exceed the 52428800-byte cap" {
			t.Fatalf("outcome = %+v", o)
		}
	}
	if err := cleanup.Run(context.Background()); err != nil || len(be.execs) != 0 || len(be.written) != 0 {
		t.Fatalf("a refused call touched the box: execs=%v written=%v err=%v", be.execs, be.written, err)
	}
}

func TestMCPFileSinkWithoutATurnWritesNothing(t *testing.T) {
	be := &fakeBox{}
	sink := &MCPFileSink{Router: routerWith(be)}
	for _, ctx := range []context.Context{t.Context(), WithRequestID(t.Context(), "req-1")} {
		out := sink.Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})
		if out[0].NotMaterialized != "no agent turn owns the file" {
			t.Fatalf("outcome = %+v", out[0])
		}
	}
	if len(be.execs) != 0 || len(be.written) != 0 {
		t.Fatalf("no turn, yet the box was touched: %v %v", be.execs, be.written)
	}
}

// routeCountingBox counts the Route calls (Backend.Resolve) made through it.
type routeCountingBox struct {
	*fakeBox
	resolves atomic.Int32
}

func (b *routeCountingBox) Resolve(ctx context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	b.resolves.Add(1)
	return b.fakeBox.Resolve(ctx, spec)
}

// A turn that wrote nothing makes no box call (spec §Turn-end cleanup): a call with no
// part, or whose parts are all refused on their own, neither routes to the box nor
// registers a removal for the turn's drain.
func TestMCPFileSinkMakesNoBoxCallWhenNoPartIsToBeWritten(t *testing.T) {
	be := &routeCountingBox{fakeBox: &fakeBox{}}
	ctx, cleanup := mcpTurnCtx(t)
	sink := &MCPFileSink{Router: routerWith(be)}

	sink.Materialize(ctx, "s", nil)
	sink.Materialize(ctx, "s", []mcp.FilePart{
		{Name: "huge.bin", Data: make([]byte, mcp.MaxFileBytes+1)},
		{Name: "old.pdf", Unavailable: "read failed: attachment expired"},
	})

	if err := cleanup.Run(context.Background()); err != nil || be.resolves.Load() != 0 || len(be.execs) != 0 || len(be.written) != 0 {
		t.Fatalf("nothing was to be written, yet the box was called: routes=%d execs=%v written=%v err=%v", be.resolves.Load(), be.execs, be.written, err)
	}

	// The counters do see a call that writes.
	sink.Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})
	if be.resolves.Load() != 1 || len(be.written) != 1 {
		t.Fatalf("a writing call made %d routes and %d writes, want 1 and 1", be.resolves.Load(), len(be.written))
	}
}

func TestMCPFileSinkDeniesWhenTheBoxIsUnreachable(t *testing.T) {
	be := &fakeBox{resolveE: errors.New("daemon down")}
	ctx, cleanup := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	if !strings.HasPrefix(out[0].NotMaterialized, "sandbox unavailable: ") || !strings.Contains(out[0].NotMaterialized, "daemon down") {
		t.Fatalf("outcome = %+v", out[0])
	}
	if err := cleanup.Run(context.Background()); err != nil || len(be.execs) != 0 {
		t.Fatalf("nothing was written, so nothing is removed: %v %v", be.execs, err)
	}
}

func TestMCPFileSinkKeepsTheTurnDirectoryRegisteredWhenTheListingFails(t *testing.T) {
	be := &fakeBox{execE: errors.New("exec down")}
	ctx, cleanup := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	if out[0].NotMaterialized != "sandbox unavailable: exec down" {
		t.Fatalf("outcome = %+v", out[0])
	}
	err := cleanup.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "remove /workspace/mcp-files/req-1: exec down") {
		t.Fatalf("cleanup error = %v, want the removal to be attempted and to say why it failed", err)
	}
}

func TestMCPFileSinkRemovesAPartialFileAndSaysTheWriteFailed(t *testing.T) {
	be := &fakeBox{writeE: errors.New("disk full")}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.pdf", Data: []byte("x")}})

	if !strings.HasPrefix(out[0].NotMaterialized, "write failed: ") || !strings.Contains(out[0].NotMaterialized, "disk full") {
		t.Fatalf("outcome = %+v", out[0])
	}
	if last := be.execs[len(be.execs)-1].Command; last != "rm -f -- '/workspace/mcp-files/req-1/s/a.pdf'" {
		t.Fatalf("partial file not removed; last exec %q", last)
	}
}

func TestMCPFileSinkPassesAnUnreadableLinkThroughWithoutTouchingTheBox(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "old.pdf", Unavailable: "read failed: attachment expired"}})

	if out[0].NotMaterialized != "read failed: attachment expired" || len(be.execs) != 0 {
		t.Fatalf("outcome = %+v, execs = %v", out[0], be.execs)
	}
}

func TestMCPFileSinkCleanupReportsAFailedRemoval(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if strings.HasPrefix(cmd, "rm -rf") {
			return usersandbox.ExecResult{ExitCode: 1, Stderr: []byte("busy")}
		}
		return usersandbox.ExecResult{}
	}}
	ctx, cleanup := mcpTurnCtx(t)
	(&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	err := cleanup.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "/workspace/mcp-files/req-1") || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("cleanup error = %v, want the directory and the box's reason", err)
	}
}
