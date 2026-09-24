package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
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
	if be.written[want[1].Path] != string(png) {
		t.Fatalf("written = %v", be.written)
	}
	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if last := be.execs[len(be.execs)-1].Command; last != "rm -rf -- '/workspace/mcp-files/req-1'" {
		t.Fatalf("turn cleanup ran %q", last)
	}
}

func TestMCPFileSinkNeverOverwritesAFileOfTheSameName(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if strings.HasPrefix(cmd, "ls ") {
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
}

func TestMCPFileNameIsOneSafeComponent(t *testing.T) {
	cases := []struct{ name, mime, want string }{
		{"Relazione finale è.docx", "", "Relazione finale è.docx"},
		{"../../.bashrc", "", "bashrc"},
		{`C:\fakepath\Contratto.xlsx`, "", "Contratto.xlsx"},
		{"a\x00b\nc.txt", "", "abc.txt"},
		{"..", "image/jpeg", "file.jpg"},
		{"", "image/png", "file.png"},
		{"scan", "application/pdf", "scan.pdf"},
		{"note", "text/plain; charset=utf-8", "note.txt"},
		{"blob", "application/x-unknown", "blob"},
	}
	for _, c := range cases {
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
