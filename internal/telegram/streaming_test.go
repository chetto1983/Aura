package telegram

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

type telegramAPICall struct {
	Method string
	Body   map[string]any
}

var fakeTelegramPDFBytes = []byte("%PDF-1.7\n% aura telegram document test\n")

func TestConsumeStreamEditsPlaceholderAndSuppressesDoubleSend(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "hello",
	}})
	content := "**bold** " + strings.Repeat("x", streamingMinThreshold)
	ch := make(chan llm.Token, 2)
	ch <- llm.Token{Content: content}
	ch <- llm.Token{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}}
	close(ch)

	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resp, delivered, err := b.consumeStream(ctx, ch, "123", &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true after placeholder edit")
	}
	if resp.Content != content || resp.Usage.TotalTokens != 7 {
		t.Fatalf("response = %+v", resp)
	}
	if got := countTelegramMethods(calls, "editMessageText"); got != 2 {
		t.Fatalf("editMessageText calls = %d, want 2 (initial placeholder edit + final edit)", got)
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 0 {
		t.Fatalf("sendMessage calls = %d, want 0", got)
	}
	for _, call := range calls {
		if call.Method != "editMessageText" {
			continue
		}
		if _, ok := call.Body["parse_mode"]; ok {
			t.Fatalf("editMessageText sent parse_mode, want entities: %+v", call.Body)
		}
		if _, ok := call.Body["entities"]; !ok {
			t.Fatalf("editMessageText missing entities: %+v", call.Body)
		}
	}
}

func TestConsumeStreamRendersReasoningAboveContent(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "hi",
	}})

	answer := strings.Repeat("ok ", streamingMinThreshold)
	ch := make(chan llm.Token, 4)
	ch <- llm.Token{Reasoning: "Considering options carefully so the answer is concise."}
	ch <- llm.Token{Reasoning: " The user wants a short reply."}
	ch <- llm.Token{Content: answer}
	ch <- llm.Token{Done: true, Usage: llm.TokenUsage{TotalTokens: 12, ReasoningTokens: 5}}
	close(ch)

	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resp, delivered, err := b.consumeStream(ctx, ch, "123", &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true after CoT + content edit")
	}
	if !strings.Contains(resp.Reasoning, "Considering options") || !strings.Contains(resp.Reasoning, "short reply") {
		t.Fatalf("Response.Reasoning lost streamed deltas: %q", resp.Reasoning)
	}
	if resp.Usage.ReasoningTokens != 5 {
		t.Fatalf("Usage.ReasoningTokens = %d, want 5", resp.Usage.ReasoningTokens)
	}
	// At least one edit body must carry the 🧠 prefix + italic CoT block so
	// the user sees the model thinking live, even before content arrives.
	var sawCoTPrefix bool
	var finalEdit string
	for _, call := range calls {
		if call.Method != "editMessageText" {
			continue
		}
		body, _ := call.Body["text"].(string)
		if strings.Contains(body, "🧠") && strings.Contains(body, "Considering options") {
			sawCoTPrefix = true
		}
		finalEdit = body
	}
	if !sawCoTPrefix {
		t.Fatalf("no editMessageText carried the live CoT prefix: %+v", calls)
	}
	// Final edit must contain ONLY the answer — the CoT is scaffolding,
	// once content is complete the user sees a clean message and the
	// reasoning lives on resp.Reasoning for the archive.
	if strings.Contains(finalEdit, "🧠") || strings.Contains(finalEdit, "Considering options") || strings.Contains(finalEdit, "short reply") {
		t.Fatalf("final edit still carries CoT prefix, want clean answer: %q", finalEdit)
	}
	if !strings.Contains(finalEdit, "ok") {
		t.Fatalf("final edit missing answer body: %q", finalEdit)
	}
}

func TestComposeStreamingMessageShape(t *testing.T) {
	if got := composeStreamingMessage("", ""); got != "" {
		t.Errorf("empty/empty = %q, want empty", got)
	}
	if got := composeStreamingMessage("", "hello"); got != "hello" {
		t.Errorf("empty/content = %q, want hello", got)
	}
	if got := composeStreamingMessage("thinking", ""); got != "🧠 _thinking_" {
		t.Errorf("cot/empty = %q", got)
	}
	if got := composeStreamingMessage("thinking", "answer"); got != "🧠 _thinking_\n\nanswer" {
		t.Errorf("cot/content = %q", got)
	}
}

func TestConsumeStreamToolCallDoesNotMarkDelivered(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
	}})
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Done: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}}
	close(ch)

	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resp, delivered, err := b.consumeStream(ctx, ch, "123", &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if delivered {
		t.Fatal("delivered = true, want false for tool call response")
	}
	if !resp.HasToolCalls || len(resp.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %+v", resp.ToolCalls)
	}
	if len(calls) != 0 {
		t.Fatalf("telegram calls = %+v, want none before tool result", calls)
	}
}

func newTelegramAPIServer(t *testing.T, calls *[]telegramAPICall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/file/") {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(fakeTelegramPDFBytes)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)
		*calls = append(*calls, telegramAPICall{Method: method, Body: body})
		w.Header().Set("Content-Type", "application/json")
		if method == "getFile" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"doc-1","file_path":"documents/test.pdf","file_size":36}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":123},"date":1760000000,"text":"ok"}}`))
	}))
}

func countTelegramMethods(calls []telegramAPICall, method string) int {
	var count int
	for _, call := range calls {
		if call.Method == method {
			count++
		}
	}
	return count
}
