package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type qa31SeedState struct {
	TaskActive       int
	ProposalPending  int
	IssueOpen        int
	ConversationRows int
}

func (e *Env) qa31SeedState(taskName, proposalSlug, issueSlug string, chatID int64) (qa31SeedState, string, error) {
	var state qa31SeedState
	query := `SELECT
  (SELECT COUNT(*) FROM scheduled_tasks WHERE name = ` + sqliteQuote(taskName) + ` AND status = 'active'),
  (SELECT COUNT(*) FROM proposed_updates WHERE target_slug = ` + sqliteQuote(proposalSlug) + ` AND status = 'pending'),
  (SELECT COUNT(*) FROM wiki_issues WHERE slug = ` + sqliteQuote(issueSlug) + ` AND status = 'open'),
  (SELECT COUNT(*) FROM conversations WHERE chat_id = ` + fmt.Sprintf("%d", chatID) + `);`
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, false, query)
		if err != nil {
			return state, "", err
		}
		parts := strings.Split(strings.TrimSpace(string(raw)), "|")
		if len(parts) == 4 {
			fmt.Sscanf(parts[0], "%d", &state.TaskActive)
			fmt.Sscanf(parts[1], "%d", &state.ProposalPending)
			fmt.Sscanf(parts[2], "%d", &state.IssueOpen)
			fmt.Sscanf(parts[3], "%d", &state.ConversationRows)
		}
		return state, fmt.Sprintf("task=%d proposal=%d issue=%d conversation=%d", state.TaskActive, state.ProposalPending, state.IssueOpen, state.ConversationRows), nil
	}
	err := e.DB.QueryRow(query).Scan(&state.TaskActive, &state.ProposalPending, &state.IssueOpen, &state.ConversationRows)
	return state, fmt.Sprintf("task=%d proposal=%d issue=%d conversation=%d", state.TaskActive, state.ProposalPending, state.IssueOpen, state.ConversationRows), err
}

func (e *Env) cleanupQA31(taskName, proposalSlug, issueSlug string, chatID int64) error {
	if !e.dockerDBReady() {
		return nil
	}
	sqlText := strings.Join([]string{
		"BEGIN IMMEDIATE;",
		"DELETE FROM scheduled_tasks WHERE name = " + sqliteQuote(taskName) + ";",
		"DELETE FROM proposed_updates WHERE target_slug = " + sqliteQuote(proposalSlug) + ";",
		"DELETE FROM wiki_issues WHERE slug = " + sqliteQuote(issueSlug) + ";",
		"DELETE FROM conversations WHERE chat_id = " + fmt.Sprintf("%d", chatID) + ";",
		"COMMIT;",
	}, "\n")
	_, err := e.dockerSQLite(false, false, sqlText)
	return err
}

type qaToolAttemptRow struct {
	ToolName      string `json:"tool_name"`
	Outcome       string `json:"outcome"`
	Class         string `json:"class"`
	Reason        string `json:"reason"`
	ArgKeysJSON   string `json:"arg_keys_json"`
	ErrorRedacted string `json:"error_redacted"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at"`
}

func (e *Env) toolAttemptRowsSince(since time.Time, toolNames, outcomes []string) ([]qaToolAttemptRow, error) {
	where := []string{"started_at >= " + sqliteQuote(since.UTC().Format(time.RFC3339Nano))}
	if len(toolNames) > 0 {
		where = append(where, "tool_name IN ("+sqliteStringList(toolNames)+")")
	}
	if len(outcomes) > 0 {
		where = append(where, "outcome IN ("+sqliteStringList(outcomes)+")")
	}
	query := `SELECT tool_name, outcome, class, reason, arg_keys_json, error_redacted, started_at, ended_at
FROM tool_attempts
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY ended_at DESC
LIMIT 10;`
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		var rows []qaToolAttemptRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("decode docker tool attempts: %w", err)
		}
		return rows, nil
	}
	sqlRows, err := e.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	var out []qaToolAttemptRow
	for sqlRows.Next() {
		var row qaToolAttemptRow
		if err := sqlRows.Scan(&row.ToolName, &row.Outcome, &row.Class, &row.Reason, &row.ArgKeysJSON, &row.ErrorRedacted, &row.StartedAt, &row.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, sqlRows.Err()
}

func (e *Env) recentConversationContent(needles ...string) (string, bool, error) {
	rows, err := e.recentConversationRows(50)
	if err != nil {
		return "", false, err
	}
	for _, row := range rows {
		ok := true
		lower := strings.ToLower(row)
		for _, needle := range needles {
			if !strings.Contains(lower, strings.ToLower(needle)) {
				ok = false
				break
			}
		}
		if ok {
			return row, true, nil
		}
	}
	if len(rows) > 0 {
		return rows[0], false, nil
	}
	return "", false, nil
}

func (e *Env) latestAssistantPreview() (string, bool, error) {
	query := `SELECT content FROM conversations WHERE role = 'assistant' ORDER BY id DESC LIMIT 1;`
	if e.dockerDBReady() {
		var rows []struct {
			Content string `json:"content"`
		}
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return "", false, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return "", false, err
		}
		if len(rows) == 0 {
			return "", false, nil
		}
		return rows[0].Content, true, nil
	}
	var content string
	err := e.DB.QueryRow(query).Scan(&content)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return content, err == nil, err
}

