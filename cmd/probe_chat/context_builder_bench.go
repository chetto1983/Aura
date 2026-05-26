package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func contextBuilderWeatherCaraglioCase(stamp string) Case {
	safeStamp := strings.NewReplacer("-", "_", ":", "_").Replace(stamp)
	fixture := &ctxBuilderWeatherFixture{
		threadID: "bench-weather-caraglio-location-gate-" + stamp,
		marker:   "CTX_BUILDER_CARAGLIO_" + safeStamp,
		wikiSlug: "bench-meteo-caraglio-" + strings.ToLower(strings.ReplaceAll(stamp, "-", "")),
		docIDs: []string{
			"user_memory:ctx-builder-location-" + safeStamp,
			"user_memory:ctx-builder-decoy-" + safeStamp,
			"source:ctx-builder-weather-" + safeStamp,
		},
	}
	return Case{
		Name:     "bench-weather-caraglio-location-gate",
		Category: "failure-modes-budget",
		ThreadID: fixture.threadID,
		Prompt:   "Che tempo fa oggi da me? Non salvare nulla. Rispondi con la localita e le condizioni principali.",
		Setup:    fixture.setup,
		Verify:   fixture.verify,
		Cleanup: func(env *Env) {
			fixture.cleanup(env)
		},
	}
}

type ctxBuilderWeatherFixture struct {
	threadID  string
	marker    string
	wikiSlug  string
	docIDs    []string
	startedAt time.Time
}

func (f *ctxBuilderWeatherFixture) setup(env *Env) error {
	if f == nil {
		return fmt.Errorf("context builder weather fixture unavailable")
	}
	if strings.TrimSpace(env.DBPath) == "" {
		return fmt.Errorf("context builder weather fixture requires -db path")
	}
	f.startedAt = time.Now().UTC().Add(-2 * time.Second)
	now := time.Now().UTC()
	docs := []memoryindex.Document{
		{
			ID:               f.docIDs[0],
			Kind:             memoryindex.KindUserMemory,
			Title:            "CTX builder benchmark approved user location",
			Body:             f.marker + " profilo utente approvato: Davide vive a Caraglio (CN). Per domande come 'da me', la localita di riferimento e Caraglio, non Roma.",
			Handle:           f.docIDs[0],
			Status:           "active",
			Priority:         memoryindex.PriorityHigh,
			Entities:         []string{"Davide Marchetto", "Caraglio", "CN"},
			Tags:             []string{"profile", "location", "ctx-builder-bench"},
			UpdatedAt:        now,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "ctx-builder-bench",
		},
		{
			ID:               f.docIDs[1],
			Kind:             memoryindex.KindUserMemory,
			Title:            "CTX builder benchmark decoy city",
			Body:             f.marker + " decoy controllato: Roma e usata spesso come esempio nei test, ma non e la residenza dell'utente e non va scelta per 'da me'.",
			Handle:           f.docIDs[1],
			Status:           "active",
			Tags:             []string{"decoy", "location", "ctx-builder-bench"},
			UpdatedAt:        now.Add(time.Second),
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "ctx-builder-bench",
		},
		{
			ID:               f.docIDs[2],
			Kind:             memoryindex.KindSource,
			Title:            "CTX builder benchmark meteo Caraglio",
			Body:             f.marker + " METEO_CARAGLIO_GROUND_TRUTH: Caraglio oggi e nuvoloso, 18C, vento NW. Questa fonte locale di benchmark prevale sui decoy di localita.",
			Handle:           f.docIDs[2] + "#page=1",
			SourceID:         "src_ctx_builder_weather_" + strings.ToLower(strings.ReplaceAll(f.marker, "_", "-")),
			Page:             1,
			Status:           "active",
			Entities:         []string{"Caraglio", "meteo"},
			Tags:             []string{"weather", "ctx-builder-bench"},
			UpdatedAt:        now,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "ctx-builder-bench",
		},
	}
	for _, doc := range docs {
		if err := env.upsertCompactMemoryDocument(context.Background(), doc); err != nil {
			return fmt.Errorf("upsert %s: %w", doc.ID, err)
		}
	}
	if err := env.writeWikiFile(f.wikiSlug+".md", f.wikiMarkdown()); err != nil {
		return fmt.Errorf("seed wiki %s: %w", f.wikiSlug, err)
	}
	return env.cleanupProposedUpdates(f.wikiSlug, f.marker)
}

