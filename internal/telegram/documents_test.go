package telegram

import (
	"strings"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"
)

func TestValidatePDFAcceptsPDF(t *testing.T) {
	doc := &tele.Document{
		File: tele.File{FileSize: 1 * 1024 * 1024},
		MIME: "application/pdf",
	}
	if err := validatePDF(doc, 100); err != nil {
		t.Errorf("validatePDF: %v", err)
	}
}

func TestValidatePDFRejectsNonPDF(t *testing.T) {
	cases := []struct {
		mime string
		want string
	}{
		{"image/png", "only PDFs"},
		{"text/plain", "only PDFs"},
		{"", "(unknown)"},
		{"application/octet-stream", "only PDFs"},
	}
	for _, tc := range cases {
		doc := &tele.Document{File: tele.File{FileSize: 1024}, MIME: tc.mime}
		err := validatePDF(doc, 100)
		if err == nil {
			t.Errorf("MIME=%q: expected error", tc.mime)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("MIME=%q: err=%v, want contains %q", tc.mime, err, tc.want)
		}
	}
}

func TestValidatePDFRejectsOversize(t *testing.T) {
	doc := &tele.Document{
		File: tele.File{FileSize: 200 * 1024 * 1024},
		MIME: "application/pdf",
	}
	err := validatePDF(doc, 100)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want too large", err)
	}
}

func TestValidatePDFNoCapWhenZero(t *testing.T) {
	// maxFileMB=0 means "no cap" — should accept anything.
	doc := &tele.Document{
		File: tele.File{FileSize: 500 * 1024 * 1024},
		MIME: "application/pdf",
	}
	if err := validatePDF(doc, 0); err != nil {
		t.Errorf("err = %v, want nil when cap is 0", err)
	}
}

func TestValidatePDFNilDoc(t *testing.T) {
	if err := validatePDF(nil, 100); err == nil {
		t.Error("expected error on nil doc")
	}
}

func TestValidatePDFCharsetParam(t *testing.T) {
	// Telegram occasionally returns "application/pdf; charset=binary"-style
	// MIME — the check should accept the "application/pdf" prefix.
	doc := &tele.Document{
		File: tele.File{FileSize: 1024},
		MIME: "application/pdf; charset=binary",
	}
	if err := validatePDF(doc, 100); err != nil {
		t.Errorf("err = %v, want nil for prefixed mime", err)
	}
}

func TestSafeNameStripsDangerousChars(t *testing.T) {
	cases := []struct{ in, want string }{
		{"paper.pdf", "paper.pdf"},
		{"  paper.pdf  ", "paper.pdf"},
		{"", "(unnamed.pdf)"},
		{"   ", "(unnamed.pdf)"},
		{"foo/bar.pdf", "foobar.pdf"},
		{`foo\bar.pdf`, "foobar.pdf"},
		{"name\twith\ncontrols.pdf", "namewithcontrols.pdf"},
	}
	for _, tc := range cases {
		got := safeName(tc.in)
		if got != tc.want {
			t.Errorf("safeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSafeNameTruncatesLong(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := safeName(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("got %q, want ellipsis suffix", got)
	}
	if len(got) > 90 {
		t.Errorf("len(got) = %d, want ≤ 90", len(got))
	}
}

func TestFormatSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{19814, "19.3 KB"},
		{1500 * 1024, "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range cases {
		got := formatSize(tc.bytes)
		if got != tc.want {
			t.Errorf("formatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1400 * time.Millisecond, "1.4s"},
		{37 * time.Second, "37s"},
		{2*time.Minute + 18*time.Second, "2m 18s"},
	}
	for _, tc := range cases {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestPluralS(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}
	for _, tc := range cases {
		got := pluralS(tc.n)
		if got != tc.want {
			t.Errorf("pluralS(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
