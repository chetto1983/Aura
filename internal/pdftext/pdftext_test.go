package pdftext

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pdfWithPages is a hand-written PDF with one page per text. Its cross-reference
// table is deliberately absent: poppler rebuilds one by scanning, which is also what
// makes it a fair stand-in for the damaged files real sources serve.
func pdfWithPages(texts ...string) string {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n")
	kids := make([]string, len(texts))
	for i := range texts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	fmt.Fprintf(&b, "2 0 obj<</Type/Pages/Kids[%s]/Count %d>>endobj\n", strings.Join(kids, " "), len(texts))
	b.WriteString("3 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n")
	for i, text := range texts {
		stream := "BT /F1 12 Tf 20 100 Td (" + text + ") Tj ET\n"
		fmt.Fprintf(&b, "%d 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 200]"+
			"/Resources<</Font<</F1 3 0 R>>>>/Contents %d 0 R>>endobj\n", 4+2*i, 5+2*i)
		fmt.Fprintf(&b, "%d 0 obj<</Length %d>>stream\n%sendstream\nendobj\n", 5+2*i, len(stream), stream)
	}
	fmt.Fprintf(&b, "trailer<</Root 1 0 R/Size %d>>\n%%%%EOF\n", 4+2*len(texts))
	return b.String()
}

func writePDF(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireExtractor keeps a missing extractor from passing for a test whose whole
// subject is the extractor. Locally it skips; in CI it fails, because a job that
// silently exercises nothing is the falsely-green case the test discipline bans
// (the CI workflows install poppler-utils for exactly this).
func requireExtractor(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(Tool); err == nil {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatalf("%s is not installed and $CI is set: the extraction path would run nowhere", Tool)
	}
	t.Skipf("%s is not installed", Tool)
}

func TestExtractReadsEveryPageWhenThePageCapIsZero(t *testing.T) {
	requireExtractor(t)
	path := writePDF(t, pdfWithPages("Static torque 12 Nm", "Rated torque 9.1 Nm", "Weight 8.9 kg"))
	text, capped, err := Extract(context.Background(), path, 0, 1<<20)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, want := range []string{"Static torque 12 Nm", "Rated torque 9.1 Nm", "Weight 8.9 kg"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q: %q", want, text)
		}
	}
	if capped {
		t.Error("an uncapped read reported a cap")
	}
}

func TestExtractStopsAtThePageCap(t *testing.T) {
	requireExtractor(t)
	path := writePDF(t, pdfWithPages("first page words", "second page words"))
	text, _, err := Extract(context.Background(), path, 1, 1<<20)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(text, "first page words") || strings.Contains(text, "second page words") {
		t.Errorf("a one-page cap returned %q", text)
	}
}

func TestExtractCutsAtTheByteCap(t *testing.T) {
	requireExtractor(t)
	path := writePDF(t, pdfWithPages("abcdefghijklmnopqrstuvwxyz"))
	text, capped, err := Extract(context.Background(), path, 0, 10)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !capped || text != "abcdefghij" {
		t.Errorf("text = %q, capped = %v, want the first 10 bytes and a cap", text, capped)
	}
}

func TestExtractFailsCleanlyWithoutTheBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	text, capped, err := Extract(context.Background(), writePDF(t, "%PDF-1.4\n"), 0, 1<<20)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap exec.ErrNotFound", err)
	}
	if text != "" || capped {
		t.Errorf("text = %q, capped = %v, want neither", text, capped)
	}
}

func TestExtractReportsAFileThatIsNotAPDF(t *testing.T) {
	requireExtractor(t)
	_, _, err := Extract(context.Background(), writePDF(t, "<html>not a pdf</html>"), 0, 1<<20)
	if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 {
		t.Errorf("err = %v, want pdftotext's exit code 1 (could not open as a PDF)", err)
	}
}

func TestExtractGivesUpWhenTheContextIsDone(t *testing.T) {
	requireExtractor(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, _, err := Extract(ctx, writePDF(t, pdfWithPages("never read")), 0, 1<<20)
	if err == nil || text != "" {
		t.Errorf("text = %q, err = %v, want no text and an error", text, err)
	}
}
