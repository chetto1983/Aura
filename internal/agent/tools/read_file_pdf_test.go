package tools

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

const sheetPath = "/workspace/mcp-files/r1/mail/sheet.pdf"

var pdfFile = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog >>\nendobj\n\x00stream")

// What each route does with the part is pinned in its client: a document block on Claude
// (internal/llm/anthropic media_test.go), reference text on openai_compat and ChatGPT.
func TestReadFileAttachesAPDFForTheModel(t *testing.T) {
	ctx, media := turnCtx(t)
	res, err := readFile(t, ctx, boxServing(t, pdfFile), sheetPath)
	if err != nil {
		t.Fatalf("read_file on a PDF: %v", err)
	}
	if !strings.HasPrefix(res.Preview, sheetPath+" is a PDF document ("+strconv.Itoa(len(pdfFile))+" bytes) and is attached below") ||
		!strings.Contains(res.Preview, "pdftotext") || !strings.Contains(res.Preview, "send_file") {
		t.Fatalf("result = %q, want the attachment and the ways out when it is not shown", res.Preview)
	}
	parts := media.Snapshot()["call-img"]
	if len(parts) != 1 || parts[0].MIMEType != "application/pdf" || parts[0].Text != sheetPath || !bytes.Equal(parts[0].Bytes, pdfFile) {
		t.Fatalf("registered parts = %+v, want the PDF as read", parts)
	}
}

// A PDF larger than the text read cap is read again, up to the attach cap.
func TestReadFileReadsAPDFPastTheTextCap(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "16")
	ctx, media := turnCtx(t)
	be := boxServing(t, pdfFile)
	if _, err := readFile(t, ctx, be, sheetPath); err != nil {
		t.Fatalf("read_file on a PDF over the text cap: %v", err)
	}
	if len(be.execs) != 2 || !strings.HasPrefix(be.execs[1].Command, "head -c "+strconv.Itoa(maxPDFAttachBytes+1)+" ") {
		t.Fatalf("execs = %+v, want the text-cap read then one read bounded at the attach cap", be.execs)
	}
	if got := media.Snapshot()["call-img"]; len(got) != 1 || !bytes.Equal(got[0].Bytes, pdfFile) {
		t.Fatalf("attached %d parts, want the whole PDF", len(got))
	}
}

func TestReadFileDoesNotAttachAnOversizePDF(t *testing.T) {
	ctx, media := turnCtx(t)
	oversize := append(append([]byte(nil), pdfFile...), make([]byte, maxPDFAttachBytes)...)
	_, err := readFile(t, ctx, boxServing(t, oversize), sheetPath)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(maxPDFAttachBytes)+"-byte cap") || !strings.Contains(err.Error(), "pdftotext") {
		t.Fatalf("err = %v, want the cap and the ways out", err)
	}
	if media.Snapshot() != nil {
		t.Fatalf("an oversize PDF registered media: %+v", media.Snapshot())
	}
}

func TestReadFilePDFCountsAgainstTheTurnMediaCap(t *testing.T) {
	ctx, media := turnCtx(t)
	for i := range llm.MaxToolMediaPerTurn {
		if err := media.Add("earlier-"+strconv.Itoa(i), llm.ProjectedRequestPart{MIMEType: "image/png", Bytes: []byte("png")}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := readFile(t, ctx, boxServing(t, pdfFile), sheetPath)
	if !errors.Is(err, llm.ErrToolMediaFull) {
		t.Fatalf("err = %v, want the turn's media cap", err)
	}
	if got := media.Snapshot()["call-img"]; got != nil {
		t.Fatalf("a PDF over the turn cap was attached: %+v", got)
	}
}
