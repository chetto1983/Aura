//go:build !web_integration

package web

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/pdftext"
)

// onePagePDF is a hand-written one-page PDF whose content stream is stream, deflated
// when compressed is set. No cross-reference table: poppler rebuilds one by scanning.
func onePagePDF(t *testing.T, stream string, compressed bool) []byte {
	t.Helper()
	body, filter := []byte(stream), ""
	if compressed {
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		body, filter = buf.Bytes(), "/Filter/FlateDecode"
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 2000 14000]" +
		"/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>endobj\n" +
		"4 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n")
	fmt.Fprintf(&b, "5 0 obj<</Length %d%s>>stream\n", len(body), filter)
	b.Write(body)
	b.WriteString("\nendstream\nendobj\ntrailer<</Root 1 0 R/Size 6>>\n%%EOF\n")
	return b.Bytes()
}

func textLine(y int, text string) string {
	return fmt.Sprintf("BT /F1 12 Tf 20 %d Td (%s) Tj ET\n", y, text)
}

// requirePDFExtractor fails under $CI instead of skipping: a PDF lane that runs
// nowhere in CI is the falsely-green case (ci.yml installs poppler-utils for it).
func requirePDFExtractor(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(pdftext.Tool); err == nil {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatalf("%s is not installed and $CI is set", pdftext.Tool)
	}
	t.Skipf("%s is not installed", pdftext.Tool)
}

func servePDF(t *testing.T, body []byte) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	_, port := hostPort(t, srv.URL)
	c := fetchClient(t, map[string][]netip.Addr{"docs.test": {publicIP}})
	return c, "http://docs.test:" + port + "/datasheet.pdf"
}

// The RS datasheet of 2026-10-07 answered 200 application/pdf and died at the MIME
// allowlist although the aura image carries pdftotext (prd.md §12).
func TestFetch_PDFTakesThePDFLane(t *testing.T) {
	requirePDFExtractor(t)
	c, url := servePDF(t, onePagePDF(t, textLine(13000, "Static torque 12.00 Nm")+
		textLine(12980, "Weight 8.9 kg"), false))
	page, err := c.Fetch(context.Background(), "c", url)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(page.ContentMD, "Static torque 12.00 Nm") || !strings.Contains(page.ContentMD, "Weight 8.9 kg") {
		t.Errorf("content_md = %q, want the PDF's text", page.ContentMD)
	}
	if page.URL != url || page.Warning != "" {
		t.Errorf("url = %q, warning = %q, want %q and no warning", page.URL, page.Warning, url)
	}
}

// A deflated content stream expands: a body inside the cap can carry text past it.
func TestFetch_PDFTextPastTheCapIsCutAndSaysSo(t *testing.T) {
	requirePDFExtractor(t)
	var stream strings.Builder
	for y := 13900; y > 100; y -= 14 {
		stream.WriteString(textLine(y, "rated data rated data rated data rated data rated data"))
	}
	body := onePagePDF(t, stream.String(), true)
	c, url := servePDF(t, body)
	c.cfg.WebFetchMaxBodyBytes = len(body) + 100
	page, err := c.Fetch(context.Background(), "c", url)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.Warning != WarningTruncated || len(page.ContentMD) > c.cfg.WebFetchMaxBodyBytes {
		t.Errorf("warning = %q, %d bytes for a cap of %d, want truncated within the cap",
			page.Warning, len(page.ContentMD), c.cfg.WebFetchMaxBodyBytes)
	}
}

func TestFetch_PDFWithNoTextIsAnExtractionFailure(t *testing.T) {
	requirePDFExtractor(t)
	c, url := servePDF(t, onePagePDF(t, "0 0 m 10 10 l S\n", false))
	_, err := c.Fetch(context.Background(), "c", url)
	assertWebErr(t, err, CodeExtractionFailed, ReasonNoPDFText)
}

func TestFetch_BytesThatAreNotAPDFAreAnExtractionFailure(t *testing.T) {
	requirePDFExtractor(t)
	c, url := servePDF(t, []byte("<html>an error page served as application/pdf</html>"))
	_, err := c.Fetch(context.Background(), "c", url)
	assertWebErr(t, err, CodeExtractionFailed, ReasonUnreadablePDF)
}

// pdftotext killed by the fetch deadline exits non-zero like a broken file does; the
// model must hear "timeout", not "this is not a PDF".
func TestPDFErrorNamesTheDeadlineNotTheFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	killed := &exec.ExitError{}
	if got := pdfError(ctx, killed); got.Code != CodeTimeout {
		t.Errorf("a killed extraction after the deadline = %s/%s, want timeout", got.Code, got.Reason)
	}
	if got := pdfError(context.Background(), killed); got.Reason != ReasonUnreadablePDF {
		t.Errorf("a failed extraction within the deadline = %s/%s, want %s", got.Code, got.Reason, ReasonUnreadablePDF)
	}
}

func TestFetch_PDFWithoutTheExtractorIsAnExtractionFailure(t *testing.T) {
	c, url := servePDF(t, onePagePDF(t, textLine(13000, "never read"), false))
	t.Setenv("PATH", t.TempDir())
	_, err := c.Fetch(context.Background(), "c", url)
	assertWebErr(t, err, CodeExtractionFailed, ReasonNoPDFExtractor)
}
