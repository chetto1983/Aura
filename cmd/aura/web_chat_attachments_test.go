package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/testutil"
)

func TestWebChatStreamCarriesSourceAttachments(t *testing.T) {
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	runStore, err := runs.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	llmClient := &fakeStreamingWebChatLLM{}
	shared, err := newSharedChatHub(
		&config.Config{LLMModel: "fake-stream", AuraBotMaxIterations: 2},
		&telegram.Deps{
			LLM:      llmClient,
			RunStore: runStore,
			Tools:    toolregistry.NewRegistry(logger),
			Logger:   logger,
		},
		nil,
		logger,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("newSharedChatHub: %v", err)
	}
	svc := newWebStreamChatService(shared.hub, shared.webStreamRouter)
	ctx := api.WithChatAttachments(context.Background(), []chat.AttachmentRef{{
		SourceID: "src_0123456789abcdef",
		Filename: "brief.pdf",
		MimeType: "application/pdf",
		Status:   "stored",
	}})

	var buf bytes.Buffer
	if err := svc.ChatStream(ctx, "alice", "sources", "read the upload", &buf, nil); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	_, _, requests := llmClient.Snapshot()
	if len(requests) != 1 || !containsMessageFragment(requests[0].Messages, "source_id: src_0123456789abcdef") {
		t.Fatalf("request missing source attachment: %+v", requests)
	}
	if !containsMessageFragment(requests[0].Messages, "filename: brief.pdf") {
		t.Fatalf("request missing source filename: %+v", requests[0].Messages)
	}
}

func containsMessageFragment(messages []llm.Message, fragment string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, fragment) {
			return true
		}
	}
	return false
}
