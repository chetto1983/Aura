package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/source"
	tele "gopkg.in/telebot.v4"
)

// Maximum number of OCR jobs in flight at once. Telegram allows >1 incoming
// document at a time and Mistral OCR is rate-limited; serializing 2-at-a-time
// is a sensible default that keeps latency bounded for a single user without
// flooding the upstream API on a burst.
const docConcurrencyLimit = 2

// Default per-OCR-call timeout. PDFs of 100 pages take ~30-60s; the cap keeps
// a stalled call from holding a worker slot forever.
const docOCRTimeout = 5 * time.Minute

// AfterOCRHook is invoked once a source reaches StatusOCRComplete on disk.
// Slice 6 (ingest) wires the LLM compile step here. Returning an error is
// logged but does not surface to the user — the OCR step is already
// considered successful by then.
type AfterOCRHook func(ctx context.Context, src *source.Source) error

// docHandlerConfig is the per-Bot wiring for slice 4 (Telegram → OCR).
type docHandlerConfig struct {
	Bot       *tele.Bot
	Sources   *source.Store
	OCR       *ocr.Client // may be nil if OCR_ENABLED=false or MISTRAL_API_KEY missing
	MaxFileMB int
	AfterOCR  AfterOCRHook
	Allowlist func(userID string) bool
	Logger    *slog.Logger
}

type docHandler struct {
	bot       *tele.Bot
	sources   *source.Store
	ocr       *ocr.Client
	maxFileMB int
	afterOCR  AfterOCRHook
	allowed   func(string) bool
	logger    *slog.Logger
	sem       chan struct{}
	wg        sync.WaitGroup // tracks in-flight workers for graceful shutdown
}

func newDocHandler(cfg docHandlerConfig) *docHandler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &docHandler{
		bot:       cfg.Bot,
		sources:   cfg.Sources,
		ocr:       cfg.OCR,
		maxFileMB: cfg.MaxFileMB,
		afterOCR:  cfg.AfterOCR,
		allowed:   cfg.Allowlist,
		logger:    cfg.Logger,
		sem:       make(chan struct{}, docConcurrencyLimit),
	}
}

// onDocument is a Telegram tele.Handler. Validates synchronously, sends the
// initial progress reply, and spawns a goroutine for the rest. Returns nil
// quickly so the Telegram update loop is never blocked.
func (h *docHandler) onDocument(c tele.Context) error {
	doc := c.Message().Document
	if doc == nil {
		return nil
	}

	userID := strconv.FormatInt(c.Sender().ID, 10)
	if h.allowed != nil && !h.allowed(userID) {
		h.logger.Warn("document from non-allowlisted user", "user_id", userID)
		return nil
	}

	if h.sources == nil {
		_ = c.Reply("Source store unavailable.")
		return nil
	}

	if err := validatePDF(doc, h.maxFileMB); err != nil {
		_ = c.Reply("❌ " + err.Error())
		return nil
	}

	progress, err := h.bot.Send(c.Chat(), "📄 Got it — saving "+safeName(doc.FileName)+"…")
	if err != nil {
		h.logger.Error("send initial progress reply failed", "user_id", userID, "err", err)
		return nil
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.process(context.Background(), userID, doc, progress)
	}()
	return nil
}

