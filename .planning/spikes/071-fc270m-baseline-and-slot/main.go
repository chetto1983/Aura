// Spike 071 — FunctionGemma-270m baseline + production-slot probe.
//
// Question: can BASE (un-finetuned) FunctionGemma-270m emit valid tool calls
// against Aura's REAL tool schemas, and at what latency on this hardware? This
// is the kill-gate for the finetune idea: if the base model is hopeless AND
// latency/VRAM are fatal, finetuning cannot save it.
//
// The harness builds Aura's actual deferred ToolSpecs (zero-value Spec() is safe
// — spike 054 convention) plus a hand-authored mail__* namespace cluster (the
// documented 16-tool "gravity well"), converts them to the OpenAI tools wire
// shape, and feeds grounded queries (bitcoin/meteo/mail/doc-search — spikes
// 052/054/057) to a llama.cpp server hosting the FunctionGemma GGUF.
//
// FunctionGemma uses a NON-STANDARD call format (research, spike README):
//
//	<start_function_call>call:web_search{query: "..."}<end_function_call>
//
// so the harness parses BOTH the OpenAI-parsed tool_calls[] (if llama.cpp's
// --jinja parser handled it) AND the raw content (regex the special tokens).
//
// Run (llama.cpp serving FunctionGemma on :8099, see README):
//
//	FC_BASE_URL=http://127.0.0.1:8099 FC_TAG=cpu-q8 go run ./.planning/spikes/071-fc270m-baseline-and-slot
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("FC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8099"
}

func tag() string {
	if v := strings.TrimSpace(os.Getenv("FC_TAG")); v != "" {
		return v
	}
	return "default"
}

func temp() float64 {
	// Greedy (0) by default for reproducible scoring; FC_TEMP=1.0 exercises
	// Google's recommended sampling (top_k=64/top_p=0.95) as a spot check.
	if os.Getenv("FC_TEMP") == "1.0" {
		return 1.0
	}
	return 0
}

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), cat, fmt.Sprintf(format, a...))
}

// ---- OpenAI wire shapes -----------------------------------------------------

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type oaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Messages    []oaMsg  `json:"messages"`
	Tools       []oaTool `json:"tools,omitempty"`
	ToolChoice  string   `json:"tool_choice,omitempty"`
	Temperature float64  `json:"temperature"`
	TopK        int      `json:"top_k,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	MaxTokens   int      `json:"max_tokens"`
	Stream      bool     `json:"stream"`
	Seed        int      `json:"seed,omitempty"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Timings *struct {
		PromptN            int     `json:"prompt_n"`
		PredictedN         int     `json:"predicted_n"`
		PredictedPerSecond float64 `json:"predicted_per_second"`
	} `json:"timings"`
}

// ---- Aura tool corpus -------------------------------------------------------

func auraTools() []oaTool {
	reg := tools.NewRegistry()
	for _, t := range []tools.Tool{
		&tools.WebSearch{}, &tools.WebFetch{}, &tools.DocumentSearch{},
		tools.CurrentTime{}, &tools.SendFile{}, &tools.FSRead{}, &tools.FSGlob{},
		&tools.FSGrep{}, &tools.FSWrite{}, &tools.FSEdit{}, &tools.ShellExec{},
	} {
		reg.Register(t)
	}
	var out []oaTool
	for _, e := range reg.Render() {
		// Promote deferred specs to their FULL form (description+params) — the
		// shape tool_search hands the model after a hit. Render() blanks deferred
		// description/params, so re-pull each Spec directly.
		out = append(out, specToOpenAI(reg, e.Name))
	}
	// Hand-authored mail__* namespace cluster (mirrors the real mail-mcp surface,
	// spike 054) — recreates the documented intra-namespace "gravity well".
	out = append(out, mailTools()...)
	sort.Slice(out, func(i, j int) bool { return out[i].Function.Name < out[j].Function.Name })
	return out
}

func specToOpenAI(reg *tools.Registry, name string) oaTool {
	var t oaTool
	for _, tool := range reg.All() {
		s := tool.Spec()
		if s.Name != name {
			continue
		}
		t.Type = "function"
		t.Function.Name = s.Name
		t.Function.Description = s.Description
		if len(s.Parameters) > 0 {
			t.Function.Parameters = append(json.RawMessage(nil), s.Parameters...)
		}
		return t
	}
	return t
}

func mkTool(name, desc, params string) oaTool {
	var t oaTool
	t.Type = "function"
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(params)
	return t
}

