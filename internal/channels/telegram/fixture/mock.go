// Package fixture provides a record-and-replay harness for the Telegram
// streaming path. Capture drives channels/telegram.Outbound.ConsumeStream
// against a lightweight fake Telegram HTTP server, records every Bot API call
// made during the run, and writes the captured sequence to
// testdata/<name>.json.
//
// US-301 created the initial snapshots using internal/telegram.Bot.ConsumeStream.
// US-401 switched Capture to use the ported Outbound.ConsumeStream so the
// fixture validates the new implementation; the committed JSON files are the
// byte-comparison baseline (diff must be 0 across all scenarios).
package fixture

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"

	telegramadapter "github.com/aura/aura/internal/channels/telegram"
	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// APICall records a single Telegram Bot API call made during a Capture run.
type APICall struct {
	Method string         `json:"method"`
	Body   map[string]any `json:"body"`
}

// CaptureResult holds the complete output of one Capture run.
type CaptureResult struct {
	Response  llm.Response `json:"response"`
	Delivered bool         `json:"delivered"`
	Calls     []APICall    `json:"calls"`
}

// Capture runs ConsumeStream on the ported Outbound adapter for the given
// token stream, records all Telegram Bot API calls made during the run, and
// writes the result to testdata/<name>.json.  The file is (re)written on every
// call so a fresh snapshot is always on disk after the test suite runs.
//
// placeholder, when non-nil, is passed to ConsumeStream so the initial
// streaming content edits the existing message rather than sending a new one.
func Capture(t *testing.T, name string, tokens []llm.Token, placeholder *tele.Message) CaptureResult {
	t.Helper()

	var calls []APICall
	srv := newFakeServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("fixture.Capture: tele.NewBot: %v", err)
	}

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "test",
	}})

	ch := make(chan llm.Token, len(tokens))
	for _, tok := range tokens {
		ch <- tok
	}
	close(ch)

	out := telegramadapter.NewOutbound(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	resp, delivered, err := out.ConsumeStream(ctx, ch, "123", placeholder)
	if err != nil {
		t.Fatalf("fixture.Capture: ConsumeStream: %v", err)
	}

	result := CaptureResult{
		Response:  resp,
		Delivered: delivered,
		Calls:     calls,
	}

	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("fixture.Capture: mkdir testdata: %v", err)
	}
	snapshotPath := filepath.Join("testdata", name+".json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("fixture.Capture: marshal: %v", err)
	}
	if err := os.WriteFile(snapshotPath, data, 0o644); err != nil {
		t.Fatalf("fixture.Capture: write snapshot %s: %v", snapshotPath, err)
	}
	return result
}

// newFakeServer returns an httptest.Server that records every Telegram Bot API
// call into calls and replies with a minimal valid message JSON so the bot
// library does not error. Requests are sequential (consumeStream makes each
// HTTP call synchronously), so a mutex protects calls and the message-ID
// counter against the race detector rather than against real concurrency.
func newFakeServer(t *testing.T, calls *[]APICall) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var seq int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)

		mu.Lock()
		*calls = append(*calls, APICall{Method: method, Body: body})
		seq++
		id := seq
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"ok":true,"result":{"message_id":%d,"chat":{"id":123},"date":1760000000,"text":"ok"}}`,
			id,
		)
	}))
}
