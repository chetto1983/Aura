//go:build live_e2e

// artifact_live_e2e_test.go is the AUTHORIZED, real-Bot-API delivery receipt for the
// artifact chain (audit row EXT-005). The unit tier proves the consumer against a fake
// bot, and cot_live_e2e_test.go proves the status pane against a real model — but
// neither ever handed a byte to Telegram, so "send_file delivers" stayed an assertion
// about our own fakes. EXT-005 asks for one thing the fakes cannot give: a receipt from
// the other side.
//
// It drives the SAME chain the live turn drives, from the tool-result Event outward:
//
//	agent.Event{Actions.ToolInvocation(end) + Actions.ArtifactDelta}   ← what send_file's
//	  → agui.Translate                                                    Meta lift produces
//	  → agui.NewFanout + Subscribe-before-Run
//	  → the real newArtifact consumer
//	  → a real *tele.Bot → sendDocument
//
// The only link it does NOT exercise is send_file's own box staging (it hands the
// descriptor a host path instead of a CopyArtifactsOut-staged one) and Telegram's
// inbound polling — an inbound message can only come from a real Telegram user, so the
// turn cannot be originated from here.
//
// REVERSIBLE BY CONSTRUCTION: the fixture is a few bytes of text carrying a nonce, and
// the delivered message is deleted before the test returns (cleanup is deferred, so it
// runs even on a failed assertion). Nothing survives in the chat.
//
// AUTHORIZED gate behind the live_e2e tag (never CI): it messages a real person. It
// t.Skips unless BOTH the bot credential and an explicitly named chat are present, so
// running the tag by accident cannot deliver anything.
//
// REPRODUCE:
//
//	set -a; . ./.env; set +a
//	AURA_E2E_TELEGRAM_CHAT_ID=<chat id> \
//	  go test -tags live_e2e -run TestArtifactLiveE2E -timeout 120s -v ./internal/channels/telegram/
package telegram

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/google/uuid"
)

func TestArtifactLiveE2E_DocumentReachesAnAuthorizedChat(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	chatRaw := strings.TrimSpace(os.Getenv("AURA_E2E_TELEGRAM_CHAT_ID"))
	if token == "" || chatRaw == "" {
		t.Skip("live_e2e: TELEGRAM_BOT_TOKEN and AURA_E2E_TELEGRAM_CHAT_ID must BOTH be set — " +
			"this test messages a real person, so it never runs on a partial environment. " +
			"Run: set -a; . ./.env; set +a; AURA_E2E_TELEGRAM_CHAT_ID=<chat id> " +
			"go test -tags live_e2e -run TestArtifactLiveE2E -timeout 120s -v ./internal/channels/telegram/")
	}
	chatID, err := strconv.ParseInt(chatRaw, 10, 64)
	if err != nil {
		t.Fatalf("AURA_E2E_TELEGRAM_CHAT_ID %q is not a chat id: %v", chatRaw, err)
	}

	bot, err := tele.NewBot(tele.Settings{Token: token, Synchronous: true})
	if err != nil {
		t.Fatalf("connect to the Bot API: %v", err)
	}

	// A fixture that names itself: the nonce is in the filename AND in the body, so a
	// receipt cannot be confused with an earlier run's leftover.
	nonce := uuid.NewString()[:8]
	filename := "aura-ext-005-" + nonce + ".txt"
	body := "EXT-005 delivery receipt\nnonce: " + nonce + "\n"
	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// An accented caption on purpose: the ASCII fold (Pitfall 4) is the difference
	// between a delivery and a Bot API 400, and only a real send can prove it holds.
	callID := "call-" + nonce
	descriptor := map[string]any{
		"path":         path,
		"filename":     filename,
		"caption":      "Aura EXT-005 — fixture reversibile, verrà cancellato subito",
		"tool_call_id": callID,
		"size_bytes":   int64(len(body)),
	}
	ev := &agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Author:    "aura",
		Timestamp: time.Now(),
		Actions: agent.Actions{
			ToolInvocation: &agent.ToolInvocation{
				Event:         agent.ToolInvocationEnd,
				ToolCallID:    callID,
				ToolName:      "send_file",
				Status:        "ok",
				ResultPreview: "queued " + filename + " for delivery",
			},
			ArtifactDelta: descriptor,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	seq := func(yield func(*agent.Event, error) bool) { yield(ev, nil) }
	translated := agui.Translate("thread-ext-005", "run-ext-005", agui.NewIDGenerator(), seq, false)
	fo := agui.NewFanout(translated)
	artifactCh := fo.Subscribe()
	fo.Run(ctx)

	// The real consumer, drained the way handleTurn drains it. It swallows a failed
	// Send by contract (a delivery must never wedge a turn), so the receipt is read
	// from the chat afterwards, not from the consumer.
	var delivered *tele.Message
	for e := range artifactCh {
		if msg, ok := newArtifact(bot, tele.ChatID(chatID)).consumeEvent(e); ok {
			delivered = msg
		}
	}
	if delivered == nil {
		t.Fatal("no document reached the chat: the artifact consumer rendered nothing — " +
			"either the translator dropped the descriptor or the Bot API refused the upload")
	}
	// Deferred so an assertion failure below still leaves the chat clean.
	defer func() {
		if err := bot.Delete(delivered); err != nil {
			t.Errorf("cleanup: the fixture message %d was NOT deleted: %v", delivered.ID, err)
		}
	}()

	if delivered.Document == nil {
		t.Fatalf("message %d arrived without a document attachment", delivered.ID)
	}
	if delivered.Document.FileName != filename {
		t.Fatalf("delivered filename = %q, want %q", delivered.Document.FileName, filename)
	}
	if got := int64(delivered.Document.FileSize); got != int64(len(body)) {
		t.Fatalf("delivered size = %d bytes, want %d", got, len(body))
	}
	for _, r := range delivered.Caption {
		if r > unicode.MaxASCII {
			t.Fatalf("caption %q reached the Bot API carrying a non-ASCII rune %q — the fold leaks",
				delivered.Caption, r)
		}
	}
	t.Logf("EXT-005 receipt: chat=%d message_id=%d file=%q %d bytes caption=%q (deleted on return)",
		chatID, delivered.ID, delivered.Document.FileName, delivered.Document.FileSize, delivered.Caption)
}