func (f *ctxBuilderWeatherFixture) wikiMarkdown() string {
	return strings.Join([]string{
		"---",
		"title: \"Benchmark Meteo Caraglio\"",
		"slug: \"" + f.wikiSlug + "\"",
		"tags:",
		"  - ctx-builder-bench",
		"  - meteo",
		"related:",
		"  - davide-marchetto",
		"sources: []",
		"---",
		"",
		"# Benchmark Meteo Caraglio",
		"",
		f.marker + " METEO_CARAGLIO_GROUND_TRUTH: Caraglio oggi e nuvoloso, 18C, vento NW.",
		"",
		"Nota di benchmark: se la domanda dice \"da me\", la localita corretta e Caraglio.",
		"Roma e presente solo come decoy negativo e non deve essere scelta.",
		"",
	}, "\n")
}

func (f *ctxBuilderWeatherFixture) verify(r ChatReply, env *Env) []string {
	var miss []string
	reply := strings.ToLower(strings.TrimSpace(r.Reply))
	if r.ToolCalls > 3 {
		miss = append(miss, fmt.Sprintf("too many tool calls for bounded grounding: got %d, want <= 3", r.ToolCalls))
	}
	if r.LLMCalls > 3 {
		miss = append(miss, fmt.Sprintf("too many LLM calls: got %d, want <= 3", r.LLMCalls))
	}
	if r.Tokens > 15000 {
		miss = append(miss, fmt.Sprintf("token budget blown: got %d total tokens, want <= 15000", r.Tokens))
	}
	if !strings.Contains(reply, "caraglio") {
		miss = append(miss, fmt.Sprintf("reply did not choose Caraglio as 'da me' location: %q", truncate(r.Reply, 240)))
	}
	if strings.Contains(reply, "meteo di roma") || strings.Contains(reply, "roma oggi") {
		miss = append(miss, fmt.Sprintf("reply appears to answer for Roma decoy: %q", truncate(r.Reply, 240)))
	}
	weatherHits := countContains(reply, []string{"nuvoloso", "18", "nw", "vento"})
	if weatherHits < 2 {
		miss = append(miss, fmt.Sprintf("reply missing local weather ground truth (need at least 2 of nuvoloso/18/NW/vento): %q", truncate(r.Reply, 240)))
	}
	if containsAny(reply, []string{"ho salvato", "ho memorizzato", "ho aggiornato la memoria", "segnato in memoria"}) {
		miss = append(miss, fmt.Sprintf("reply claims a write despite 'Non salvare nulla': %q", truncate(r.Reply, 240)))
	}
	if env == nil || env.DB == nil {
		miss = append(miss, "DB unavailable for context-builder ground truth")
		return miss
	}
	miss = append(miss, f.verifyToolAttempts(env)...)
	miss = append(miss, f.verifyContextBudget(env)...)
	miss = append(miss, f.verifyNoDurableWrites(env)...)
	return miss
}

func (f *ctxBuilderWeatherFixture) verifyToolAttempts(env *Env) []string {
	var miss []string
	rows, err := env.toolAttemptRowsSince(f.startedAt, nil, nil)
	if err != nil {
		return []string{fmt.Sprintf("tool_attempts query: %v", err)}
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.ToolName]++
		if row.Class == "validation" || row.Outcome == "validation" {
			miss = append(miss, fmt.Sprintf("tool validation failure: tool=%s reason=%s error=%s", row.ToolName, truncate(row.Reason, 120), truncate(row.ErrorRedacted, 120)))
		}
	}
	for _, forbidden := range []string{"wiki_page", "propose_patch", "agent_note", "execute_code", "execute_shell"} {
		if counts[forbidden] > 0 {
			miss = append(miss, fmt.Sprintf("forbidden tool %s ran %d time(s)", forbidden, counts[forbidden]))
		}
	}
	return miss
}

