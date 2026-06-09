//go:build ignore

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type promptRow struct {
	ID     string
	Flags  map[string]bool
	Prompt string
}

type turnRow struct {
	Seq       int
	Role      string
	Content   string
	ToolCalls string
}

type toolGroup struct {
	RequestID  string
	ToolCallID string
	ToolName   string
	Starts     int64
	Ends       int64
	Errors     int64
}

type checkSet struct {
	pass int
	fail int
}

func (c *checkSet) check(id, desc string, ok bool) {
	if ok {
		c.pass++
		fmt.Printf("  PASS %s %s\n", id, desc)
		return
	}
	c.fail++
	fmt.Printf("  FAIL %s %s\n", id, desc)
}

func main() {
	var promptPath, logPath, sinceRaw string
	var minScore, maxToolsPerRequest, minToolEnds int
	flag.StringVar(&promptPath, "prompts", "scripts/fixtures/chat_50_prompts.tsv", "TSV prompt fixture")
	flag.StringVar(&logPath, "log", "", "aura chat stdout/stderr log")
	flag.StringVar(&sinceRaw, "since", "", "RFC3339 lower bound for the run")
	flag.IntVar(&minScore, "min-score", 95, "minimum integer score")
	flag.IntVar(&maxToolsPerRequest, "max-tools-per-request", 8, "maximum completed tools per request_id")
	flag.IntVar(&minToolEnds, "min-tool-ends", 1, "minimum completed tool invocations across the run")
	flag.Parse()

	if sinceRaw == "" {
		fatalf("--since is required")
	}
	since, err := time.Parse(time.RFC3339, sinceRaw)
	if err != nil {
		fatalf("parse --since: %v", err)
	}
	prompts, err := readPrompts(promptPath)
	if err != nil {
		fatalf("read prompts: %v", err)
	}
	if len(prompts) == 0 {
		fatalf("prompt fixture is empty")
	}

	dsn := os.Getenv("AURA_DB_URL")
	if dsn == "" {
		cfg := config.LoadDB()
		dsn = cfg.DB.URL
	}
	if dsn == "" {
		fatalf("AURA_DB_URL is required (or POSTGRES_* must compose one)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer pool.Close()

	convID, err := findConversation(ctx, pool, since, prompts[0].Prompt)
	if err != nil {
		fatalf("find conversation: %v", err)
	}
	turns, err := loadTurns(ctx, pool, convID)
	if err != nil {
		fatalf("load turns: %v", err)
	}
	tools, maxEnds, totalEnds, err := loadToolGroups(ctx, pool, convID)
	if err != nil {
		fatalf("load tool invocations: %v", err)
	}
	toolPayload, err := loadToolPayload(ctx, pool, convID)
	if err != nil {
		fatalf("load tool payload: %v", err)
	}
	logText := ""
	if logPath != "" {
		b, err := os.ReadFile(logPath)
		if err != nil {
			fatalf("read chat log: %v", err)
		}
		logText = string(b)
	}

	fmt.Println("== chat50 DB validation")
	fmt.Println("conversation:", convID)
	checks := &checkSet{}
	today := todayISO()
	validateTurns(checks, prompts, turns, today)
	validateLeaks(checks, turns, toolPayload, logText)
	validateTools(checks, tools, totalEnds, maxEnds, minToolEnds, maxToolsPerRequest)

	total := checks.pass + checks.fail
	score := 0
	if total > 0 {
		score = checks.pass * 100 / total
	}
	fmt.Printf("== tool events: completed=%d max_per_request=%d groups=%d\n", totalEnds, maxEnds, len(tools))
	fmt.Printf("== today: %s\n", today)
	fmt.Printf("== SCORE: %d/%d = %d%% (gate >=%d%%)\n", checks.pass, total, score, minScore)
	if checks.fail == 0 && score >= minScore {
		fmt.Println("== VERDICT: PASS")
		return
	}
	fmt.Println("== VERDICT: FAIL")
	os.Exit(1)
}

func readPrompts(path string) ([]promptRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	var out []promptRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("bad prompt line %q", line)
		}
		flags := map[string]bool{}
		for _, f := range strings.Split(parts[1], ",") {
			if f = strings.TrimSpace(f); f != "" {
				flags[f] = true
			}
		}
		out = append(out, promptRow{ID: parts[0], Flags: flags, Prompt: parts[2]})
	}
	return out, sc.Err()
}

