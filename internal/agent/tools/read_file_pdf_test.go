package tools

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pdftext"
)

const sheetPath = "/workspace/mcp-files/r1/mail/sheet.pdf"

var pdfFile = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog >>\nendobj\n\x00stream")

// pdfinfoAnswers stands in for pdfinfo, whose own reading is pinned in internal/pdftext.
func pdfinfoAnswers(t *testing.T, info pdftext.Info, err error) {
	t.Helper()
	original := inspectPDF
	t.Cleanup(func() { inspectPDF = original })
	inspectPDF = func(context.Context, []byte) (pdftext.Info, error) { return info, err }
}

// What each route does with the part is pinned in its client: a document block on Claude
// (internal/llm/anthropic media_test.go), reference text on openai_compat and ChatGPT.
func TestReadFileAttachesAPDFForTheModel(t *testing.T) {
	pdfinfoAnswers(t, pdftext.Info{Pages: maxPDFAttachPages}, nil)
	ctx, media := turnCtx(t)
	res, err := readFile(t, ctx, boxServing(t, pdfFile), sheetPath)
	if err != nil {
		t.Fatalf("read_file on a PDF: %v", err)
	}
	want := sheetPath + " is a PDF document (" + strconv.Itoa(maxPDFAttachPages) + " pages, " + strconv.Itoa(len(pdfFile)) +
		" bytes) and is attached below for you to read. If it is not shown to you, " + pdfWaysOut + "."
	if res.Preview != want {
		t.Fatalf("result = %q\nwant     %q", res.Preview, want)
	}
	parts := media.Snapshot()["call-img"]
	if len(parts) != 1 || parts[0].MIMEType != "application/pdf" || parts[0].Text != sheetPath || !bytes.Equal(parts[0].Bytes, pdfFile) {
		t.Fatalf("registered parts = %+v, want the PDF as read", parts)
	}
}

func TestPDFPageBoundFollowsTheDocumentedWorstCase(t *testing.T) {
	if maxPDFAttachPages != 10 || maxPDFAttachPages*pdfWorstPageTokens > pdfWindowTokens/pdfWindowShare {
		t.Fatalf("%d pages of %d tokens against a %d-token share", maxPDFAttachPages, pdfWorstPageTokens, pdfWindowTokens/pdfWindowShare)
	}
}

// A PDF larger than the text read cap is read again, up to the attach cap.
func TestReadFileReadsAPDFPastTheTextCap(t *testing.T) {
	pdfinfoAnswers(t, pdftext.Info{Pages: 1}, nil)
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

func TestReadFileDoesNotAttachWhatItCannotBound(t *testing.T) {
	oversize := append(append([]byte(nil), pdfFile...), make([]byte, maxPDFAttachBytes)...)
	for name, tc := range map[string]struct {
		data []byte
		info pdftext.Info
		err  error
		want string
	}{
		"over the byte cap": {oversize, pdftext.Info{Pages: 1}, nil, strconv.Itoa(maxPDFAttachBytes) + "-byte cap"},
		"over the page cap": {pdfFile, pdftext.Info{Pages: maxPDFAttachPages + 1}, nil, "PDF of " + strconv.Itoa(maxPDFAttachPages+1) + " pages"},
		"pages not counted": {pdfFile, pdftext.Info{}, errors.New("pdfinfo: exit status 1"), "cannot be counted"},
		"an encrypted file": {pdfFile, pdftext.Info{Pages: 1, Encrypted: true}, nil, "encrypted PDF"},
	} {
		t.Run(name, func(t *testing.T) {
			pdfinfoAnswers(t, tc.info, tc.err)
			ctx, media := turnCtx(t)
			_, err := readFile(t, ctx, boxServing(t, tc.data), sheetPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), pdfWaysOut) {
				t.Fatalf("err = %v, want %q and the ways out", err, tc.want)
			}
			if media.Snapshot() != nil {
				t.Fatalf("a refused PDF registered media: %+v", media.Snapshot())
			}
		})
	}
}

func TestReadFilePDFCountsAgainstTheTurnMediaCap(t *testing.T) {
	pdfinfoAnswers(t, pdftext.Info{Pages: 1}, nil)
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
