// Command probe_chat is the canonical end-to-end chat pipe for Aura.
//
// It exercises the running /api/chat endpoint with a battery of cases
// and verifies BOTH the assistant's textual reply AND ground truth
// (SQLite tables, filesystem). This is the rule documented in
// CLAUDE.md: "tool_calls: N in the response is necessary but never
// sufficient; the model can call a tool correctly and still
// hallucinate around the result."
//
// Roles per CLAUDE.md:
//
//	I (Claude Code) am the programmer — I write and run this pipe.
//	Aura is the runtime under test — every textual claim she makes
//	must be cross-checked against ground truth before being trusted.
//
// Usage:
//
//	go run ./cmd/probe_chat                       # run all cases
//	go run ./cmd/probe_chat -case schedule        # run one case
//	go run ./cmd/probe_chat -prompt "..." -raw    # ad-hoc one-shot
//
// Env config (with defaults):
//
//	AURA_CHAT_URL    = http://localhost:18080/api/chat
//	AURA_CHAT_TOKEN  = (required for non-loopback; bearer token)
//	AURA_DB_PATH     = ./data/aura.db   (read-only)
//	AURA_API_BASE    = http://localhost:18080/api   (for wiki ground truth)
//
// Wiki ground truth is fetched via /api/wiki/page?slug=… because the
// in-container wiki lives in a named Docker volume, not a bind mount.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/db"
)

// ChatReply mirrors api.ChatReply just enough to deserialize the JSON.
type ChatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
}

// Case is one E2E assertion: a prompt to send and a Verify closure
// that returns one entry per assertion violation. Empty slice = PASS.
type Case struct {
	Name    string
	Prompt  string
	Setup   func(env *Env) error                // optional: prep state before sending
	Verify  func(reply ChatReply, env *Env) []string // required
	Cleanup func(env *Env)                      // optional: tear down leftover state
}

// Env bundles everything a Verify function needs to consult ground truth.
type Env struct {
	DB        *sql.DB
	APIBase   string // e.g. http://localhost:18080/api
	APIToken  string
	APIClient *http.Client
}

