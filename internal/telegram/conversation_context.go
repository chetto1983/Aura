package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/search"
)

type turnRetrievalCapsule struct {
	Text                 string
	HasEvidence          bool
	SuppressSearchMemory bool
}

func composeTurnRetrievalCapsule(ctx context.Context, repo search.Searcher, _ string, userText string, timeoutMS int, logger *slog.Logger, userID string) turnRetrievalCapsule {
	route := retrievalRouteForTurn(userText)
	if route == "minimal" {
		return turnRetrievalCapsule{}
	}
	searchContext := runSpeculativeSearch(ctx, repo, userText, timeoutMS, logger, userID)
	hasEvidence := strings.TrimSpace(searchContext) != ""
	if strings.TrimSpace(searchContext) == "" && route == "produce" {
		searchContext = "No compact wiki evidence was found for this document request. Use the user request directly unless broader Aura memory search is still exposed."
	}
	text := conversation.ComposeRetrievalCapsule(conversation.RetrievalCapsuleInput{
		UserText:      userText,
		SearchContext: searchContext,
		Route:         route,
	})
	return turnRetrievalCapsule{
		Text:                 text,
		HasEvidence:          hasEvidence,
		SuppressSearchMemory: route == "produce" && (hasEvidence || documentTurnCanUsePromptOnly(userText)),
	}
}

func retrievalRouteForTurn(userText string) string {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return "minimal"
	}
	for _, needle := range []string{
		"crea documento", "creami documento", "documento word", "docx", "pdf", "xlsx",
		"riassumi i documenti", "riepilogo dei documenti", "documenti e note",
	} {
		if strings.Contains(text, needle) {
			return "produce"
		}
	}
	for _, needle := range []string{
		"memoria", "memory", "ricordi", "ricord", "wiki", "source", "fonti", "fonte",
		"archivio", "conversaz", "decisioni", "preferenze", "note", "documenti",
		"cosa resta", "cosa sai", "cosa conosci", "come funziona aura",
		"picobot", "perche aura", "perche' aura",
		"grafo", "graph", "wiki graph", "mappa", "collegamenti",
		"log", "ultime modifiche", "cosa e cambiato",
	} {
		if strings.Contains(text, needle) {
			return "retrieve"
		}
	}
	return "minimal"
}

func documentTurnCanUsePromptOnly(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	for _, needle := range []string{
		"documenti e note", "documenti disponibili", "note disponibili",
		"in aura", "memoria", "memory", "wiki", "source", "fonti", "fonte",
		"archivio", "conversaz", "riassumi i documenti", "riepilogo dei documenti",
	} {
		if strings.Contains(text, needle) {
			return false
		}
	}
	return true
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
