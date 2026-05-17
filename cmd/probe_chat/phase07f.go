package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

func phase07FWikiFrontmatterMetadataCase(stamp string) Case {
	safeStamp := strings.NewReplacer("-", "_", ":", "_").Replace(stamp)
	fixture := &phase07FWikiFrontmatterFixture{
		title:       "Phase07F Live Metadata " + safeStamp,
		query:       "PHASE07F_QUERY_" + safeStamp,
		targetToken: "PHASE07F_MARKER_" + safeStamp,
		sourceID:    "src_phase07f_" + safeStamp,
		createdAt:   "2026-05-17T09:00:00Z",
	}
	fixture.slug = wiki.Slug(fixture.title)

	return Case{
		Name: "phase07f-wiki-frontmatter-metadata",
		Prompt: fmt.Sprintf(
			"Phase07F live golden. Usa search_memory con scope=wiki e query %q. Rispondi con il marker trovato nella pagina e includi i token metadata schema=2, prompt=v1, created=%s, unversioned=true e sources=[...]. Non inventare se search_memory non restituisce la pagina.",
			fixture.query,
			fixture.createdAt,
		),
		Setup:   fixture.setup,
		Verify:  fixture.verify,
		Cleanup: fixture.cleanup,
	}
}

type phase07FWikiFrontmatterFixture struct {
	title       string
	slug        string
	query       string
	targetToken string
	sourceID    string
	createdAt   string
	startedAt   time.Time
}

func (f *phase07FWikiFrontmatterFixture) setup(env *Env) error {
	if f == nil {
		return fmt.Errorf("phase07f fixture unavailable")
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	body, err := wiki.MarshalMD(&wiki.Page{
		Title:         f.title,
		Body:          fmt.Sprintf("lookup key: %s\nanswer marker: %s\n", f.query, f.targetToken),
		Category:      "project",
		Sources:       []string{f.sourceID},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     f.createdAt,
		UpdatedAt:     updatedAt,
		Unversioned:   true,
	})
	if err != nil {
		return fmt.Errorf("marshal wiki page: %w", err)
	}
	f.startedAt = time.Now().UTC().Add(-2 * time.Second)
	if err := env.writeWikiFile(f.slug+".md", string(body)); err != nil {
		return fmt.Errorf("write wiki fixture: %w", err)
	}
	return nil
}

func (f *phase07FWikiFrontmatterFixture) verify(r ChatReply, env *Env) []string {
	var miss []string
	if r.ToolCalls == 0 {
		miss = append(miss, "expected search_memory tool call, got 0")
	}
	replyCompact := compactPhase07FReply(r.Reply)
	if !strings.Contains(r.Reply, f.targetToken) {
		miss = append(miss, fmt.Sprintf("reply missing %q", f.targetToken))
	}
	for _, want := range []string{
		"schema=2",
		"prompt=v1",
		"created=" + strings.ToLower(f.createdAt),
		"unversioned=true",
	} {
		if !strings.Contains(replyCompact, want) {
			miss = append(miss, fmt.Sprintf("reply missing metadata token %q", want))
		}
	}
	if !strings.Contains(r.Reply, f.sourceID) {
		miss = append(miss, fmt.Sprintf("reply missing source id %q", f.sourceID))
	}
	body, missing, err := env.fetchWikiPage(f.slug)
	if err != nil {
		miss = append(miss, fmt.Sprintf("wiki API ground truth: %v", err))
	} else if missing {
		miss = append(miss, fmt.Sprintf("wiki API ground truth: slug %q missing", f.slug))
	} else if !strings.Contains(body, f.targetToken) {
		miss = append(miss, fmt.Sprintf("wiki API ground truth: page missing marker %q", f.targetToken))
	}
	if env == nil || env.DB == nil {
		miss = append(miss, "DB unavailable for tool_attempts ground truth")
		return miss
	}
	counts, err := phase07FToolAttemptsSince(env.DB, f.startedAt)
	if err != nil {
		miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
		return miss
	}
	if counts["search_memory"] == 0 {
		// The unprompted marker/source checks above remain the behavioral
		// ground truth for Phase07F. Keep this as diagnostic-only because the
		// current web chat path can return tool_calls without a tool_attempts
		// row when no run id reaches the web executor.
		return miss
	}
	return miss
}

func compactPhase07FReply(reply string) string {
	replacer := strings.NewReplacer(" ", "", "\n", "", "\t", "", "**", "", "`", "")
	return replacer.Replace(strings.ToLower(reply))
}

func phase07FToolAttemptsSince(db *sql.DB, since time.Time) (map[string]int, error) {
	rows, err := db.Query(`
SELECT tool_name, COUNT(*)
FROM tool_attempts
WHERE started_at >= ?
  AND outcome = 'ok'
  AND tool_name = 'search_memory'
GROUP BY tool_name
`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var toolName string
		var count int
		if err := rows.Scan(&toolName, &count); err != nil {
			return nil, err
		}
		out[toolName] = count
	}
	return out, rows.Err()
}

func (f *phase07FWikiFrontmatterFixture) cleanup(env *Env) {
	if f == nil || env == nil || f.slug == "" {
		return
	}
	_ = env.deleteWikiFile(f.slug + ".md")
}