func (e *Env) recentConversationRows(limit int) ([]string, error) {
	query := fmt.Sprintf(`SELECT role || ': ' || substr(content, 1, 1000) AS preview FROM conversations ORDER BY id DESC LIMIT %d;`, limit)
	if e.dockerDBReady() {
		var rows []struct {
			Preview string `json:"preview"`
		}
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.Preview)
		}
		return out, nil
	}
	rows, err := e.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var preview string
		if err := rows.Scan(&preview); err != nil {
			return nil, err
		}
		out = append(out, preview)
	}
	return out, rows.Err()
}

func qaSanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func qaToolPageSlug(name string) string {
	title := strings.ToLower("Tool: " + name)
	title = strings.ReplaceAll(title, " ", "-")
	var b strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == '_' || r == '/' || r == '.' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func (e *Env) readWikiFile(path string) (string, bool, error) {
	u := strings.TrimRight(e.APIBase, "/") + "/files/wiki/file?path=" + url.QueryEscape(path)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var fr struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &fr); err != nil {
		return "", false, err
	}
	return fr.Content, false, nil
}

func (e *Env) cleanupDevTool(name string) {
	_ = e.deleteWikiFile("tools/" + name + ".py")
	_ = e.deleteWikiFile(qaToolPageSlug(name) + ".md")
	index, missing, err := e.readWikiFile("tools/index.md")
	if err != nil || missing || !strings.Contains(index, name) {
		return
	}
	lines := strings.Split(index, "\n")
	out := make([]string, 0, len(lines))
	skip := 0
	for _, line := range lines {
		if skip > 0 {
			skip--
			continue
		}
		if strings.Contains(line, "**"+name+"**") {
			skip = 2
			continue
		}
		out = append(out, line)
	}
	_ = e.writeWikiFile("tools/index.md", strings.Join(out, "\n"))
}

func (e *Env) proposedUpdateCounts(slug, marker string) (wikiCount, userCount int, preview string, err error) {
	query := `SELECT kind, target_slug, category, status, substr(fact, 1, 200) AS fact
FROM proposed_updates
WHERE target_slug = ` + sqliteQuote(slug) + ` OR fact LIKE ` + sqliteQuote("%"+marker+"%") + `
ORDER BY id DESC
LIMIT 10;`
	type row struct {
		Kind       string `json:"kind"`
		TargetSlug string `json:"target_slug"`
		Category   string `json:"category"`
		Status     string `json:"status"`
		Fact       string `json:"fact"`
	}
	var rows []row
	if e.dockerDBReady() {
		raw, qErr := e.dockerSQLite(true, true, query)
		if qErr != nil {
			return 0, 0, "", qErr
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if qErr := json.Unmarshal(raw, &rows); qErr != nil {
			return 0, 0, "", qErr
		}
	} else {
		sqlRows, qErr := e.DB.Query(query)
		if qErr != nil {
			return 0, 0, "", qErr
		}
		defer sqlRows.Close()
		for sqlRows.Next() {
			var r row
			if qErr := sqlRows.Scan(&r.Kind, &r.TargetSlug, &r.Category, &r.Status, &r.Fact); qErr != nil {
				return 0, 0, "", qErr
			}
			rows = append(rows, r)
		}
		if qErr := sqlRows.Err(); qErr != nil {
			return 0, 0, "", qErr
		}
	}
	var parts []string
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("kind=%s status=%s target=%s category=%s fact=%s", r.Kind, r.Status, r.TargetSlug, r.Category, truncate(r.Fact, 80)))
		if r.Kind == "wiki" && r.TargetSlug == slug && r.Status == "pending" {
			wikiCount++
		}
		if r.Kind == "user_memory" && strings.Contains(r.Fact, marker) && r.Status == "pending" {
			userCount++
		}
	}
	return wikiCount, userCount, strings.Join(parts, " | "), nil
}