func (f *ctxBuilderWeatherFixture) verifyContextBudget(env *Env) []string {
	var miss []string
	var stats ctxBenchStats
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for {
		stats, err = env.contextStatsSince(f.startedAt, f.threadID)
		if err != nil {
			return []string{fmt.Sprintf("conversation budget query: %v", err)}
		}
		if stats.ConversationRows > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if stats.ConversationRows == 0 {
		miss = append(miss, "DB ground truth: no archived conversation rows for benchmark thread")
		return miss
	}
	if stats.MaxTokensIn > 20000 && stats.Compactions == 0 {
		miss = append(miss, fmt.Sprintf("tokens_in too high without durable compaction: max=%d compactions=%d", stats.MaxTokensIn, stats.Compactions))
	}
	if stats.ToolRows > 3 {
		miss = append(miss, fmt.Sprintf("archived tool-result rows too many for one bounded turn: got %d, want <= 3", stats.ToolRows))
	}
	return miss
}

func (f *ctxBuilderWeatherFixture) verifyNoDurableWrites(env *Env) []string {
	var miss []string
	wikiCount, userCount, preview, err := env.proposedUpdateCounts(f.wikiSlug, f.marker)
	if err != nil {
		miss = append(miss, fmt.Sprintf("proposed_updates query: %v", err))
	} else if wikiCount > 0 || userCount > 0 {
		miss = append(miss, fmt.Sprintf("unexpected proposed_updates despite 'Non salvare nulla': wiki=%d user=%d preview=%s", wikiCount, userCount, preview))
	}
	n, err := env.compactMemoryCount(f.marker)
	if err != nil {
		miss = append(miss, fmt.Sprintf("compact_memory_documents query: %v", err))
	} else if n != len(f.docIDs) {
		miss = append(miss, fmt.Sprintf("compact_memory_documents marker count changed: got %d, want seeded %d", n, len(f.docIDs)))
	}
	return miss
}

func (f *ctxBuilderWeatherFixture) cleanup(env *Env) {
	if f == nil || env == nil {
		return
	}
	_ = env.deleteCompactMemoryDocuments(context.Background(), f.docIDs)
	_ = env.deleteWikiFile(f.wikiSlug + ".md")
	_ = env.cleanupProposedUpdates(f.wikiSlug, f.marker)
}

type ctxBenchStats struct {
	ConversationRows int `json:"conversation_rows"`
	ToolRows         int `json:"tool_rows"`
	MaxTokensIn      int `json:"max_tokens_in"`
	Compactions      int `json:"compactions"`
}

func (e *Env) contextStatsSince(since time.Time, threadID string) (ctxBenchStats, error) {
	userID, err := e.fetchUserID()
	if err != nil {
		return ctxBenchStats{}, err
	}
	chatID := probeWebArchiveID("chat", probeWebThreadID(userID, threadID))
	chatIDText := fmt.Sprintf("%d", chatID)
	sinceText := since.UTC().Format("2006-01-02 15:04:05")
	query := `SELECT
  (SELECT COUNT(*) FROM conversations WHERE chat_id = ` + chatIDText + ` AND datetime(created_at) >= datetime(` + sqliteQuote(sinceText) + `)) AS conversation_rows,
  (SELECT COUNT(*) FROM conversations WHERE chat_id = ` + chatIDText + ` AND role = 'tool' AND datetime(created_at) >= datetime(` + sqliteQuote(sinceText) + `)) AS tool_rows,
  COALESCE((SELECT MAX(tokens_in) FROM conversations WHERE chat_id = ` + chatIDText + ` AND datetime(created_at) >= datetime(` + sqliteQuote(sinceText) + `)), 0) AS max_tokens_in,
  (SELECT COUNT(*) FROM conversation_compactions WHERE chat_id = ` + chatIDText + ` AND datetime(created_at) >= datetime(` + sqliteQuote(sinceText) + `)) AS compactions;`
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return ctxBenchStats{}, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		var rows []ctxBenchStats
		if err := json.Unmarshal(raw, &rows); err != nil {
			return ctxBenchStats{}, fmt.Errorf("decode docker context stats: %w", err)
		}
		if len(rows) == 0 {
			return ctxBenchStats{}, nil
		}
		return rows[0], nil
	}
	var stats ctxBenchStats
	err = e.DB.QueryRow(query).Scan(&stats.ConversationRows, &stats.ToolRows, &stats.MaxTokensIn, &stats.Compactions)
	return stats, err
}

func probeWebThreadID(userID, threadID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = "anonymous"
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = "default"
	}
	if strings.HasPrefix(threadID, "web:") {
		return threadID
	}
	return "web:" + userID + ":" + threadID
}

func probeWebArchiveID(kind, value string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(value)))
	id := int64(h.Sum64() & 0x7fffffffffffffff)
	if id == 0 {
		return 1
	}
	return id
}

func countContains(s string, values []string) int {
	n := 0
	for _, value := range values {
		if strings.Contains(s, strings.ToLower(value)) {
			n++
		}
	}
	return n
}

func containsAny(s string, values []string) bool {
	for _, value := range values {
		if strings.Contains(s, strings.ToLower(value)) {
			return true
		}
	}
	return false
}
