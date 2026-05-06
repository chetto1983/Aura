package telegram

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
	tele "gopkg.in/telebot.v4"
)

type fakeDocumentPyodideRunner struct {
	calls        int
	code         string
	allowNetwork bool
}

func (r *fakeDocumentPyodideRunner) Execute(_ context.Context, code string, allowNetwork bool) (*sandbox.Result, error) {
	r.calls++
	r.code = code
	r.allowNetwork = allowNetwork
	return &sandbox.Result{
		OK:     true,
		Stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","sheet_count":1,"row_count":1}}`,
	}, nil
}

func TestValidatePDFAcceptsPDF(t *testing.T) {
	doc := &tele.Document{
		File: tele.File{FileSize: 1 * 1024 * 1024},
		MIME: "application/pdf",
	}
	if err := validatePDF(doc, 100); err != nil {
		t.Errorf("validatePDF: %v", err)
	}
}

func TestValidateDocumentAcceptsUniversalFormats(t *testing.T) {
	cases := []struct {
		name string
		mime string
		kind source.Kind
	}{
		{"paper.pdf", "application/pdf", source.KindPDF},
		{"notes.txt", "text/plain", source.KindText},
		{"daily.md", "text/markdown", source.KindMarkdown},
		{"data.json", "application/json", source.KindJSON},
		{"budget.csv", "text/csv", source.KindCSV},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", source.KindXLSX},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &tele.Document{File: tele.File{FileSize: 1024}, MIME: tc.mime, FileName: tc.name}
			format, err := validateDocument(doc, 100)
			if err != nil {
				t.Fatalf("validateDocument() error = %v", err)
			}
			if format.Kind != tc.kind {
				t.Fatalf("kind = %s, want %s", format.Kind, tc.kind)
			}
		})
	}
}

func TestValidateDocumentRejectsDeferredDOCX(t *testing.T) {
	doc := &tele.Document{
		File:     tele.File{FileSize: 1024},
		MIME:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		FileName: "memo.docx",
	}
	_, err := validateDocument(doc, 100)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("err = %v, want unsupported file type", err)
	}
}

func TestValidateDocumentRejectsUnsupported(t *testing.T) {
	doc := &tele.Document{File: tele.File{FileSize: 1024}, MIME: "application/octet-stream", FileName: "deck.pptx"}
	_, err := validateDocument(doc, 100)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("err = %v, want unsupported file type", err)
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

func TestDocHandlerUnauthorizedDocumentRejectedBeforeWork(t *testing.T) {
	sources := newDocumentTestSourceStore(t)
	h := newDocHandler(docHandlerConfig{
		Sources: sources,
		Allowlist: func(string) bool {
			return false
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)
	ctx := tele.NewContext(nil, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 999, Username: "guest"},
		Chat:   &tele.Chat{ID: 999},
		Document: &tele.Document{
			File:     tele.File{FileID: "doc-1", FileSize: int64(len(fakeTelegramPDFBytes))},
			MIME:     "application/pdf",
			FileName: "blocked.pdf",
		},
	}})

	if err := h.onDocument(ctx); err != nil {
		t.Fatalf("onDocument() error = %v, want nil", err)
	}
	got, err := sources.List(source.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("sources = %d, want none for unauthorized document", len(got))
	}
	if !h.beginWork() {
		t.Fatal("beginWork returned false after unauthorized document")
	}
	h.finishWork()
}

func TestDocHandlerAuthorizedPDFDocumentStoresSource(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	h := newDocHandler(docHandlerConfig{
		Bot:     tb,
		Sources: sources,
		Allowlist: func(string) bool {
			return true
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Document: &tele.Document{
			File:     tele.File{FileID: "doc-1", FileSize: int64(len(fakeTelegramPDFBytes))},
			MIME:     "application/pdf",
			FileName: "paper.pdf",
		},
	}})

	if err := h.onDocument(ctx); err != nil {
		t.Fatalf("onDocument() error = %v, want nil", err)
	}

	var stored []*source.Source
	waitUntil(t, time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Kind: source.KindPDF})
		return err == nil && len(stored) == 1
	})
	h.Stop()

	if stored[0].Status != source.StatusStored {
		t.Fatalf("source status = %q, want %q", stored[0].Status, source.StatusStored)
	}
	if stored[0].Filename != "paper.pdf" {
		t.Fatalf("source filename = %q, want paper.pdf", stored[0].Filename)
	}
	if stored[0].SizeBytes != int64(len(fakeTelegramPDFBytes)) {
		t.Fatalf("source size = %d, want %d", stored[0].SizeBytes, len(fakeTelegramPDFBytes))
	}
	if got := countTelegramMethods(calls, "getFile"); got != 1 {
		t.Fatalf("getFile calls = %d, want 1", got)
	}
}

