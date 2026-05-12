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
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aura/aura/internal/db"
	"github.com/ledongthuc/pdf"
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

// sourceIDRe extracts a source_id token (`src_<16 hex>`) from free-form
// assistant text. The doc tool's JSON reply contains source_id and the
// model usually echoes it; we grep the natural-language reply for it
// rather than parse a structured field that may or may not exist.
var sourceIDRe = regexp.MustCompile(`src_[a-f0-9]{16}`)

// findSourceID returns the first src_xxx token in s, or "" if absent.
func findSourceID(s string) string {
	return sourceIDRe.FindString(s)
}

// fetchSourceRaw returns the raw bytes of a source via /api/sources/{id}/raw.
func (e *Env) fetchSourceRaw(id string) ([]byte, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id + "/raw"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return io.ReadAll(resp.Body)
}

// extractTextFromZipEntry returns the concatenated text content of every
// `<TAG>...</TAG>` (e.g. <w:t> in docx, <t> in xlsx sharedStrings) inside
// the named entry of the ZIP body. Returns "" if entry missing.
func extractTextFromZipEntry(body []byte, entryName, tag string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("not a valid zip: %w", err)
	}
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == entryName {
			entry = f
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("entry %q not found in zip", entryName)
	}
	rc, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("open %s: %w", entryName, err)
	}
	defer rc.Close()
	xmlBytes, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", entryName, err)
	}
	xml := string(xmlBytes)
	// Naive but reliable: scan for `<tag` ... `>` ... `</tag>` segments.
	// Works for both `<w:t>foo</w:t>` (docx) and `<t>foo</t>` (xlsx) and
	// tolerates attributes like `<w:t xml:space="preserve">`.
	var out strings.Builder
	openMarker := "<" + tag
	closeMarker := "</" + tag + ">"
	i := 0
	for {
		start := strings.Index(xml[i:], openMarker)
		if start < 0 {
			break
		}
		afterOpen := strings.Index(xml[i+start:], ">")
		if afterOpen < 0 {
			break
		}
		textStart := i + start + afterOpen + 1
		end := strings.Index(xml[textStart:], closeMarker)
		if end < 0 {
			break
		}
		out.WriteString(xml[textStart : textStart+end])
		out.WriteString(" ")
		i = textStart + end + len(closeMarker)
	}
	return out.String(), nil
}

