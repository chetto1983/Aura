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
	"strings"
	"sync"
	"testing"

	telegramadapter "github.com/aura/aura/internal/channels/telegram"
	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// APICall records a single Telegram Bot API call made during a Capture run.
// Failed indicates that the fake Telegram server returned a non-2xx status for
// this call — used by the fallback scenarios so the snapshot distinguishes the
// failed attempt from the subsequent retry.
type APICall struct {
	Method string         `json:"method"`
	Body   map[string]any `json:"body"`
	Failed bool           `json:"failed,omitempty"`
}

// CaptureResult holds the complete output of one Capture run.
type CaptureResult struct {
	Response  llm.Response `json:"response"`
	Delivered bool         `json:"delivered"`
	Calls     []APICall    `json:"calls"`
}

// Option customises Capture's fake Telegram server. The default server
// always answers 200 OK; options can inject deterministic failures so
// fallback paths in Outbound become observable in the snapshot.
type Option func(*serverConfig)

type serverConfig struct {
	// entityEditFailsRemaining causes the next N editMessageText calls that
	// carry a non-empty entities body to be answered with HTTP 400, simulating
	// Telegram rejecting entity parsing. Subsequent calls (including the
	// adapter's plain-text retry) succeed normally.
	entityEditFailsRemaining int
}

// WithEntityEditError makes the next n editMessageText calls that carry
// entities fail with HTTP 400, simulating Telegram rejecting an entity-bearing
// edit. The adapter's fallback path retries with plain text (no entities) and
// must succeed for the run to deliver — both calls are recorded in the
// snapshot.
func WithEntityEditError(n int) Option {
	return func(c *serverConfig) { c.entityEditFailsRemaining = n }
}

// Capture runs ConsumeStream on the ported Outbound adapter for the given
// token stream, records all Telegram Bot API calls made during the run, and
// writes the result to testdata/<name>.json.  The file is (re)written on every
// call so a fresh snapshot is always on disk after the test suite runs.
//
// placeholder, when non-nil, is passed to ConsumeStream so the initial
// streaming content edits the existing message rather than sending a new one.
//
// opts customise the fake Telegram server — e.g. inject deterministic
// entity-edit failures so fallback paths become observable.
func Capture(t *testing.T, name string, tokens []llm.Token, placeholder *tele.Message, opts ...Option) CaptureResult {
	t.Helper()

	var calls []APICall
	srv := newFakeServer(t, &calls, opts...)
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
// library does not error. A mutex protects calls and the message-ID counter
// against the race detector. opts inject deterministic failures (see Option).
func newFakeServer(t *testing.T, calls *[]APICall, opts ...Option) *httptest.Server {
	t.Helper()
	cfg := serverConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	var mu sync.Mutex
	var seq int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeRequestBody(r)
		method := path.Base(r.URL.Path)

		mu.Lock()
		failNow := method == "editMessageText" && bodyHasEntities(body) && cfg.entityEditFailsRemaining > 0
		if failNow {
			cfg.entityEditFailsRemaining--
		}
		*calls = append(*calls, APICall{Method: method, Body: body, Failed: failNow})
		var id int
		if !failNow {
			seq++
			id = seq
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if failNow {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w,
				`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`,
			)
			return
		}
		_, _ = fmt.Fprintf(w,
			`{"ok":true,"result":{"message_id":%d,"chat":{"id":123},"date":1760000000,"text":"ok"}}`,
			id,
		)
	}))
}

// decodeRequestBody normalises the request body into a map. Telebot v4 sends
// editMessageText / sendMessage as application/x-www-form-urlencoded; older
// or alternate transports may send JSON. We try form first, fall back to JSON
// if the body isn't form-encoded.
func decodeRequestBody(r *http.Request) map[string]any {
	body := map[string]any{}
	if r.Body == nil {
		return body
	}
	// Reading r.Body via ParseForm consumes it; that's fine — we only inspect
	// the request once.
	if err := r.ParseForm(); err == nil && len(r.PostForm) > 0 {
		for k, v := range r.PostForm {
			if len(v) == 1 {
				body[k] = v[0]
			} else if len(v) > 1 {
				body[k] = v
			}
		}
		return body
	}
	// Fall back to JSON decode (unlikely path, but kept for symmetry with the
	// pre-refactor behaviour).
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

// bodyHasEntities returns true when the editMessageText body carries a
// non-empty entities field. Telebot serialises entities as a JSON string in
// the form-encoded body, so the string check covers both the form and the
// JSON-body transports.
func bodyHasEntities(body map[string]any) bool {
	v, ok := body["entities"]
	if !ok {
		return false
	}
	switch s := v.(type) {
	case string:
		t := strings.TrimSpace(s)
		return t != "" && t != "[]" && t != "null"
	case []any:
		return len(s) > 0
	}
	return false
}
