package telegram

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/search"
)

func composeTurnMemoryPack(ctx context.Context, repo search.Searcher, wikiDir, userText string, timeoutMS int, logger *slog.Logger, userID string) string {
	searchContext := runSpeculativeSearch(ctx, repo, userText, timeoutMS, logger, userID)
	var graphContext, recentLog string
	if strings.TrimSpace(wikiDir) != "" {
		graphContext = readWorkspaceText(filepath.Join(wikiDir, "graph", "context.md"), 4096)
		recentLog = recentWikiLog(filepath.Join(wikiDir, "log.md"), 4096, 12)
	}
	return conversation.ComposeMemoryPack(conversation.MemoryPackInput{
		UserText:      userText,
		SearchContext: searchContext,
		GraphContext:  graphContext,
		RecentLog:     recentLog,
	})
}

func runSpeculativeSearch(ctx context.Context, repo search.Searcher, userText string, timeoutMS int, logger *slog.Logger, userID string) string {
	if repo == nil || !repo.IsIndexed() {
		return ""
	}
	timeout := speculativeSearchTimeout(timeoutMS)
	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	results, err := repo.Search(searchCtx, userText, 5)
	if err == nil && len(results) > 0 {
		return search.FormatResults(results)
	}
	if err != nil && logger != nil {
		logger.Debug("speculative wiki search failed",
			"user_id", userID,
			"error", err,
			"timeout_ms", timeout.Milliseconds(),
			"elapsed_ms", time.Since(start).Milliseconds(),
		)
	}
	return ""
}

func speculativeSearchTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		timeoutMS = config.DefaultSpeculativeSearchTimeoutMS
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func readWorkspaceText(path string, maxBytes int) string {
	if strings.TrimSpace(path) == "" || maxBytes <= 0 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return strings.TrimSpace(text)
}

func recentWikiLog(path string, maxBytes, maxLines int) string {
	text := readWorkspaceText(path, maxBytes)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, maxLines)
	for i := len(lines) - 1; i >= 0 && len(kept) < maxLines; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		kept = append(kept, line)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "\n")
}