func mailTools() []oaTool {
	return []oaTool{
		mkTool("mail__send_email", "Send an email to one or more recipients with a subject and body.",
			`{"type":"object","properties":{"to":{"type":"string","description":"Recipient name or address"},"subject":{"type":"string"},"body":{"type":"string"}},"required":["to","body"]}`),
		mkTool("mail__search_emails", "Search the mailbox for emails matching a query string.",
			`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		mkTool("mail__delete_mailbox", "Permanently delete an entire mailbox folder by name.",
			`{"type":"object","properties":{"mailbox":{"type":"string"}},"required":["mailbox"]}`),
		mkTool("mail__mark_email", "Mark an email as read or unread by id.",
			`{"type":"object","properties":{"id":{"type":"string"},"read":{"type":"boolean"}},"required":["id"]}`),
	}
}

// ---- Grounded eval set ------------------------------------------------------

type qcase struct {
	q        string
	gold     string // gold tool name (full, incl. namespace)
	argSub   string // expected substring in the emitted args (lowercased); "" = skip arg check
	category string
}

func cases() []qcase {
	if strings.EqualFold(os.Getenv("FC_LANG"), "en") {
		return []qcase{
			{"how much does bitcoin cost right now?", "web_search", "bitcoin", "price-gravity-well"},
			{"what is the weather in Turin tomorrow?", "web_search", "turin", "weather"},
			{"find the latest news about Cuneo", "web_search", "cuneo", "news"},
			{"send an email to Marco saying I will be late", "mail__send_email", "marco", "mail-namespace"},
			{"search my documents for the rental contract", "document_search", "contract", "doc-search"},
			{"what time is it now?", "current_time", "", "time"},
			{"read the file config.yaml", "fs_read", "config.yaml", "fs-read"},
			{"find all files with the .go extension in the project", "fs_glob", ".go", "fs-glob"},
			{"download the content of the page https://example.com", "web_fetch", "example.com", "web-fetch"},
			{"run the build.sh script", "shell_exec", "build.sh", "shell"},
			{"search for the word TODO in the code", "fs_grep", "todo", "fs-grep"},
			{"send me the report.pdf file", "send_file", "report.pdf", "send-file"},
		}
	}
	return []qcase{
		{"quanto costa il bitcoin adesso?", "web_search", "bitcoin", "price-gravity-well"},
		{"che tempo fa a Torino domani?", "web_search", "torino", "weather"},
		{"cerca le ultime notizie su Cuneo", "web_search", "cuneo", "news"},
		{"manda una mail a Marco dicendo che arrivo tardi", "mail__send_email", "marco", "mail-namespace"},
		{"cerca nei miei documenti il contratto di noleggio", "document_search", "contratto", "doc-search"},
		{"che ore sono adesso?", "current_time", "", "time"},
		{"leggi il file config.yaml", "fs_read", "config.yaml", "fs-read"},
		{"trova tutti i file con estensione .go nel progetto", "fs_glob", ".go", "fs-glob"},
		{"scarica il contenuto della pagina https://example.com", "web_fetch", "example.com", "web-fetch"},
		{"esegui lo script build.sh", "shell_exec", "build.sh", "shell"},
		{"cerca la parola TODO nel codice", "fs_grep", "todo", "fs-grep"},
		{"inviami il file report.pdf", "send_file", "report.pdf", "send-file"},
	}
}

// ---- call parsing -----------------------------------------------------------

// FunctionGemma raw format: <start_function_call>call:NAME{args}<end_function_call>
var rawCallRe = regexp.MustCompile(`(?s)<start_function_call>\s*call:\s*([A-Za-z0-9_]+)\s*\{(.*?)<end_function_call>`)

type parsedCall struct {
	name string
	args string
	via  string // "tool_calls" | "raw" | ""
}

func parseCall(r chatResp) parsedCall {
	if len(r.Choices) == 0 {
		return parsedCall{}
	}
	m := r.Choices[0].Message
	if len(m.ToolCalls) > 0 {
		return parsedCall{name: m.ToolCalls[0].Function.Name, args: m.ToolCalls[0].Function.Arguments, via: "tool_calls"}
	}
	if mm := rawCallRe.FindStringSubmatch(m.Content); mm != nil {
		return parsedCall{name: mm[1], args: strings.TrimSuffix(strings.TrimSpace(mm[2]), "}"), via: "raw"}
	}
	return parsedCall{}
}

func sameTool(emitted, gold string) bool {
	if emitted == gold {
		return true
	}
	return strings.HasSuffix(emitted, "__"+gold) || strings.HasSuffix(gold, "__"+emitted)
}

// ---- run --------------------------------------------------------------------

func post(url string, body chatReq) (chatResp, time.Duration, error) {
	b, _ := json.Marshal(body)
	start := time.Now()
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return chatResp{}, time.Since(start), err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	dur := time.Since(start)
	if resp.StatusCode != 200 {
		return chatResp{}, dur, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatResp{}, dur, fmt.Errorf("decode: %w (body=%s)", err, truncate(string(raw), 300))
	}
	return cr, dur, nil
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func main() {
	url := baseURL() + "/v1/chat/completions"
	corpus := auraTools()
	cs := cases()
	logf("SETUP", "tag=%s url=%s temp=%.1f tools=%d cases=%d", tag(), url, temp(), len(corpus), len(cs))
	names := make([]string, 0, len(corpus))
	for _, t := range corpus {
		names = append(names, t.Function.Name)
	}
	logf("CORPUS", "%s", strings.Join(names, ", "))

	// Warmup (load weights / fill caches; not scored).
	if _, _, err := post(url, chatReq{Messages: []oaMsg{{Role: "user", Content: "ciao"}}, Tools: corpus, Temperature: 0, MaxTokens: 64, ToolChoice: "auto"}); err != nil {
		logf("FATAL", "warmup failed (is the server up?): %v", err)
		os.Exit(1)
	}
	logf("WARMUP", "ok")

	var emitted, top1, argOK, validParse int
	var lat []time.Duration
	viaCount := map[string]int{}
	for i, c := range cs {
		msgs := []oaMsg{{Role: "user", Content: c.q}}
		if os.Getenv("FC_SYS") == "1" {
			// FunctionGemma's documented activation preamble. The llama.cpp GGUF
			// template defaults to "You are a helpful assistant" (wrong preamble),
			// so test whether the correct one lifts the refusal rate.
			msgs = append([]oaMsg{{Role: "system", Content: "You are a model that can do function calling with the following functions"}}, msgs...)
		}
		req := chatReq{
			Messages:    msgs,
			Tools:       corpus,
			ToolChoice:  "auto",
			Temperature: temp(),
			MaxTokens:   256,
			Seed:        3407,
		}
		if temp() > 0 {
			req.TopK, req.TopP = 64, 0.95
		}
		r, dur, err := post(url, req)
		if err != nil {
			logf("ERROR", "case %d %q: %v", i+1, c.category, err)
			continue
		}
		lat = append(lat, dur)
		pc := parseCall(r)
		hasCall := pc.name != ""
		if hasCall {
			emitted++
			validParse++
			viaCount[pc.via]++
		}
		isTop1 := hasCall && sameTool(pc.name, c.gold)
		if isTop1 {
			top1++
		}
		argHit := isTop1 && (c.argSub == "" || strings.Contains(strings.ToLower(pc.args), c.argSub))
		if argHit {
			argOK++
		}
		verdict := "MISS"
		switch {
		case argHit:
			verdict = "OK"
		case isTop1:
			verdict = "TOOL-ONLY"
		case hasCall:
			verdict = "WRONG-TOOL"
		case !hasCall:
			verdict = "NO-CALL"
		}
		preview := pc.name + "(" + truncate(pc.args, 80) + ")"
		if !hasCall {
			preview = truncate(firstContent(r), 90)
		}
		logf("CASE", "%-20s gold=%-18s -> %-10s via=%-10s %dms | %s", c.category, c.gold, verdict, pc.via, dur.Milliseconds(), preview)
	}

	n := len(cs)
	logf("SCORE", "emitted-call %d/%d | top1-tool %d/%d | arg-correct %d/%d | valid-parse %d/%d",
		emitted, n, top1, n, argOK, n, validParse, n)
	logf("PARSE-VIA", "tool_calls=%d raw=%d", viaCount["tool_calls"], viaCount["raw"])
	if len(lat) > 0 {
		logf("LATENCY", "n=%d p50=%dms p95=%dms min=%dms max=%dms",
			len(lat), pct(lat, 50).Milliseconds(), pct(lat, 95).Milliseconds(),
			minD(lat).Milliseconds(), maxD(lat).Milliseconds())
	}
	// VALIDATED here means "the harness ran and produced a real baseline", not
	// "the model is good" — the verdict on quality is in the README.
	logf("SUMMARY", "tag=%s baseline captured: top1=%d/%d arg=%d/%d p50=%dms", tag(), top1, n, argOK, n, pct(lat, 50).Milliseconds())
}

func firstContent(r chatResp) string {
	if len(r.Choices) == 0 {
		return "(no choices)"
	}
	return r.Choices[0].Message.Content
}

func pct(d []time.Duration, p int) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := (p * (len(s) - 1)) / 100
	return s[idx]
}

func minD(d []time.Duration) time.Duration { return pct(d, 0) }
func maxD(d []time.Duration) time.Duration { return pct(d, 100) }
