// debug_convdump prints recent conversation turns from the live SQLite DB so
// we can see exactly what the user sees on Telegram vs what tools Aura
// actually called. Read-only — never writes to the DB.
//
//	go run ./cmd/debug_convdump -db data/aura.db -n 60
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	auradb "github.com/aura/aura/internal/db"
)

type turn struct {
	ID         int64
	ChatID     int64
	UserID     int64
	TurnIndex  int64
	Role       string
	Content    string
	ToolCalls  sql.NullString
	ToolCallID sql.NullString
	LLMCalls   int64
	ToolsCount int64
	ElapsedMS  int64
	TokensIn   int64
	TokensOut  int64
	CreatedAt  string
}

func main() {
	dbPath := flag.String("db", "data/aura.db", "path to aura.db")
	limit := flag.Int("n", 60, "number of most-recent turns to dump")
	chat := flag.Int64("chat", 0, "filter by chat_id (0 = all)")
	flag.Parse()

	db, err := auradb.OpenReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	q := `SELECT id, chat_id, user_id, turn_index, role, content,
	             tool_calls, tool_call_id,
	             llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at
	      FROM conversations`
	args := []any{}
	if *chat != 0 {
		q += ` WHERE chat_id = ?`
		args = append(args, *chat)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, *limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	var turns []turn
	for rows.Next() {
		var t turn
		if err := rows.Scan(&t.ID, &t.ChatID, &t.UserID, &t.TurnIndex, &t.Role, &t.Content,
			&t.ToolCalls, &t.ToolCallID, &t.LLMCalls, &t.ToolsCount, &t.ElapsedMS,
			&t.TokensIn, &t.TokensOut, &t.CreatedAt); err != nil {
			fmt.Fprintf(os.Stderr, "scan: %v\n", err)
			os.Exit(1)
		}
		turns = append(turns, t)
	}
	for i := len(turns) - 1; i >= 0; i-- {
		printTurn(turns[i])
	}
	fmt.Printf("\n--- %d turns ---\n", len(turns))
}

func printTurn(t turn) {
	hdr := fmt.Sprintf("[#%d chat=%d turn=%d %s %s]",
		t.ID, t.ChatID, t.TurnIndex, t.Role, t.CreatedAt)
	stats := ""
	if t.LLMCalls > 0 || t.ToolsCount > 0 {
		stats = fmt.Sprintf(" llm=%d tools=%d elapsed=%dms in=%d out=%d",
			t.LLMCalls, t.ToolsCount, t.ElapsedMS, t.TokensIn, t.TokensOut)
	}
	fmt.Printf("\n%s%s\n", hdr, stats)

	body := strings.TrimSpace(t.Content)
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			fmt.Printf("  | %s\n", line)
		}
	}

	if t.ToolCallID.Valid && t.ToolCallID.String != "" {
		fmt.Printf("  tool_call_id=%s\n", t.ToolCallID.String)
	}
	if t.ToolCalls.Valid && t.ToolCalls.String != "" {
		var calls []map[string]any
		if err := json.Unmarshal([]byte(t.ToolCalls.String), &calls); err == nil {
			_ = err
			if raw := strings.TrimSpace(t.ToolCalls.String); len(raw) > 0 && len(calls) == 0 {
				fmt.Printf("  tool_calls(raw-unmapped)=%s\n", truncate(raw, 400))
			}
			for _, c := range calls {
				name, _ := c["name"].(string)
				if name == "" {
					if fn, ok := c["function"].(map[string]any); ok {
						name, _ = fn["name"].(string)
					}
				}
				args := ""
				if a, ok := c["arguments"].(string); ok {
					args = a
				} else if fn, ok := c["function"].(map[string]any); ok {
					if a, ok := fn["arguments"].(string); ok {
						args = a
					}
				}
				fmt.Printf("  TOOL_CALL %s args=%s\n", name, truncate(args, 240))
			}
		} else {
			fmt.Printf("  tool_calls(raw)=%s\n", truncate(t.ToolCalls.String, 200))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