func (e *Env) compactMemoryCount(marker string) (int, error) {
	query := `SELECT COUNT(*) FROM compact_memory_documents WHERE body LIKE ` + sqliteQuote("%"+marker+"%") + ` OR title LIKE ` + sqliteQuote("%"+marker+"%") + `;`
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, false, query)
		if err != nil {
			return 0, err
		}
		var n int
		fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &n)
		return n, nil
	}
	var n int
	err := e.DB.QueryRow(query).Scan(&n)
	return n, err
}

func (e *Env) cleanupProposedUpdates(slug, marker string) error {
	if !e.dockerDBReady() {
		return nil
	}
	sqlText := "DELETE FROM proposed_updates WHERE target_slug = " + sqliteQuote(slug) + " OR fact LIKE " + sqliteQuote("%"+marker+"%") + ";"
	_, err := e.dockerSQLite(false, false, sqlText)
	return err
}

type qaSwarmTask struct {
	ID     string `json:"id"`
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Result string `json:"result"`
}

func (e *Env) latestCompletedSwarmTask(since time.Time) (qaSwarmTask, bool, error) {
	query := `SELECT id, run_id, status, result FROM swarm_tasks
WHERE created_at >= ` + sqliteQuote(since.UTC().Format(time.RFC3339Nano)) + ` AND status = 'completed'
ORDER BY created_at DESC LIMIT 1;`
	var rows []qaSwarmTask
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return qaSwarmTask{}, false, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return qaSwarmTask{}, false, err
		}
	} else {
		row := e.DB.QueryRow(query)
		var task qaSwarmTask
		err := row.Scan(&task.ID, &task.RunID, &task.Status, &task.Result)
		if err == sql.ErrNoRows {
			return qaSwarmTask{}, false, nil
		}
		return task, err == nil, err
	}
	if len(rows) == 0 {
		return qaSwarmTask{}, false, nil
	}
	return rows[0], true, nil
}

type qaSwarmRun struct {
	ID        string `json:"id"`
	Goal      string `json:"goal"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
}

func (e *Env) swarmRunForGoal(marker string) (qaSwarmRun, bool, error) {
	query := `SELECT id, goal, status, last_error FROM swarm_runs
WHERE goal LIKE ` + sqliteQuote("%"+marker+"%") + `
ORDER BY created_at DESC LIMIT 1;`
	var rows []qaSwarmRun
	if e.dockerDBReady() {
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return qaSwarmRun{}, false, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return qaSwarmRun{}, false, err
		}
	} else {
		row := e.DB.QueryRow(query)
		var run qaSwarmRun
		err := row.Scan(&run.ID, &run.Goal, &run.Status, &run.LastError)
		if err == sql.ErrNoRows {
			return qaSwarmRun{}, false, nil
		}
		return run, err == nil, err
	}
	if len(rows) == 0 {
		return qaSwarmRun{}, false, nil
	}
	return rows[0], true, nil
}

func (e *Env) swarmTaskStatsForRun(runID, marker, control string) (count, incomplete int, matched bool, preview string, err error) {
	query := `SELECT id, status, result FROM swarm_tasks WHERE run_id = ` + sqliteQuote(runID) + ` ORDER BY created_at;`
	type row struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Result string `json:"result"`
	}
	var rows []row
	if e.dockerDBReady() {
		raw, qErr := e.dockerSQLite(true, true, query)
		if qErr != nil {
			return 0, 0, false, "", qErr
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if qErr := json.Unmarshal(raw, &rows); qErr != nil {
			return 0, 0, false, "", qErr
		}
	} else {
		sqlRows, qErr := e.DB.Query(query)
		if qErr != nil {
			return 0, 0, false, "", qErr
		}
		defer sqlRows.Close()
		for sqlRows.Next() {
			var r row
			if qErr := sqlRows.Scan(&r.ID, &r.Status, &r.Result); qErr != nil {
				return 0, 0, false, "", qErr
			}
			rows = append(rows, r)
		}
		if qErr := sqlRows.Err(); qErr != nil {
			return 0, 0, false, "", qErr
		}
	}
	var parts []string
	for _, r := range rows {
		count++
		if r.Status != "completed" {
			incomplete++
		}
		if strings.Contains(r.Result, marker) && strings.Contains(r.Result, control) {
			matched = true
		}
		parts = append(parts, fmt.Sprintf("id=%s status=%s result=%s", r.ID, r.Status, truncate(r.Result, 120)))
	}
	return count, incomplete, matched, strings.Join(parts, " | "), nil
}

func qaFetchURL(target string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return string(body), nil
}
