package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/storage/memoryindex"
)

func (e *Env) dockerDBReady() bool {
	if strings.TrimSpace(os.Getenv("AURA_PROBE_DB_DOCKER")) == "0" {
		return false
	}
	_, err := e.dockerSQLite(true, false, `SELECT 1;`)
	return err == nil
}

func (e *Env) dockerSQLite(readonly, jsonMode bool, query string) ([]byte, error) {
	args := []string{"compose", "exec", "-T", "aura", "sqlite3"}
	if readonly {
		args = append(args, "-readonly")
	}
	if jsonMode {
		args = append(args, "-json")
	}
	args = append(args, "/data/aura.db", query)

	cmd := exec.Command("docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker sqlite: %w: %s", err, truncate(stderr.String(), 300))
	}
	return bytes.TrimSpace(out), nil
}

func (e *Env) upsertCompactMemoryDocument(ctx context.Context, doc memoryindex.Document) error {
	if e.dockerDBReady() {
		return e.upsertCompactMemoryDocumentDocker(doc)
	}
	wdb, err := auradb.Open(e.DBPath)
	if err != nil {
		return fmt.Errorf("open writable db: %w", err)
	}
	defer wdb.Close()
	store, err := memoryindex.NewStore(wdb)
	if err != nil {
		return fmt.Errorf("memoryindex store: %w", err)
	}
	return store.Upsert(ctx, doc)
}

func (e *Env) upsertCompactMemoryDocumentDocker(doc memoryindex.Document) error {
	doc.ID = strings.TrimSpace(doc.ID)
	doc.Kind = strings.TrimSpace(doc.Kind)
	doc.Body = strings.TrimSpace(doc.Body)
	if doc.ID == "" || doc.Kind == "" || doc.Body == "" {
		return fmt.Errorf("compact memory doc requires id, kind, and body")
	}
	if doc.Handle == "" {
		doc.Handle = doc.ID
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = time.Now().UTC()
	}
	if doc.ContentHash == "" {
		doc.ContentHash = memoryindex.ContentHash(doc.Kind, doc.Body, doc.EmbeddingModelID)
	}
	entitiesJSON, _ := json.Marshal(doc.Entities)
	tagsJSON, _ := json.Marshal(doc.Tags)
	updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339Nano)

	sqlText := strings.Join([]string{
		"BEGIN IMMEDIATE;",
		"DELETE FROM compact_memory_fts WHERE id = " + sqliteQuote(doc.ID) + ";",
		`INSERT OR REPLACE INTO compact_memory_documents
  (id, kind, title, body, handle, source_id, page, chunk_index, byte_start, byte_end, chat_id, conversation_id, proposal_id, status, entities_json, tags_json, updated_at, content_hash, embedding_model_id, index_build_id)
VALUES (` + strings.Join([]string{
			sqliteQuote(doc.ID),
			sqliteQuote(doc.Kind),
			sqliteQuote(doc.Title),
			sqliteQuote(doc.Body),
			sqliteQuote(doc.Handle),
			sqliteQuote(doc.SourceID),
			strconv.Itoa(doc.Page),
			strconv.Itoa(doc.ChunkIndex),
			strconv.Itoa(doc.ByteStart),
			strconv.Itoa(doc.ByteEnd),
			strconv.FormatInt(doc.ChatID, 10),
			strconv.FormatInt(doc.ConversationID, 10),
			strconv.FormatInt(doc.ProposalID, 10),
			sqliteQuote(doc.Status),
			sqliteQuote(string(entitiesJSON)),
			sqliteQuote(string(tagsJSON)),
			sqliteQuote(updatedAt),
			sqliteQuote(doc.ContentHash),
			sqliteQuote(doc.EmbeddingModelID),
			sqliteQuote(doc.IndexBuildID),
		}, ", ") + `);`,
		`INSERT INTO compact_memory_fts
  (id, kind, title, body, handle, source_id, status, entities, tags)
VALUES (` + strings.Join([]string{
			sqliteQuote(doc.ID),
			sqliteQuote(doc.Kind),
			sqliteQuote(doc.Title),
			sqliteQuote(doc.Body),
			sqliteQuote(doc.Handle),
			sqliteQuote(doc.SourceID),
			sqliteQuote(doc.Status),
			sqliteQuote(strings.Join(doc.Entities, " ")),
			sqliteQuote(strings.Join(doc.Tags, " ")),
		}, ", ") + `);`,
		"COMMIT;",
	}, "\n")

	if _, err := e.dockerSQLite(false, false, sqlText); err != nil {
		return fmt.Errorf("docker compact memory upsert %s: %w", doc.ID, err)
	}
	return nil
}