func findConversation(ctx context.Context, pool *pgxpool.Pool, since time.Time, firstPrompt string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		SELECT c.id::text
		FROM aura.conversations c
		JOIN aura.conversation_turns t ON t.conversation_id = c.id
		WHERE c.created_at >= $1 AND t.role = 'user' AND t.content = $2
		ORDER BY c.created_at DESC
		LIMIT 1`, since, firstPrompt).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func loadTurns(ctx context.Context, pool *pgxpool.Pool, convID string) ([]turnRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT seq, role, COALESCE(content, ''), COALESCE(tool_calls::text, '')
		FROM aura.conversation_turns
		WHERE conversation_id = $1::uuid
		ORDER BY seq`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []turnRow
	for rows.Next() {
		var r turnRow
		if err := rows.Scan(&r.Seq, &r.Role, &r.Content, &r.ToolCalls); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadToolGroups(ctx context.Context, pool *pgxpool.Pool, convID string) ([]toolGroup, int64, int64, error) {
	rows, err := pool.Query(ctx, `
		SELECT request_id::text, tool_call_id, max(tool_name),
		       count(*) FILTER (WHERE event_kind = 'start'),
		       count(*) FILTER (WHERE event_kind = 'end'),
		       count(*) FILTER (WHERE event_kind = 'end' AND status = 'error')
		FROM aura.tool_invocations
		WHERE conversation_id = $1::uuid
		GROUP BY request_id, tool_call_id
		ORDER BY request_id, tool_call_id`, convID)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	var out []toolGroup
	var totalEnds int64
	perReq := map[string]int64{}
	for rows.Next() {
		var g toolGroup
		if err := rows.Scan(&g.RequestID, &g.ToolCallID, &g.ToolName, &g.Starts, &g.Ends, &g.Errors); err != nil {
			return nil, 0, 0, err
		}
		out = append(out, g)
		totalEnds += g.Ends
		perReq[g.RequestID] += g.Ends
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	var maxEnds int64
	for _, n := range perReq {
		if n > maxEnds {
			maxEnds = n
		}
	}
	return out, maxEnds, totalEnds, nil
}

func loadToolPayload(ctx context.Context, pool *pgxpool.Pool, convID string) (string, error) {
	var payload string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(string_agg(
			COALESCE(args_raw, '') || E'\n' ||
			COALESCE(result_preview, '') || E'\n' ||
			COALESCE(meta::text, ''), E'\n'), '')
		FROM aura.tool_invocations
		WHERE conversation_id = $1::uuid`, convID).Scan(&payload)
	return payload, err
}

func validateTurns(checks *checkSet, prompts []promptRow, turns []turnRow, today string) {
	userTurns := filterTurns(turns, "user")
	checks.check("P00", fmt.Sprintf("50 user prompts persisted (got %d)", len(userTurns)), len(userTurns) == len(prompts))
	for i, p := range prompts {
		if i >= len(userTurns) {
			checks.check(p.ID, "assistant response exists after prompt", false)
			continue
		}
		checks.check(p.ID+"u", "user prompt text persisted in order", userTurns[i].Content == p.Prompt)
		nextSeq := int(^uint(0) >> 1)
		if i+1 < len(userTurns) {
			nextSeq = userTurns[i+1].Seq
		}
		answer := assistantBetween(turns, userTurns[i].Seq, nextSeq)
		checks.check(p.ID+"a", "non-empty assistant response after prompt", strings.TrimSpace(answer) != "")
		if p.Flags["today"] {
			checks.check(p.ID+"t", "today answer contains ISO date "+today, strings.Contains(answer, today))
		}
	}
}

func validateLeaks(checks *checkSet, turns []turnRow, toolPayload, logText string) {
	var db strings.Builder
	for _, t := range turns {
		db.WriteString(t.Content)
		db.WriteString("\n")
		db.WriteString(t.ToolCalls)
		db.WriteString("\n")
	}
	db.WriteString(toolPayload)
	checks.check("COT-db", "no raw reasoning protocol leaked to DB", firstLeak(db.String()) == "")
	if logText != "" {
		checks.check("COT-log", "no raw reasoning protocol leaked to chat log", firstLeak(logText) == "")
	}
}

func validateTools(checks *checkSet, tools []toolGroup, totalEnds, maxEnds int64, minToolEnds, maxToolsPerRequest int) {
	checks.check("T00", fmt.Sprintf("completed tool invocations >= %d (got %d)", minToolEnds, totalEnds), totalEnds >= int64(minToolEnds))
	checks.check("T01", fmt.Sprintf("max completed tools per request <= %d (got %d)", maxToolsPerRequest, maxEnds), maxEnds <= int64(maxToolsPerRequest))
	for i, g := range tools {
		ok := g.Starts == 1 && g.Ends == 1
		checks.check(fmt.Sprintf("T%02d", i+2), fmt.Sprintf("%s/%s has one start and one end", g.ToolName, g.ToolCallID), ok)
	}
}

func filterTurns(turns []turnRow, role string) []turnRow {
	var out []turnRow
	for _, t := range turns {
		if t.Role == role {
			out = append(out, t)
		}
	}
	return out
}

func assistantBetween(turns []turnRow, afterSeq, beforeSeq int) string {
	var parts []string
	for _, t := range turns {
		if t.Seq > afterSeq && t.Seq < beforeSeq && t.Role == "assistant" && strings.TrimSpace(t.Content) != "" {
			parts = append(parts, t.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func firstLeak(s string) string {
	lower := strings.ToLower(s)
	for _, bad := range []string{
		"reasoning_content",
		`"reasoning":`,
		"<think",
		"</think",
		"<analysis",
		"</analysis",
		"chain of thought",
		"catena di pensiero",
		"ragionamento interno",
	} {
		if strings.Contains(lower, bad) {
			return bad
		}
	}
	return ""
}

func todayISO() string {
	if v := strings.TrimSpace(os.Getenv("AURA_CHAT_50_TODAY")); v != "" {
		return v
	}
	return time.Now().Format("2006-01-02")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "chat50 db probe: "+format+"\n", args...)
	os.Exit(1)
}