// extractPDFText returns the textual content of a PDF byte buffer via
// ledongthuc/pdf. Returns "" with a wrapped error on parse failure.
func extractPDFText(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("pdf NewReader: %w", err)
	}
	var out strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, perr := page.GetPlainText(nil)
		if perr != nil {
			continue
		}
		out.WriteString(text)
		out.WriteString("\n")
	}
	return out.String(), nil
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

		// 4. web-fetch-summarize — fetch a specific page and produce a
		//    real summary. Hardest case in the suite: must call web with
		//    action=fetch, parse the HTML, and synthesize a faithful
		//    summary that contains the article's load-bearing concepts.
		//    Anthropic's "Effective context engineering for AI agents"
		//    is a stable target with predictable vocabulary.
		{
			Name:   "web-fetch-summarize-context-engineering",
			Prompt: "Vai a https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents e fai un riassunto in 5 bullet point dei concetti principali. Cita almeno: context window, tool use, agent loop.",
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (web fetch) — got 0, likely phantom")
				}
				reply := strings.ToLower(r.Reply)
				// Load-bearing concepts from the actual page. If the
				// summary doesn't mention these, the model either didn't
				// fetch or hallucinated content.
				required := []string{"context", "agent", "tool"}
				for _, kw := range required {
					if !strings.Contains(reply, kw) {
						miss = append(miss, fmt.Sprintf("summary missing required keyword %q (page is about %s)", kw, kw))
					}
				}
				// A real summary should be substantive — at least 300
				// chars and contain enumeration cues (bullets, numbers).
				if len(strings.TrimSpace(r.Reply)) < 300 {
					miss = append(miss, fmt.Sprintf("summary too short: %d chars (real summary of a multi-section article should be >= 300)", len(r.Reply)))
				}
				if !strings.Contains(r.Reply, "-") && !strings.Contains(r.Reply, "•") && !strings.Contains(r.Reply, "1.") && !strings.Contains(r.Reply, "1)") {
					miss = append(miss, "reply has no bullet/numbered enumeration despite the prompt asking for 5 bullet points")
				}
				return miss
			},
		},

		// 5. doc-xlsx — generate a workbook, fetch the bytes, parse the
		//    sharedStrings table, assert the requested cells round-trip.
		{
			Name:   "doc-xlsx-roundtrip",
			Prompt: fmt.Sprintf("Generami un file Excel chiamato probe-%s.xlsx con un foglio 'Sintesi' che ha questa tabella: prima riga 'Voce' e 'Valore', poi righe ['Anno', '2026'], ['Wave', '2.7d'], ['Marker', 'PROBE-%s']. Non inviarlo via Telegram (deliver:false). Confermami il source_id.", stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc xlsx)")
				}
				id := findSourceID(r.Reply)
				if id == "" {
					miss = append(miss, "reply does not include a src_xxx identifier")
					return miss
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				// xlsx is a ZIP — sanity check magic header.
				if len(body) < 4 || string(body[:2]) != "PK" {
					miss = append(miss, fmt.Sprintf("not a valid xlsx zip (head=%q)", body[:min(len(body), 8)]))
					return miss
				}
				// All cell strings live in xl/sharedStrings.xml as <t>...</t>.
				strs, err := extractTextFromZipEntry(body, "xl/sharedStrings.xml", "t")
				if err != nil {
					miss = append(miss, fmt.Sprintf("parse sharedStrings: %v", err))
					return miss
				}
				for _, want := range []string{"Voce", "Valore", "Anno", "2026", "Wave", "2.7d", "Marker", "PROBE-" + stamp} {
					if !strings.Contains(strs, want) {
						miss = append(miss, fmt.Sprintf("sharedStrings missing %q (got: %q)", want, truncate(strs, 200)))
					}
				}
				return miss
			},
		},

		// 6. doc-docx — generate a Word doc, fetch, parse word/document.xml,
		//    assert the requested headings/paragraphs round-trip.
		{
			Name:   "doc-docx-roundtrip",
			Prompt: fmt.Sprintf("Generami un file Word chiamato probe-%s.docx con titolo 'Probe Docx %s' e questi blocchi: heading livello 2 testo 'Sezione A', paragraph 'Frase distintiva PROBE-%s', bullet 'Punto uno'. deliver:false. Confermami il source_id.", stamp, stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc docx)")
				}
				id := findSourceID(r.Reply)
				if id == "" {
					miss = append(miss, "reply does not include a src_xxx identifier")
					return miss
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				if len(body) < 4 || string(body[:2]) != "PK" {
					miss = append(miss, fmt.Sprintf("not a valid docx zip (head=%q)", body[:min(len(body), 8)]))
					return miss
				}
				// docx text lives in word/document.xml as <w:t>...</w:t>.
				body_text, err := extractTextFromZipEntry(body, "word/document.xml", "w:t")
				if err != nil {
					miss = append(miss, fmt.Sprintf("parse word/document.xml: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Docx " + stamp, "Sezione A", "PROBE-" + stamp, "Punto uno"} {
					if !strings.Contains(body_text, want) {
						miss = append(miss, fmt.Sprintf("docx text missing %q (got: %q)", want, truncate(body_text, 200)))
					}
				}
				return miss
			},
		},

		// 7. doc-pdf — generate a PDF, fetch, extract text via a real PDF
		//    parser, assert the requested content round-trips.
		{
			Name:   "doc-pdf-roundtrip",
			Prompt: fmt.Sprintf("Generami un file PDF chiamato probe-%s.pdf con titolo 'Probe Pdf %s' e due blocchi: heading livello 2 'Risultati', paragraph 'Esito atteso PROBE-%s'. deliver:false. Confermami il source_id.", stamp, stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc pdf)")
				}
				id := findSourceID(r.Reply)
				if id == "" {
					miss = append(miss, "reply does not include a src_xxx identifier")
					return miss
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				if len(body) < 4 || string(body[:4]) != "%PDF" {
					miss = append(miss, fmt.Sprintf("not a valid pdf (head=%q)", body[:min(len(body), 8)]))
					return miss
				}
				text, err := extractPDFText(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("extract pdf text: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Pdf " + stamp, "Risultati", "PROBE-" + stamp} {
					if !strings.Contains(text, want) {
						miss = append(miss, fmt.Sprintf("pdf text missing %q (got: %q)", want, truncate(text, 200)))
					}
				}
				return miss
			},
		},

		// 8. phantom-trap — non-existent task name; model MUST NOT claim it ran.
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
