package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/tools"
	tele "gopkg.in/telebot.v4"
)

func TestBotCommandsMenu(t *testing.T) {
	commands := botCommands()
	got := make([]string, 0, len(commands))
	for _, command := range commands {
		got = append(got, command.Text)
		if strings.TrimSpace(command.Description) == "" {
			t.Fatalf("command %q has empty description", command.Text)
		}
	}
	want := []string{"start", "status", "help", "clear", "tools", "login"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestInstallBotCommandsCallsTelegramMenuAPI(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{
		bot:    tb,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	b.installBotCommands()

	if got := countTelegramMethods(calls, "setMyCommands"); got != 1 {
		t.Fatalf("setMyCommands calls = %d, want 1 (calls=%+v)", got, calls)
	}
	commands, ok := calls[0].Body["commands"].([]any)
	if !ok || len(commands) != len(botCommands()) {
		t.Fatalf("commands body = %#v, want %d commands", calls[0].Body["commands"], len(botCommands()))
	}
}

func TestOnClearDeletesAllowlistedConversation(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	store := agent.NewSessionStore()
	session, _ := store.Begin("123", conversation.Config{})
	session.Conversation().AddUserMessage("vecchio contesto")
	store.StoreSnapshot("123", agent.Snapshot{PromptVersion: "v-test"})
	b := &Bot{
		bot:      tb,
		cfg:      &config.Config{Allowlist: []string{"123"}, AllowlistConfigured: true},
		sessions: store,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "/clear",
	}})

	if err := b.onClear(ctx); err != nil {
		t.Fatalf("onClear() error = %v, want nil", err)
	}
	if _, ok := store.Load("123"); ok {
		t.Fatal("conversation survived /clear")
	}
	if store.IsActive("123") {
		t.Fatal("active marker survived /clear")
	}
	if _, ok := store.Snapshot("123"); ok {
		t.Fatal("snapshot survived /clear")
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", got)
	}
	text, _ := calls[0].Body["text"].(string)
	if !strings.Contains(text, "Conversazione cancellata") {
		t.Fatalf("clear reply = %q", text)
	}
}

func TestOnClearIgnoresUnauthorizedUser(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	store := agent.NewSessionStore()
	session, _ := store.Begin("999", conversation.Config{})
	session.Conversation().AddUserMessage("keep")
	b := &Bot{
		bot:      tb,
		cfg:      &config.Config{Allowlist: []string{"123"}, AllowlistConfigured: true},
		sessions: store,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 999},
		Chat:   &tele.Chat{ID: 999},
		Text:   "/clear",
	}})

	if err := b.onClear(ctx); err != nil {
		t.Fatalf("onClear() error = %v, want nil", err)
	}
	if _, ok := store.Load("999"); !ok {
		t.Fatal("unauthorized /clear deleted conversation")
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 0 {
		t.Fatalf("sendMessage calls = %d, want 0", got)
	}
}

func TestOnHelpRepliesToAllowlistedUser(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{
		bot:    tb,
		cfg:    &config.Config{Allowlist: []string{"123"}, AllowlistConfigured: true},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "/help",
	}})

	if err := b.onHelp(ctx); err != nil {
		t.Fatalf("onHelp() error = %v, want nil", err)
	}
	text, _ := calls[0].Body["text"].(string)
	for _, want := range []string{"/status", "/tools", "/clear", "/login"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help reply = %q, missing %q", text, want)
		}
	}
}

func TestOnToolsListsModelVisibleTools(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registry.Register(commandFakeTool{name: "execute_code"})
	registry.Register(commandFakeTool{name: "search_memory"})
	b := &Bot{
		bot:    tb,
		cfg:    &config.Config{Allowlist: []string{"123"}, AllowlistConfigured: true},
		tools:  registry,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "/tools",
	}})

	if err := b.onTools(ctx); err != nil {
		t.Fatalf("onTools() error = %v, want nil", err)
	}
	text, _ := calls[0].Body["text"].(string)
	for _, want := range []string{"execute_code", "search_memory"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tools reply = %q, missing %q", text, want)
		}
	}
}

type commandFakeTool struct {
	name string
}

func (t commandFakeTool) Name() string { return t.name }

func (t commandFakeTool) Description() string { return t.name + " test tool" }

func (t commandFakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t commandFakeTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}