func TestDocHandlerAuthorizedTextDocumentExtractsSource(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	h := newDocHandler(docHandlerConfig{
		Bot:     tb,
		Sources: sources,
		Allowlist: func(string) bool {
			return true
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Document: &tele.Document{
			File:     tele.File{FileID: "doc-1", FileSize: int64(len(fakeTelegramPDFBytes))},
			MIME:     "text/plain",
			FileName: "notes.txt",
		},
	}})

	if err := h.onDocument(ctx); err != nil {
		t.Fatalf("onDocument() error = %v, want nil", err)
	}

	var stored []*source.Source
	waitUntil(t, time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Kind: source.KindText})
		return err == nil && len(stored) == 1 && stored[0].Status == source.StatusExtractComplete
	})
	h.Stop()

	if stored[0].Extract == nil {
		t.Fatalf("source extraction metadata missing: %+v", stored[0])
	}
	if _, err := os.Stat(sources.Path(stored[0].ID, source.ExtractMarkdownFile)); err != nil {
		t.Fatalf("extract.md missing: %v", err)
	}
	if got := countTelegramMethods(calls, "getFile"); got != 1 {
		t.Fatalf("getFile calls = %d, want 1", got)
	}
}

func TestDocHandlerAuthorizedXLSXDocumentExtractsSource(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	runner := &fakeDocumentPyodideRunner{}
	h := newDocHandler(docHandlerConfig{
		Bot:       tb,
		Sources:   sources,
		Extractor: runner,
		Allowlist: func(string) bool {
			return true
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Document: &tele.Document{
			File:     tele.File{FileID: "doc-1", FileSize: int64(len(fakeTelegramPDFBytes))},
			MIME:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			FileName: "budget.xlsx",
		},
	}})

	if err := h.onDocument(ctx); err != nil {
		t.Fatalf("onDocument() error = %v, want nil", err)
	}

	var stored []*source.Source
	waitUntil(t, time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Kind: source.KindXLSX})
		return err == nil && len(stored) == 1 && stored[0].Status == source.StatusExtractComplete
	})
	h.Stop()

	if runner.calls != 1 || runner.allowNetwork || !strings.Contains(runner.code, "pd.ExcelFile") {
		t.Fatalf("runner calls=%d allowNetwork=%v codeContainsExcel=%v", runner.calls, runner.allowNetwork, strings.Contains(runner.code, "pd.ExcelFile"))
	}
	if stored[0].Extract == nil || stored[0].Extract.ExtractorName != "pyodide_xlsx" {
		t.Fatalf("source extraction metadata = %+v, want pyodide_xlsx", stored[0].Extract)
	}
	extract, err := os.ReadFile(sources.Path(stored[0].ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("extract.md missing: %v", err)
	}
	if !strings.Contains(string(extract), "sandbox") {
		t.Fatalf("extract.md = %q", extract)
	}
	if got := countTelegramMethods(calls, "getFile"); got != 1 {
		t.Fatalf("getFile calls = %d, want 1", got)
	}
}

func TestDocHandlerStopWaitsForInFlightWorker(t *testing.T) {
	h := newDocHandler(docHandlerConfig{})

	workerStarted := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerDone atomic.Bool
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		close(workerStarted)
		<-releaseWorker
		workerDone.Store(true)
	}()

	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	stopped := make(chan struct{})
	go func() {
		h.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned before document worker completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseWorker)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after document worker completed")
	}
	if !workerDone.Load() {
		t.Fatal("worker did not complete before Stop returned")
	}
}

func TestDocHandlerStopWaitsForWorkRegisteredBeforeWorkerLaunch(t *testing.T) {
	h := newDocHandler(docHandlerConfig{})

	if !h.beginWork() {
		t.Fatal("beginWork returned false before Stop")
	}

	stopped := make(chan struct{})
	go func() {
		h.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned while registered document work was still in progress")
	case <-time.After(100 * time.Millisecond):
	}

	if h.beginWork() {
		t.Fatal("beginWork returned true after Stop began")
	}

	h.finishWork()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after registered document work finished")
	}

	if h.beginWork() {
		t.Fatal("beginWork returned true after Stop returned")
	}
}

func newDocumentTestSourceStore(t *testing.T) *source.Store {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new source store: %v", err)
	}
	return store
}