// fetchWikiPage reads a single wiki page through the dashboard API.
// Returns (nil, true) when the API responds 404 (page genuinely missing)
// so Verify functions can distinguish "missing" from transport errors.
func (e *Env) fetchWikiPage(slug string) (body string, missing bool, err error) {
	url := strings.TrimRight(e.APIBase, "/") + "/wiki/page?slug=" + slug
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var page struct {
		BodyMD string `json:"body_md"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", false, fmt.Errorf("decode: %w", err)
	}
	// Reconstruct a frontmatter-ish view so substring asserts work against title.
	return "title: " + page.Title + "\n" + page.BodyMD, false, nil
}

// =========================================================================
// CASES — add new cases here. Keep them deterministic; pick unique names so
// re-runs don't collide with prior state.
// =========================================================================

func allCases(now time.Time) []Case {
	stamp := now.Format("20060102-150405")
	taskName := "probe-chat-task-" + stamp
	wikiSlug := "probe-chat-page-" + stamp
	wikiTitle := "Probe Chat Page " + stamp

	return []Case{
		// 1. Pure conversational — no tools needed, no phantom risk.
		{
			Name:   "greeting-no-tools",
			Prompt: "Ciao Aura, dimmi solo in una riga come stai.",
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls != 0 {
					miss = append(miss, fmt.Sprintf("expected 0 tool calls for greeting, got %d", r.ToolCalls))
				}
				if strings.TrimSpace(r.Reply) == "" {
					miss = append(miss, "reply is empty")
				}
				return miss
			},
		},

		// 2. schedule-reminder — verify DB row matches what the reply claims.
		{
			Name:   "schedule-reminder",
			Prompt: fmt.Sprintf("Schedulami un reminder chiamato %s fra 30 minuti con payload 'probe chat smoke'. Poi conferma.", taskName),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				// Reply must reference the task name we asked for.
				if !strings.Contains(strings.ToLower(r.Reply), strings.ToLower(taskName)) {
					miss = append(miss, fmt.Sprintf("reply does not reference task name %q", taskName))
				}
				// Ground truth: row must exist in scheduled_tasks with kind=reminder.
				var kind, status string
				err := env.DB.QueryRow(
					`SELECT kind, status FROM scheduled_tasks WHERE name = ?`,
					taskName,
				).Scan(&kind, &status)
				if err == sql.ErrNoRows {
					miss = append(miss, fmt.Sprintf("DB ground truth: scheduled_tasks row for %q missing", taskName))
				} else if err != nil {
					miss = append(miss, fmt.Sprintf("DB query error: %v", err))
				} else {
					if kind != "reminder" {
						miss = append(miss, fmt.Sprintf("DB kind = %q, want reminder", kind))
					}
					if status != "active" {
						miss = append(miss, fmt.Sprintf("DB status = %q, want active", status))
					}
				}
				return miss
			},
			Cleanup: func(env *Env) {
				_, _ = env.DB.Exec(`UPDATE scheduled_tasks SET status='cancelled' WHERE name = ?`, taskName)
			},
		},

		// 3. wiki-page-create — verify the page lands in the live wiki via
		//    /api/wiki/page (named-volume mount; not visible on host FS).
		{
			Name:   "wiki-page-create",
			Prompt: fmt.Sprintf("Crea una pagina wiki intitolata %q con questo body: 'E2E probe chat run %s'. Conferma quando hai finito.", wikiTitle, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if !strings.Contains(strings.ToLower(r.Reply), strings.ToLower(wikiSlug)) {
					miss = append(miss, fmt.Sprintf("reply does not reference slug %q", wikiSlug))
				}
				body, missing, err := env.fetchWikiPage(wikiSlug)
				if err != nil {
					miss = append(miss, fmt.Sprintf("wiki API ground truth: %v", err))
					return miss
				}
				if missing {
					miss = append(miss, fmt.Sprintf("wiki API ground truth: slug %q returned 404 — page was not actually created", wikiSlug))
					return miss
				}
				if !strings.Contains(body, wikiTitle) {
					miss = append(miss, fmt.Sprintf("wiki page %q does not contain title %q", wikiSlug, wikiTitle))
				}
				if !strings.Contains(body, "E2E probe chat run "+stamp) {
					miss = append(miss, fmt.Sprintf("wiki page %q does not contain expected body", wikiSlug))
				}
				return miss
			},
			// No cleanup — wiki pages are user-visible artifacts and the
			// timestamped slug avoids cross-run collisions. A pre-existing
			// cleanup endpoint would be nicer; leave it for now.
		},

		// 4. web-search — verify the model uses the unified web tool and
		//    surfaces at least one URL from a known stable search.
		{
			Name:   "web-search-wikipedia",
			Prompt: "Cerca sul web il sito ufficiale di Wikipedia. Riportami solo l'URL principale di wikipedia.org.",
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call for a web search")
				}
				if !strings.Contains(strings.ToLower(r.Reply), "wikipedia.org") {
					miss = append(miss, "reply does not contain wikipedia.org")
				}
				return miss
			},
		},

		// 5. phantom-trap — non-existent task name; model MUST NOT claim it ran.
		//    The reply may explain that the task doesn't exist OR may schedule a
		//    new one of that name. Either is fine; what's forbidden is claiming
		//    a past run that never happened.
		{
			Name:   "phantom-trap-nonexistent-task",
			Prompt: "Eseguito già run_now sul task probe-chat-nonexistent-zzz oggi? Confermami solo se è successo davvero.",
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				reply := strings.ToLower(r.Reply)
				// DB ground truth: this task should not exist.
				var count int
				_ = env.DB.QueryRow(
					`SELECT count(*) FROM scheduled_tasks WHERE name = 'probe-chat-nonexistent-zzz'`,
				).Scan(&count)
				if count > 0 {
					miss = append(miss, "test precondition violated: probe-chat-nonexistent-zzz already exists in DB")
					return miss
				}
				// The reply must NOT contain a phantom claim of past execution.
				// Acceptable: "non esiste", "non lo trovo", "no, non risulta".
				phantomClaims := []string{
					"sì, è stato eseguito",
					"si, è stato eseguito",
					"l'ho eseguito",
					"ho eseguito",
					"è già stato eseguito",
					"il task è stato eseguito",
					"yes, i ran it",
					"i executed it",
					"i ran it",
					"successfully ran",
				}
				for _, phrase := range phantomClaims {
					if strings.Contains(reply, phrase) {
						miss = append(miss, fmt.Sprintf("phantom claim detected: reply contains %q", phrase))
					}
				}
				return miss
			},
		},
	}
}

// =========================================================================

func main() {
	var (
		caseName  = flag.String("case", "", "run only the named case (empty = run all)")
		prompt    = flag.String("prompt", "", "send a single ad-hoc prompt and print the structured reply (skips Verify)")
		jsonOut   = flag.Bool("json", false, "emit results as JSON instead of human-readable table")
		baseURL   = flag.String("url", envDefault("AURA_CHAT_URL", "http://localhost:18080/api/chat"), "chat endpoint")
		apiBase   = flag.String("api", envDefault("AURA_API_BASE", "http://localhost:18080/api"), "dashboard API base (used for wiki ground truth)")
		token     = flag.String("token", os.Getenv("AURA_CHAT_TOKEN"), "bearer token (defaults to $AURA_CHAT_TOKEN)")
		dbPath    = flag.String("db", envDefault("AURA_DB_PATH", "./data/aura.db"), "SQLite DB path (read-only)")
		timeoutS  = flag.Int("timeout", 240, "per-prompt timeout (seconds)")
	)
	flag.Parse()

	if *token == "" {
		fail("AURA_CHAT_TOKEN is required (env or -token)")
	}

	client := &http.Client{Timeout: time.Duration(*timeoutS) * time.Second}

	// Ad-hoc one-shot: print structured reply, exit.
	if strings.TrimSpace(*prompt) != "" {
		reply, err := sendChat(client, *baseURL, *token, *prompt)
		if err != nil {
			fail(err.Error())
		}
		printOneShot(reply, *jsonOut)
		return
	}

	// Ground-truth probes need DB + wiki access.
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fail(fmt.Sprintf("open DB %s: %v", *dbPath, err))
	}
	defer db.Close()
	env := &Env{
		DB:        db,
		APIBase:   *apiBase,
		APIToken:  *token,
		APIClient: client,
	}

	cases := allCases(time.Now())
	if *caseName != "" {
		filtered := cases[:0]
		for _, c := range cases {
			if c.Name == *caseName {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			fail(fmt.Sprintf("no case named %q", *caseName))
		}
		cases = filtered
	}

	results := runAll(client, *baseURL, *token, env, cases)
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(results)
	} else {
		printReport(results)
	}
	if anyFailed(results) {
		os.Exit(1)
	}
}

// =========================================================================
// EXECUTION
// =========================================================================

type Result struct {
	Name        string   `json:"name"`
	Prompt      string   `json:"prompt"`
	Reply       string   `json:"reply"`
	ToolCalls   int      `json:"tool_calls"`
	LLMCalls    int      `json:"llm_calls"`
	Tokens      int      `json:"tokens"`
	ElapsedMs   int64    `json:"elapsed_ms"`
	Mismatches  []string `json:"mismatches"`
	TransportErr string  `json:"transport_err,omitempty"`
	Pass        bool     `json:"pass"`
}

func runAll(client *http.Client, baseURL, token string, env *Env, cases []Case) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		if c.Setup != nil {
			if err := c.Setup(env); err != nil {
				out = append(out, Result{
					Name:         c.Name,
					Prompt:       c.Prompt,
					TransportErr: fmt.Sprintf("setup: %v", err),
				})
				continue
			}
		}
		reply, err := sendChat(client, baseURL, token, c.Prompt)
		if err != nil {
			out = append(out, Result{
				Name:         c.Name,
				Prompt:       c.Prompt,
				TransportErr: err.Error(),
			})
			if c.Cleanup != nil {
				c.Cleanup(env)
			}
			continue
		}
		mismatches := c.Verify(reply, env)
		out = append(out, Result{
			Name:       c.Name,
			Prompt:     c.Prompt,
			Reply:      reply.Reply,
			ToolCalls:  reply.ToolCalls,
			LLMCalls:   reply.LLMCalls,
			Tokens:     reply.Tokens,
			ElapsedMs:  reply.ElapsedMs,
			Mismatches: mismatches,
			Pass:       len(mismatches) == 0,
		})
		if c.Cleanup != nil {
			c.Cleanup(env)
		}
	}
	return out
}

func sendChat(client *http.Client, baseURL, token, prompt string) (ChatReply, error) {
	payload, _ := json.Marshal(map[string]string{"message": prompt})
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	if err != nil {
		return ChatReply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ChatReply{}, fmt.Errorf("POST %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatReply{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ChatReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var reply ChatReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return ChatReply{}, fmt.Errorf("decode reply: %w (raw: %s)", err, truncate(string(body), 400))
	}
	return reply, nil
}

// =========================================================================
// REPORT
// =========================================================================

func printOneShot(r ChatReply, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	fmt.Printf("reply:      %s\n", r.Reply)
	fmt.Printf("tool_calls: %d\n", r.ToolCalls)
	fmt.Printf("llm_calls:  %d\n", r.LLMCalls)
	fmt.Printf("tokens:     %d\n", r.Tokens)
	fmt.Printf("elapsed_ms: %d\n", r.ElapsedMs)
}

func printReport(results []Result) {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Pass {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("=== probe_chat: %d total, %d PASS, %d FAIL ===\n\n", len(results), passed, failed)
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s  (tool_calls=%d, llm=%d, tokens=%d, elapsed=%dms)\n",
			status, r.Name, r.ToolCalls, r.LLMCalls, r.Tokens, r.ElapsedMs)
		fmt.Printf("  prompt: %s\n", truncate(r.Prompt, 160))
		fmt.Printf("  reply : %s\n", truncate(r.Reply, 280))
		if r.TransportErr != "" {
			fmt.Printf("  TRANSPORT ERROR: %s\n", r.TransportErr)
		}
		for _, m := range r.Mismatches {
			fmt.Printf("  MISMATCH: %s\n", m)
		}
		fmt.Println()
	}
}

func anyFailed(results []Result) bool {
	for _, r := range results {
		if !r.Pass {
			return true
		}
	}
	return false
}

// =========================================================================
// HELPERS
// =========================================================================

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	// Route through internal/db.OpenReadOnly to honor the shared driver
	// policy (the TestProductionSQLiteOpensGoThroughSharedDBPackage gate).
	return db.OpenReadOnly(path)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "probe_chat: "+msg)
	os.Exit(2)
}
