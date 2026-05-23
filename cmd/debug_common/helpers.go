// Package debugcommon provides shared scenario/assert helpers for Aura debug harnesses.
package debugcommon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	source "github.com/aura/aura/internal/storage/sources/store"
)

// Harness is a lightweight harness for debug smoke scripts.
type Harness struct {
	name string
}

// New returns a Harness tagged with the given harness name.
func New(name string) *Harness {
	return &Harness{name: name}
}

// Fail prints a prefixed error message and exits with status 1.
func (h *Harness) Fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, h.name+": "+format+"\n", args...)
	os.Exit(1)
}

// MustEqual asserts got == want using fmt.Sprintf and calls Fail if not.
func (h *Harness) MustEqual(label string, got, want any) {
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		h.Fail("  %s = %v, want %v", label, got, want)
	}
}

// Scenario runs fn, printing the scenario name and pass/fail result.
func (h *Harness) Scenario(name string, fn func() error) {
	fmt.Printf("-> %s\n", name)
	if err := fn(); err != nil {
		h.Fail("  FAIL: %v", err)
	}
	fmt.Println("  ok")
}

// DocumentCall records one fake document delivery from a debug harness.
type DocumentCall struct {
	UserID, Filename, Caption string
	BodyLen                   int
}

// DocumentSender records document deliveries without touching Telegram.
type DocumentSender struct {
	Calls []DocumentCall
}

// SendDocumentToUser satisfies registry.DocumentSender for document tools.
func (s *DocumentSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.Calls = append(s.Calls, DocumentCall{UserID: userID, Filename: filename, Caption: caption, BodyLen: len(body)})
	return nil
}

// Reset clears recorded fake document deliveries.
func (s *DocumentSender) Reset() {
	s.Calls = s.Calls[:0]
}

// TempSourceStore creates a temp wiki-backed source store for hermetic debug commands.
func (h *Harness) TempSourceStore(prefix string, keep bool) (*source.Store, string, func()) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	wikiDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		h.Fail("mkdir temp wiki: %v", err)
	}
	cleanup := func() {
		if !keep {
			_ = os.RemoveAll(wikiDir)
		}
	}
	if keep {
		fmt.Printf("temp wiki: %s\n", wikiDir)
	}
	// Per Phase-FS-LAYOUT (2026-05-23) the source store roots itself
	// on a sourcesDir passed by the caller — not derived from wikiDir.
	// Debug helpers keep the legacy layout (sources under wiki/raw) by
	// passing the same path so existing fixtures continue to work.
	sourcesDir := filepath.Join(wikiDir, "raw")
	store, err := source.NewStore(sourcesDir, logger)
	if err != nil {
		cleanup()
		h.Fail("source.NewStore: %v", err)
	}
	return store, wikiDir, cleanup
}

// WriteOptionalOutput writes body to outFile when requested by a debug command.
func WriteOptionalOutput(outFile string, body []byte) error {
	if outFile == "" {
		return nil
	}
	abs, _ := filepath.Abs(outFile)
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		return fmt.Errorf("write -out: %w", err)
	}
	fmt.Printf("  wrote %d bytes to %s\n", len(body), abs)
	return nil
}

// EnvDefault returns a trimmed env value, or fallback when unset/blank.
func EnvDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// ContainsTools reports whether every wanted tool name appears in called.
func ContainsTools(called, wants []string) bool {
	seen := make(map[string]bool, len(called))
	for _, name := range called {
		seen[name] = true
	}
	for _, want := range wants {
		if !seen[want] {
			return false
		}
	}
	return true
}

// ContainsAllText reports whether text contains every wanted substring.
func ContainsAllText(text string, wants []string) bool {
	text = strings.ToLower(text)
	for _, want := range wants {
		if !strings.Contains(text, strings.ToLower(want)) {
			return false
		}
	}
	return true
}

// ContainsNoText reports whether text avoids every non-blank rejected substring.
func ContainsNoText(text string, rejects []string) bool {
	text = strings.ToLower(text)
	for _, reject := range rejects {
		if strings.TrimSpace(reject) == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(reject)) {
			return false
		}
	}
	return true
}

// SingleLine collapses whitespace and truncates long output for debug summaries.
func SingleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
