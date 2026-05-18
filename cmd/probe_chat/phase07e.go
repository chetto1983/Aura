package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func phase07ESourceSpanReadCase(stamp string) Case {
	safeStamp := strings.NewReplacer("-", "_", ":", "_").Replace(stamp)
	fixture := &phase07ESourceSpanFixture{
		query:       "phase07e source span " + safeStamp,
		filename:    "phase07e-source-span-" + safeStamp + ".txt",
		prefixToken: "PHASE07E_PREFIX_LEAK_" + safeStamp,
		targetToken: "PHASE07E_TARGET_SPAN_" + safeStamp,
		suffixToken: "PHASE07E_SUFFIX_LEAK_" + safeStamp,
	}
	fixture.content = fmt.Sprintf("%s\nlookup key: %s\n%s is the exact byte span\n%s\n", fixture.prefixToken, fixture.query, fixture.targetToken, fixture.suffixToken)

	return Case{
		Name:     "phase07e-source-span-read",
		Category: "tools-source",
		Prompt: fmt.Sprintf(
			"Phase07E live golden. Usa prima search_memory con scope=sources e query %q. Poi usa il follow_up source(action=read,...) del risultato per leggere solo lo span. Rispondi esattamente con SPAN=%s e non citare altri token PHASE07E.",
			fixture.query,
			fixture.targetToken,
		),
		Setup:   fixture.setup,
		Verify:  fixture.verify,
		Cleanup: fixture.cleanup,
	}
}

type phase07ESourceSpanFixture struct {
	query       string
	filename    string
	content     string
	prefixToken string
	targetToken string
	suffixToken string
	sourceID    string
	docID       string
	startedAt   time.Time
}

func (f *phase07ESourceSpanFixture) setup(env *Env) error {
	if f == nil {
		return fmt.Errorf("phase07e fixture unavailable")
	}
	if strings.TrimSpace(env.DBPath) == "" {
		return fmt.Errorf("phase07e fixture requires -db path")
	}
	id, _, err := env.uploadSourceFile(f.filename, "text/plain", []byte(f.content))
	if err != nil {
		return fmt.Errorf("upload source: %w", err)
	}
	f.sourceID = id
	f.docID = "source:" + id + "#page=0"

	start := strings.Index(f.content, f.targetToken)
	if start < 0 {
		return fmt.Errorf("target token missing from fixture")
	}
	end := start + len(f.targetToken)

	ctx := context.Background()
	f.startedAt = time.Now().UTC().Add(-2 * time.Second)
	if err := env.upsertCompactMemoryDocument(ctx, memoryindex.Document{
		ID:               f.docID,
		Kind:             memoryindex.KindSource,
		Title:            f.filename,
		Body:             f.content,
		Handle:           f.docID,
		SourceID:         id,
		ChunkIndex:       0,
		ByteStart:        start,
		ByteEnd:          end,
		Status:           "active",
		Tags:             []string{"text", "stored", "phase07e"},
		UpdatedAt:        time.Now().UTC(),
		EmbeddingModelID: "embeddinggemma-300m@256-mrl",
		IndexBuildID:     "phase07e-live-probe",
	}); err != nil {
		return fmt.Errorf("upsert source span doc: %w", err)
	}
	return nil
}

func (f *phase07ESourceSpanFixture) verify(r ChatReply, env *Env) []string {
	var miss []string
	if r.ToolCalls < 2 {
		miss = append(miss, fmt.Sprintf("expected at least 2 tool calls for source search/read, got %d", r.ToolCalls))
	}
	if !strings.Contains(r.Reply, f.targetToken) {
		miss = append(miss, fmt.Sprintf("reply missing target span token %q", f.targetToken))
	}
	for _, reject := range []string{f.prefixToken, f.suffixToken} {
		if strings.Contains(r.Reply, reject) {
			miss = append(miss, fmt.Sprintf("reply leaked outside-span token %q", reject))
		}
	}
	if env == nil || env.DB == nil {
		miss = append(miss, "DB unavailable for tool_attempts ground truth")
		return miss
	}
	counts, err := env.toolAttemptsSince(f.startedAt, "search_memory", "source")
	if err != nil {
		miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
		return miss
	}
	for _, toolName := range []string{"search_memory", "source"} {
		if counts[toolName] == 0 {
			miss = append(miss, fmt.Sprintf("tool_attempts missing ok row for %s since %s", toolName, f.startedAt.Format(time.RFC3339Nano)))
		}
	}
	return miss
}

func (f *phase07ESourceSpanFixture) cleanup(env *Env) {
	if f == nil || env == nil {
		return
	}
	if f.sourceID != "" {
		_ = env.deleteSource(f.sourceID)
	}
	if strings.TrimSpace(env.DBPath) == "" || f.docID == "" {
		return
	}
	_ = env.deleteCompactMemoryDocuments(context.Background(), []string{f.docID})
}
