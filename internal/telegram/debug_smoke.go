package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

// DebugTextSmokeResult summarizes a local synthetic Telegram text turn.
// It is intentionally compact so debug commands can assert milestone behavior
// without exposing raw tool arguments or secrets.
type DebugTextSmokeResult struct {
	UserID                   string
	Prompt                   string
	ToolCalls                []string
	CalledExecuteCode        bool
	Contains5050             bool
	ContainsArtifactMetadata bool
	ArtifactFilenames        []string
	DocumentSends            []DebugDocumentSend
	FinalText                string
}

// DebugDocumentSend records metadata for documents successfully delivered by
// the bot during a debug smoke. It never stores file bodies.
type DebugDocumentSend struct {
	Seq       uint64
	Filename  string
	Caption   string
	SizeBytes int
}

// RunDebugTextSmoke injects one synthetic private text message into this Bot
// and runs the normal conversation handler. It uses the real bot API for sends
// and edits, so callers should point userID at an allowlisted operator.
func (b *Bot) RunDebugTextSmoke(ctx context.Context, userID int64, username, prompt string) (DebugTextSmokeResult, error) {
	if b == nil || b.bot == nil {
		return DebugTextSmokeResult{}, errors.New("telegram debug smoke: bot is not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return DebugTextSmokeResult{}, errors.New("telegram debug smoke: prompt is required")
	}
	userIDString := strconv.FormatInt(userID, 10)
	if !b.isAllowlisted(userIDString) {
		return DebugTextSmokeResult{}, fmt.Errorf("telegram debug smoke: user %s is not allowlisted", userIDString)
	}

	update := tele.Update{
		ID: int(time.Now().Unix() % 1_000_000_000),
		Message: &tele.Message{
			ID:       int(time.Now().Unix() % 1_000_000),
			Unixtime: time.Now().Unix(),
			Sender: &tele.User{
				ID:       userID,
				Username: username,
			},
			Chat: &tele.Chat{
				ID:       userID,
				Type:     tele.ChatPrivate,
				Username: username,
			},
			Text: prompt,
		},
	}
	c := tele.NewContext(b.bot, update)
	docSeq := b.debugDocSeq.Load()

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.handleConversation(c)
	}()

	select {
	case <-ctx.Done():
		return DebugTextSmokeResult{UserID: userIDString, Prompt: prompt}, ctx.Err()
	case <-done:
	}

	ctxVal, ok := b.ctxMap.Load(userIDString)
	if !ok {
		return DebugTextSmokeResult{UserID: userIDString, Prompt: prompt}, errors.New("telegram debug smoke: conversation context missing after turn")
	}
	convCtx, ok := ctxVal.(*conversation.Context)
	if !ok || convCtx == nil {
		return DebugTextSmokeResult{UserID: userIDString, Prompt: prompt}, errors.New("telegram debug smoke: invalid conversation context after turn")
	}
	result := debugTextSmokeResultFromMessages(userIDString, prompt, convCtx.Messages())
	result.DocumentSends = b.debugDocumentSendsAfter(docSeq)
	return result, nil
}

func debugTextSmokeResultFromMessages(userID, prompt string, messages []llm.Message) DebugTextSmokeResult {
	result := DebugTextSmokeResult{
		UserID: userID,
		Prompt: prompt,
	}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			result.ToolCalls = append(result.ToolCalls, call.Name)
			if call.Name == "execute_code" {
				result.CalledExecuteCode = true
			}
		}
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			result.FinalText = strings.TrimSpace(msg.Content)
		}
		if strings.Contains(msg.Content, "5050") {
			result.Contains5050 = true
		}
		for _, name := range artifactFilenamesFromToolContent(msg.Content) {
			result.ContainsArtifactMetadata = true
			result.ArtifactFilenames = append(result.ArtifactFilenames, name)
		}
	}
	return result
}

func artifactFilenamesFromToolContent(content string) []string {
	if !strings.Contains(content, "artifacts:") {
		return nil
	}
	var names []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		name, _, ok := strings.Cut(rest, " ")
		if !ok {
			name = rest
		}
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
