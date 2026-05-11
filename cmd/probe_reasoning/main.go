// Command probe_reasoning fires two identical chat completions against the
// LLM provider Aura uses in production — once with reasoning disabled, once
// with it enabled — and prints the full token-usage breakdown for both so we
// can see (a) whether the provider honors the reasoning shape Aura emits and
// (b) how much depth costs vs the no-reasoning baseline.
//
// Reads the same settings the running container uses (LLM_API_KEY,
// LLM_BASE_URL, LLM_MODEL from the SQLite settings table; falls back to
// the environment). Renders Aura's real system prompt so the experiment
// mirrors a turn the bot actually runs.
//
// Usage:
//
//	# from inside the aura container so /data/aura.db is mounted:
//	aura-probe-reasoning -prompt "Quanti possibili gusti di gelato esistono?"
//
// Build:
//
//	go build -o /tmp/aura-probe-reasoning ./cmd/probe_reasoning
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/llm"
)

func main() {
	var (
		dbPath = flag.String("db", "/data/aura.db", "path to the aura sqlite database")
		prompt = flag.String("prompt", "Spiegami in 4 frasi cosa è il teorema CAP e dimmi un esempio reale di sistema CP.", "user message")
		effort = flag.String("effort", "high", "reasoning effort to test ('enabled'|'low'|'medium'|'high'|'xhigh')")
	)
	flag.Parse()

	baseURL, model, apiKey, err := readSettings(*dbPath)
	if err != nil {
		die("read settings: %v", err)
	}
	if baseURL == "" || model == "" || apiKey == "" {
		die("missing LLM_BASE_URL / LLM_MODEL / LLM_API_KEY")
	}

	system := conversation.RenderSystemPrompt(time.Now(), time.Local)
	messages := []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: *prompt},
	}

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})
	_ = http.Client{} // keep import for future tweaks (Timeout override etc.)

	fmt.Printf("== probe ==\nbase_url:   %s\nmodel:      %s\nprompt:     %s\nsystem_len: %d chars\n\n", baseURL, model, *prompt, len(system))

	for _, cfg := range []struct {
		label  string
		effort string
	}{
		{"reasoning OFF", ""},
		{"reasoning " + *effort, *effort},
	} {
		fmt.Printf("--- %s ---\n", cfg.label)
		start := time.Now()
		resp, err := client.Send(context.Background(), llm.Request{
			Messages:        messages,
			ReasoningEffort: cfg.effort,
		})
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("error: %v\n\n", err)
			continue
		}
		preview := strings.TrimSpace(resp.Content)
		if len(preview) > 600 {
			preview = preview[:600] + "…"
		}
		fmt.Printf("elapsed:    %s\n", elapsed.Round(time.Millisecond))
		fmt.Printf("tokens:     prompt=%d completion=%d (reasoning=%d) total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens,
			resp.Usage.ReasoningTokens, resp.Usage.TotalTokens)
		fmt.Printf("has_tools:  %t\n", resp.HasToolCalls)
		if resp.Reasoning != "" {
			r := resp.Reasoning
			if len(r) > 2000 {
				r = r[:2000] + "…"
			}
			fmt.Printf("reasoning (%d chars):\n%s\n\n", len(resp.Reasoning), r)
		} else {
			fmt.Println("reasoning:  (none surfaced by provider)")
		}
		if len(resp.ReasoningDetails) > 0 {
			d := string(resp.ReasoningDetails)
			if len(d) > 1500 {
				d = d[:1500] + "…"
			}
			fmt.Printf("reasoning_details (%d bytes):\n%s\n\n", len(resp.ReasoningDetails), d)
		}
		fmt.Printf("content:\n%s\n\n", preview)
	}
}

// readSettings pulls the live LLM config from Aura's settings table. Falls
// back to env vars when the DB row is absent, so the probe works on a
// freshly-bootstrapped instance too.
func readSettings(dbPath string) (baseURL, model, apiKey string, err error) {
	if _, statErr := os.Stat(dbPath); statErr == nil {
		db, openErr := auradb.OpenReadOnly(dbPath)
		if openErr != nil {
			return "", "", "", openErr
		}
		defer db.Close()
		baseURL = settingValue(db, "LLM_BASE_URL")
		model = settingValue(db, "LLM_MODEL")
		apiKey = settingValue(db, "LLM_API_KEY")
	}
	if baseURL == "" {
		baseURL = os.Getenv("LLM_BASE_URL")
	}
	if model == "" {
		model = os.Getenv("LLM_MODEL")
	}
	if apiKey == "" {
		apiKey = os.Getenv("LLM_API_KEY")
	}
	return baseURL, model, apiKey, nil
}

func settingValue(db *sql.DB, key string) string {
	var v string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "probe: "+format+"\n", args...)
	os.Exit(1)
}

// Force JSON import for future expansion (planned: parse upstream reasoning_tokens
// when providers surface them in usage_details). Silences the linter for now.
var _ = json.Marshal