// process runs the PDF → ocr_complete pipeline. Each step edits the same
// progress message; failures replace its text with a single-line error.
func (h *docHandler) process(ctx context.Context, userID string, doc *tele.Document, progress *tele.Message) {
	// Bounded concurrency: queue if 2 jobs already in flight.
	h.sem <- struct{}{}
	defer func() { <-h.sem }()

	editor := newProgressEditor(h.bot, progress, h.logger)

	// Step 1: download from Telegram.
	body, err := h.bot.File(&doc.File)
	if err != nil {
		editor.fail("Download failed: " + err.Error())
		return
	}
	pdfBytes, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		editor.fail("Read failed: " + err.Error())
		return
	}

	// Step 2: store as immutable source.
	src, dup, err := h.sources.Put(ctx, source.PutInput{
		Kind:     source.KindPDF,
		Filename: safeName(doc.FileName),
		MimeType: "application/pdf",
		Bytes:    pdfBytes,
	})
	if err != nil {
		editor.fail("Save failed: " + err.Error())
		return
	}

	if dup {
		editor.set(fmt.Sprintf("🔁 Already stored as %s · status: %s · send /reocr %s to re-run.",
			src.ID, src.Status, src.ID))
		h.logger.Info("pdf duplicate", "user_id", userID, "source_id", src.ID, "status", src.Status)
		return
	}

	if h.ocr == nil {
		editor.set(fmt.Sprintf("📥 Saved · %s · %s — OCR disabled.",
			src.ID, formatSize(src.SizeBytes)))
		h.logger.Info("pdf stored, ocr disabled", "user_id", userID, "source_id", src.ID, "size_bytes", src.SizeBytes)
		return
	}

	editor.set(fmt.Sprintf("📥 Saved · %s · %s · running OCR…",
		src.ID, formatSize(src.SizeBytes)))

	// Step 3: OCR.
	ocrCtx, cancel := context.WithTimeout(ctx, docOCRTimeout)
	defer cancel()

	start := time.Now()
	res, err := h.ocr.Process(ocrCtx, ocr.ProcessInput{PDFBytes: pdfBytes})
	if err != nil {
		_, _ = h.sources.Update(src.ID, func(s *source.Source) error {
			s.Status = source.StatusFailed
			s.Error = err.Error()
			return nil
		})
		editor.fail("OCR failed: " + err.Error())
		h.logger.Warn("ocr failed", "user_id", userID, "source_id", src.ID, "err", err)
		return
	}
	duration := time.Since(start)

	// Step 4: write ocr.md and ocr.json next to the source (PDR §4 layout).
	md := ocr.RenderMarkdown(ocr.RenderMeta{
		SourceID: src.ID,
		Filename: src.Filename,
		Model:    res.Response.Model,
	}, res.Response)

	if err := writeNextToSource(h.sources, src.ID, "ocr.md", []byte(md)); err != nil {
		editor.fail("Write ocr.md failed: " + err.Error())
		return
	}
	if err := writeNextToSource(h.sources, src.ID, "ocr.json", res.RawJSON); err != nil {
		editor.fail("Write ocr.json failed: " + err.Error())
		return
	}

	pageCount := len(res.Response.Pages)
	if res.Response.UsageInfo != nil && res.Response.UsageInfo.PagesProcessed > 0 {
		pageCount = res.Response.UsageInfo.PagesProcessed
	}

	// Step 5: flip status, attach OCR metadata.
	updated, err := h.sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = res.Response.Model
		s.PageCount = pageCount
		return nil
	})
	if err != nil {
		editor.fail("Status update failed: " + err.Error())
		return
	}

	editor.set(fmt.Sprintf("✅ Done · %s · %d page%s · %s · ready for ingest",
		src.ID, pageCount, pluralS(pageCount), formatDuration(duration)))

	h.logger.Info("pdf processed",
		"user_id", userID,
		"source_id", src.ID,
		"sha_prefix", src.SHA256[:16],
		"page_count", pageCount,
		"size_bytes", src.SizeBytes,
		"ocr_duration_ms", duration.Milliseconds(),
	)

	if h.afterOCR != nil {
		// Hook for slice 6 (ingest_source). Errors are logged but not
		// surfaced — OCR is already complete on disk.
		hookCtx, hookCancel := context.WithTimeout(ctx, docOCRTimeout)
		defer hookCancel()
		if err := h.afterOCR(hookCtx, updated); err != nil {
			h.logger.Warn("afterOCR hook failed", "source_id", src.ID, "err", err)
		}
	}
}

// validatePDF rejects non-PDF MIME types and oversized uploads up front. The
// OCR client and source store both gracefully reject empty bytes too, but
// failing here gives the user a faster, more specific error.
func validatePDF(doc *tele.Document, maxFileMB int) error {
	if doc == nil {
		return errors.New("no document attached")
	}
	mime := strings.ToLower(strings.TrimSpace(doc.MIME))
	if !strings.HasPrefix(mime, "application/pdf") {
		got := mime
		if got == "" {
			got = "(unknown)"
		}
		return fmt.Errorf("only PDFs supported (got %s)", got)
	}
	if maxFileMB > 0 {
		max := int64(maxFileMB) * 1024 * 1024
		if doc.FileSize > max {
			return fmt.Errorf("PDF too large: %s exceeds %d MB cap",
				formatSize(doc.FileSize), maxFileMB)
		}
	}
	return nil
}

// writeNextToSource writes data to <raw>/<id>/<name> using source.Store.Path
// for containment-checked path resolution.
func writeNextToSource(s *source.Store, id, name string, data []byte) error {
	path := s.Path(id, name)
	if path == "" {
		return fmt.Errorf("invalid path for %s/%s", id, name)
	}
	return os.WriteFile(path, data, 0o644)
}

// safeName sanitizes a Telegram-supplied filename for display only. The real
// disk path uses original.<ext> regardless (see internal/source/store.go).
func safeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "(unnamed.pdf)"
	}
	// Strip any control or path characters from the display string.
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if len(cleaned) > 80 {
		cleaned = cleaned[:80] + "…"
	}
	return cleaned
}

// formatSize returns a human-readable size string ("19.4 KB", "3.2 MB").
func formatSize(bytes int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatDuration returns a short human duration ("1.4s", "37s", "2m 18s").
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		mins := int(d.Minutes())
		secs := int(d.Seconds()) - mins*60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// progressEditor edits the same Telegram message across pipeline steps,
// falling back to a fresh send if the original message can no longer be
// edited (deleted, too old, etc.).
type progressEditor struct {
	bot    *tele.Bot
	msg    *tele.Message
	logger *slog.Logger
	mu     sync.Mutex
}

func newProgressEditor(b *tele.Bot, msg *tele.Message, logger *slog.Logger) *progressEditor {
	return &progressEditor{bot: b, msg: msg, logger: logger}
}

func (p *progressEditor) set(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.msg == nil {
		return
	}
	updated, err := p.bot.Edit(p.msg, text)
	if err != nil {
		p.logger.Warn("progress edit failed, sending fresh message", "err", err)
		if p.msg.Chat != nil {
			fresh, sendErr := p.bot.Send(p.msg.Chat, text)
			if sendErr == nil {
				p.msg = fresh
			}
		}
		return
	}
	if updated != nil {
		p.msg = updated
	}
}

func (p *progressEditor) fail(text string) {
	p.set("❌ " + text)
}