func (e *Env) deleteCompactMemoryDocuments(ctx context.Context, ids []string) error {
	ids = nonEmptyStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	if e.dockerDBReady() {
		inList := sqliteStringList(ids)
		sqlText := "BEGIN IMMEDIATE;\n" +
			"DELETE FROM compact_memory_fts WHERE id IN (" + inList + ");\n" +
			"DELETE FROM compact_memory_documents WHERE id IN (" + inList + ");\n" +
			"COMMIT;"
		_, err := e.dockerSQLite(false, false, sqlText)
		return err
	}
	wdb, err := auradb.Open(e.DBPath)
	if err != nil {
		return err
	}
	defer wdb.Close()
	for _, id := range ids {
		_, _ = wdb.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE id = ?`, id)
		_, _ = wdb.ExecContext(ctx, `DELETE FROM compact_memory_documents WHERE id = ?`, id)
	}
	return nil
}

func (e *Env) toolAttemptsSince(since time.Time, toolNames ...string) (map[string]int, error) {
	toolNames = nonEmptyStrings(toolNames)
	if len(toolNames) == 0 {
		return map[string]int{}, nil
	}
	if e.dockerDBReady() {
		var rows []struct {
			ToolName string `json:"tool_name"`
			Count    int    `json:"count"`
		}
		query := `
SELECT tool_name, COUNT(*) AS count
FROM tool_attempts
WHERE started_at >= ` + sqliteQuote(since.UTC().Format(time.RFC3339Nano)) + `
  AND outcome = 'ok'
  AND tool_name IN (` + sqliteStringList(toolNames) + `)
GROUP BY tool_name;`
		raw, err := e.dockerSQLite(true, true, query)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("decode docker tool_attempts: %w", err)
		}
		out := make(map[string]int, len(rows))
		for _, row := range rows {
			out[row.ToolName] = row.Count
		}
		return out, nil
	}
	return toolAttemptsSinceSQL(e.DB, since, toolNames)
}

func toolAttemptsSinceSQL(db *sql.DB, since time.Time, toolNames []string) (map[string]int, error) {
	args := []any{since.UTC().Format(time.RFC3339Nano)}
	placeholders := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		placeholders = append(placeholders, "?")
		args = append(args, name)
	}
	rows, err := db.Query(`
SELECT tool_name, COUNT(*)
FROM tool_attempts
WHERE started_at >= ?
  AND outcome = 'ok'
  AND tool_name IN (`+strings.Join(placeholders, ",")+`)
GROUP BY tool_name
`, args...)
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

func (e *Env) agentNoteContent(conversationID string) (content string, exists bool, err error) {
	if e.dockerDBReady() {
		var rows []struct {
			Content string `json:"content"`
		}
		raw, err := e.dockerSQLite(true, true,
			`SELECT content FROM agent_notes WHERE conversation_id = `+sqliteQuote(conversationID)+` LIMIT 1;`)
		if err != nil {
			return "", false, err
		}
		if len(raw) == 0 {
			raw = []byte("[]")
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return "", false, fmt.Errorf("decode docker agent_notes: %w", err)
		}
		if len(rows) == 0 {
			return "", false, nil
		}
		return rows[0].Content, true, nil
	}
	var noteContent string
	err = e.DB.QueryRow(`SELECT content FROM agent_notes WHERE conversation_id = ?`, conversationID).Scan(&noteContent)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return noteContent, err == nil, err
}

func (e *Env) deleteAgentNote(conversationID string) error {
	if e.dockerDBReady() {
		_, err := e.dockerSQLite(false, false,
			`DELETE FROM agent_notes WHERE conversation_id = `+sqliteQuote(conversationID)+`;`)
		return err
	}
	_, err := e.DB.Exec(`DELETE FROM agent_notes WHERE conversation_id = ?`, conversationID)
	return err
}

func sqliteQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func sqliteStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, sqliteQuote(value))
	}
	return strings.Join(quoted, ", ")
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
