// Spike 073 — FunctionGemma training dataset from Aura's real tool registry.
//
// Produces an SFT dataset in the HF tool-calling shape the official Unsloth
// FunctionGemma-270M notebook consumes after a one-cell dataset swap:
//
//	{"messages":[{"role":"user","content":"…"},
//	             {"role":"assistant","tool_calls":[{"type":"function",
//	               "function":{"name":"web_search","arguments":{…}}}]}],
//	 "tools":[<FULL Aura capability catalog>]}
//
// The notebook formats each row with
//
//	tokenizer.apply_chat_template(row["messages"], tools=row["tools"],
//	                              add_generation_prompt=False, tokenize=False)
//
// then train_on_responses_only masks everything before "<start_of_turn>model\n".
// No developer message is needed in messages — the template injects FunctionGemma's
// developer preamble + <start_function_declaration> block from tools=.
//
// Design decisions (operator, spike 073):
//   - FULL tool catalog in every example (teach selection-under-distractors —
//     directly targets the spike-071 bitcoin→mail gravity well + fs_glob→fs_read).
//   - Italian-primary ~70/30 (base model refuses Italian; that is the gap to close).
//   - Native tool schemas pulled from the REAL registry (internal/agent/tools);
//     MCP namespaces (mail/whatsapp/calendar/memory) from a static catalog mirroring
//     the live surfaces (spikes 054/063). Every gold call is schema-valid.
//
// Run: go run ./.planning/spikes/073-fc270m-dataset-from-registry
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// ---- wire shapes (HF tool-calling) -----------------------------------------

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type tcFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolCall struct {
	Type     string     `json:"type"`
	Function tcFunction `json:"function"`
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type row struct {
	Messages []message `json:"messages"`
	Tools    []oaTool  `json:"tools"`
	gold     string    // not serialized — used for stratified split + coverage
	lang     string
}

// ---- catalog ----------------------------------------------------------------

func nativeTool(name string) oaTool {
	reg := tools.NewRegistry()
	for _, t := range []tools.Tool{
		&tools.WebSearch{}, &tools.WebFetch{}, &tools.DocumentSearch{},
		tools.CurrentTime{}, &tools.SendFile{}, &tools.FSRead{}, &tools.FSGlob{},
		&tools.FSGrep{}, &tools.FSWrite{}, &tools.FSEdit{}, &tools.ShellExec{},
	} {
		reg.Register(t)
	}
	for _, t := range reg.All() {
		s := t.Spec()
		if s.Name != name {
			continue
		}
		var o oaTool
		o.Type = "function"
		o.Function.Name = s.Name
		o.Function.Description = s.Description
		o.Function.Parameters = append(json.RawMessage(nil), s.Parameters...)
		return o
	}
	panic("native tool not found: " + name)
}

func mkTool(name, desc, params string) oaTool {
	var o oaTool
	o.Type = "function"
	o.Function.Name = name
	o.Function.Description = desc
	o.Function.Parameters = json.RawMessage(params)
	return o
}

// mcpCatalog mirrors the live MCP surfaces (mail/whatsapp/calendar/memory) so the
// dataset teaches namespace disambiguation (the documented gravity well).
func mcpCatalog() []oaTool {
	return []oaTool{
		mkTool("mail__send_email", "Send an email to a recipient with an optional subject and a body.",
			`{"type":"object","properties":{"to":{"type":"string","description":"Recipient name or email address"},"subject":{"type":"string"},"body":{"type":"string"}},"required":["to","body"]}`),
		mkTool("mail__search_emails", "Search the mailbox for emails matching a query string.",
			`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		mkTool("mail__mark_email", "Mark an email as read or unread by id.",
			`{"type":"object","properties":{"id":{"type":"string"},"read":{"type":"boolean"}},"required":["id"]}`),
		mkTool("mail__delete_mailbox", "Permanently delete an entire mailbox folder by name.",
			`{"type":"object","properties":{"mailbox":{"type":"string"}},"required":["mailbox"]}`),
		mkTool("whatsapp__send_message", "Send a WhatsApp message to a contact.",
			`{"type":"object","properties":{"to":{"type":"string"},"message":{"type":"string"}},"required":["to","message"]}`),
		mkTool("whatsapp__search_contacts", "Search WhatsApp contacts by name or number.",
			`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		mkTool("calendar__create_event", "Create a calendar event with a title and a start time.",
			`{"type":"object","properties":{"title":{"type":"string"},"start":{"type":"string","description":"Natural-language or ISO start time"},"end":{"type":"string"}},"required":["title","start"]}`),
		mkTool("calendar__list_events", "List upcoming calendar events in a time range.",
			`{"type":"object","properties":{"range":{"type":"string","description":"e.g. today, tomorrow, this week"}},"required":["range"]}`),
		mkTool("memory__search", "Search the user's long-term memory graph for facts/preferences.",
			`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		mkTool("memory__add", "Store a new fact or preference in the user's long-term memory.",
			`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`),
	}
}

func catalog() []oaTool {
	native := []string{"web_search", "web_fetch", "document_search", "current_time",
		"send_file", "fs_read", "fs_glob", "fs_grep", "fs_write", "fs_edit", "shell_exec"}
	var c []oaTool
	for _, n := range native {
		c = append(c, nativeTool(n))
	}
	c = append(c, mcpCatalog()...)
	sort.Slice(c, func(i, j int) bool { return c[i].Function.Name < c[j].Function.Name })
	return c
}

// ---- templates --------------------------------------------------------------

type tmpl struct {
	lang string
	gold string
	pat  string // %s = slot
	vals []string
	arg  func(v string) map[string]any
}

func a(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func templates() []tmpl {
	return []tmpl{
		// web_search — counter the bitcoin/price gravity well (NOT mail) + meteo/news
		{"it", "web_search", "quanto costa %s adesso?", []string{"il bitcoin", "ethereum", "l'oro", "il petrolio", "il gas", "un'azione Tesla"},
			func(v string) map[string]any { return a("query", v+" prezzo attuale", "category", "general") }},
		{"it", "web_search", "che tempo fa a %s domani?", []string{"Torino", "Milano", "Roma", "Cuneo", "Napoli"},
			func(v string) map[string]any { return a("query", "meteo "+v+" domani") }},
		{"it", "web_search", "cerca le ultime notizie su %s", []string{"Cuneo", "l'AI Act europeo", "l'inflazione", "le elezioni"},
			func(v string) map[string]any {
				return a("query", "notizie "+v, "category", "news", "time_range", "week")
			}},
		{"it", "web_search", "cerca sul web %s", []string{"ricette di pasta veloci", "hotel economici a Parigi", "come si cambia una gomma"},
			func(v string) map[string]any { return a("query", v) }},
		{"en", "web_search", "how much does %s cost right now?", []string{"bitcoin", "ethereum", "gold"},
			func(v string) map[string]any { return a("query", v+" current price", "category", "general") }},
		{"en", "web_search", "find the latest news about %s", []string{"the EU AI Act", "NVIDIA earnings"},
			func(v string) map[string]any {
				return a("query", "latest news "+v, "category", "news", "time_range", "week")
			}},

		// web_fetch
		{"it", "web_fetch", "scarica il contenuto della pagina %s", []string{"https://example.com", "https://it.wikipedia.org/wiki/Torino"},
			func(v string) map[string]any { return a("url", v) }},
		{"it", "web_fetch", "apri %s e dimmi cosa dice", []string{"https://news.ycombinator.com", "https://example.org/post"},
			func(v string) map[string]any { return a("url", v) }},
		{"en", "web_fetch", "fetch the content of %s", []string{"https://example.org", "https://en.wikipedia.org/wiki/Go"},
			func(v string) map[string]any { return a("url", v) }},

		// document_search — counter doc→web_fetch confusion
		{"it", "document_search", "cerca nei miei documenti %s", []string{"il contratto di noleggio", "la fattura di marzo", "il manuale della caldaia"},
			func(v string) map[string]any { return a("query", v) }},
		{"it", "document_search", "trova nei documenti caricati %s", []string{"la garanzia del frigorifero", "le condizioni della polizza"},
			func(v string) map[string]any { return a("query", v) }},
		{"en", "document_search", "search my documents for %s", []string{"the rental contract", "the boiler manual"},
			func(v string) map[string]any { return a("query", v) }},

		// current_time (no slot)
		{"it", "current_time", "%s", []string{"che ore sono adesso?", "che giorno è oggi?", "dimmi data e ora"},
			func(string) map[string]any { return a() }},
		{"en", "current_time", "%s", []string{"what time is it now?", "what is today's date?"},
			func(string) map[string]any { return a() }},

		// fs_read
		{"it", "fs_read", "leggi il file %s", []string{"config.yaml", "README.md", "main.go", "/etc/hosts"},
			func(v string) map[string]any { return a("path", v) }},
		{"en", "fs_read", "read the file %s", []string{"config.yaml", "package.json"},
			func(v string) map[string]any { return a("path", v) }},

		// fs_glob — counter the fs_glob→fs_read confusion
		{"it", "fs_glob", "trova tutti i file con estensione %s nel progetto", []string{"go", "md", "yaml", "ts"},
			func(v string) map[string]any { return a("pattern", "**/*."+v) }},
		{"it", "fs_glob", "elenca i file che corrispondono a %s", []string{"src/**/*.ts", "*.json", "test_*.py"},
			func(v string) map[string]any { return a("pattern", v) }},
		{"en", "fs_glob", "find all %s files in the project", []string{".go", ".py"},
			func(v string) map[string]any { return a("pattern", "**/*"+v) }},

		// fs_grep
		{"it", "fs_grep", "cerca la parola %s nel codice", []string{"TODO", "FIXME", "panic(", "func main"},
			func(v string) map[string]any { return a("pattern", v) }},
		{"en", "fs_grep", "grep for %s in the codebase", []string{"TODO", "error"},
			func(v string) map[string]any { return a("pattern", v) }},

		// fs_write
		{"it", "fs_write", "crea un file %s con scritto ciao", []string{"note.txt", "appunti.md"},
			func(v string) map[string]any { return a("path", v, "content", "ciao") }},
		{"en", "fs_write", "write 'hello world' to %s", []string{"out.txt", "hello.md"},
			func(v string) map[string]any { return a("path", v, "content", "hello world") }},

		// fs_edit
		{"it", "fs_edit", "nel file %s sostituisci foo con bar", []string{"config.go", "settings.py"},
			func(v string) map[string]any { return a("path", v, "old_string", "foo", "new_string", "bar") }},
		{"en", "fs_edit", "in %s replace DEBUG with INFO", []string{"logger.go"},
			func(v string) map[string]any { return a("path", v, "old_string", "DEBUG", "new_string", "INFO") }},

		// shell_exec
		{"it", "shell_exec", "esegui il comando %s", []string{"ls -la", "git status", "npm test", "./build.sh"},
			func(v string) map[string]any { return a("command", v) }},
		{"en", "shell_exec", "run %s", []string{"docker ps", "go build ./..."},
			func(v string) map[string]any { return a("command", v) }},

		// send_file
		{"it", "send_file", "inviami il file %s", []string{"report.pdf", "foto.jpg", "bilancio.xlsx"},
			func(v string) map[string]any { return a("path", v) }},
		{"en", "send_file", "send me the file %s", []string{"invoice.pdf"},
			func(v string) map[string]any { return a("path", v) }},

		// mail__send_email — counter bitcoin/news bleeding into mail; real mail intent
		{"it", "mail__send_email", "manda una mail a %s dicendo che arrivo tardi", []string{"Marco", "marco@example.com", "il capo"},
			func(v string) map[string]any { return a("to", v, "body", "Arrivo un po' in ritardo, scusa.") }},
		{"it", "mail__send_email", "scrivi un'email a %s per confermare l'appuntamento", []string{"Anna", "lo studio medico"},
			func(v string) map[string]any {
				return a("to", v, "subject", "Conferma appuntamento", "body", "Confermo l'appuntamento.")
			}},
		{"en", "mail__send_email", "send an email to %s saying I will be late", []string{"Marco"},
			func(v string) map[string]any { return a("to", v, "body", "I will be a bit late.") }},

		// mail__search_emails — counter the mail__search vs document_search/web_search lines
		{"it", "mail__search_emails", "cerca nelle mie email i messaggi di %s", []string{"Marco", "la banca", "Amazon"},
			func(v string) map[string]any { return a("query", v) }},
		{"en", "mail__search_emails", "search my inbox for emails from %s", []string{"the bank"},
			func(v string) map[string]any { return a("query", v) }},

		// mail__mark_email / delete (rarer, intra-namespace contrast)
		{"it", "mail__mark_email", "segna come letta l'email %s", []string{"abc123", "msg-42", "id-7", "th-99", "x12"},
			func(v string) map[string]any { return a("id", v, "read", true) }},
		{"it", "mail__delete_mailbox", "elimina la cartella di posta %s", []string{"Spam", "Cestino", "Promozioni", "Archivio", "Bozze"},
			func(v string) map[string]any { return a("mailbox", v) }},

		// whatsapp
		{"it", "whatsapp__send_message", "manda un messaggio whatsapp a %s", []string{"Luca", "mamma", "+39 333 1234567"},
			func(v string) map[string]any { return a("to", v, "message", "Ciao, ci sentiamo dopo!") }},
		{"en", "whatsapp__send_message", "send a whatsapp to %s", []string{"John"},
			func(v string) map[string]any { return a("to", v, "message", "Hey, talk later!") }},
		{"it", "whatsapp__search_contacts", "trova il contatto whatsapp di %s", []string{"Luca", "Giulia", "Marco Rossi", "il dentista", "papà"},
			func(v string) map[string]any { return a("query", v) }},

		// calendar
		{"it", "calendar__create_event", "crea un evento %s domani alle 15", []string{"riunione di team", "visita dal dentista", "chiamata con il cliente", "cena con amici"},
			func(v string) map[string]any { return a("title", v, "start", "domani 15:00") }},
		{"en", "calendar__create_event", "schedule %s for tomorrow at 3pm", []string{"a team meeting", "a dentist appointment"},
			func(v string) map[string]any { return a("title", v, "start", "tomorrow 15:00") }},
		{"it", "calendar__list_events", "che impegni ho %s?", []string{"oggi", "domani", "questa settimana"},
			func(v string) map[string]any { return a("range", v) }},
		{"en", "calendar__list_events", "what is on my calendar %s?", []string{"today", "tomorrow"},
			func(v string) map[string]any { return a("range", v) }},

		// memory
		{"it", "memory__search", "cosa ricordi su %s?", []string{"le mie preferenze di caffè", "il progetto Aura", "la mia dieta", "i miei progetti di lavoro", "il wifi di casa"},
			func(v string) map[string]any { return a("query", v) }},
		{"it", "memory__add", "ricordati che %s", []string{"preferisco il caffè senza zucchero", "il mio compleanno è il 12 marzo", "non mangio glutine", "lavoro da remoto il venerdì", "la mia auto è una Panda"},
			func(v string) map[string]any { return a("content", v) }},
	}
}

// chatExamples curb over-firing: a small set of no-tool turns (assistant emits text).
func chatExamples() []row {
	cat := catalog()
	mk := func(lang, q, ans string) row {
		return row{Messages: []message{{Role: "user", Content: q}, {Role: "assistant", Content: ans}}, Tools: cat, gold: "(none)", lang: lang}
	}
	return []row{
		mk("it", "ciao", "Ciao! Come posso aiutarti?"),
		mk("it", "grazie mille", "Di nulla, figurati!"),
		mk("it", "come ti chiami?", "Sono Aura, il tuo assistente personale."),
		mk("en", "hello", "Hi! How can I help you?"),
		mk("en", "thank you", "You're welcome!"),
	}
}

// ---- build ------------------------------------------------------------------

func buildRows() []row {
	cat := catalog()
	var rows []row
	for _, t := range templates() {
		for _, v := range t.vals {
			q := t.pat
			if strings.Contains(q, "%s") {
				q = fmt.Sprintf(t.pat, v)
			}
			rows = append(rows, row{
				Messages: []message{
					{Role: "user", Content: q},
					{Role: "assistant", ToolCalls: []toolCall{{Type: "function", Function: tcFunction{Name: t.gold, Arguments: t.arg(v)}}}},
				},
				Tools: cat,
				gold:  t.gold,
				lang:  t.lang,
			})
		}
	}
	rows = append(rows, chatExamples()...)
	return rows
}

// stratifiedSplit holds out ~1 example per gold tool (and a fraction of large
// groups) so eval covers every tool without starving train.
func stratifiedSplit(rows []row, rng *rand.Rand) (train, eval []row) {
	byGold := map[string][]row{}
	for _, r := range rows {
		byGold[r.gold] = append(byGold[r.gold], r)
	}
	golds := make([]string, 0, len(byGold))
	for g := range byGold {
		golds = append(golds, g)
	}
	sort.Strings(golds)
	for _, g := range golds {
		grp := byGold[g]
		rng.Shuffle(len(grp), func(i, j int) { grp[i], grp[j] = grp[j], grp[i] })
		nEval := 1 + len(grp)/6 // ~1 + 15%
		if nEval >= len(grp) {
			nEval = 1
		}
		eval = append(eval, grp[:nEval]...)
		train = append(train, grp[nEval:]...)
	}
	rng.Shuffle(len(train), func(i, j int) { train[i], train[j] = train[j], train[i] })
	rng.Shuffle(len(eval), func(i, j int) { eval[i], eval[j] = eval[j], eval[i] })
	return train, eval
}

func writeJSONL(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	outDir := filepath.Join(".planning", "spikes", "073-fc270m-dataset-from-registry")
	rng := rand.New(rand.NewSource(3407))
	all := buildRows()
	train, eval := stratifiedSplit(all, rng)
	cat := catalog()

	if err := writeJSONL(filepath.Join(outDir, "train.jsonl"), train); err != nil {
		fmt.Println("write train:", err)
		os.Exit(1)
	}
	if err := writeJSONL(filepath.Join(outDir, "eval.jsonl"), eval); err != nil {
		fmt.Println("write eval:", err)
		os.Exit(1)
	}

	// Coverage report.
	perTool := map[string]int{}
	perLang := map[string]int{}
	for _, r := range all {
		perTool[r.gold]++
		perLang[r.lang]++
	}
	names := make([]string, 0, len(perTool))
	for n := range perTool {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "# Spike 073 — dataset coverage\n\n")
	fmt.Fprintf(&b, "Generated by `go run ./.planning/spikes/073-fc270m-dataset-from-registry` (seed 3407, deterministic).\n\n")
	fmt.Fprintf(&b, "- **Total examples:** %d (train %d / eval %d)\n", len(all), len(train), len(eval))
	fmt.Fprintf(&b, "- **Catalog size (tools per example):** %d (full catalog every row)\n", len(cat))
	fmt.Fprintf(&b, "- **Language mix:** it %d (%.0f%%) / en %d (%.0f%%)\n",
		perLang["it"], 100*float64(perLang["it"])/float64(len(all)), perLang["en"], 100*float64(perLang["en"])/float64(len(all)))
	fmt.Fprintf(&b, "- **Format:** HF `{messages, tools}`; assistant `tool_calls[].function.arguments` is an object.\n\n")
	fmt.Fprintf(&b, "## Examples per gold tool\n\n| Gold tool | Examples |\n|---|---|\n")
	for _, n := range names {
		fmt.Fprintf(&b, "| %s | %d |\n", n, perTool[n])
	}
	fmt.Fprintf(&b, "\n## Catalog (shown in every example)\n\n")
	for _, t := range cat {
		fmt.Fprintf(&b, "- `%s`\n", t.Function.Name)
	}
	if err := os.WriteFile(filepath.Join(outDir, "COVERAGE.md"), []byte(b.String()), 0o644); err != nil {
		fmt.Println("write coverage:", err)
		os.Exit(1)
	}

	fmt.Printf("[OK] wrote train.jsonl (%d) eval.jsonl (%d) COVERAGE.md | catalog=%d tools | it/en=%d/%d\n",
		len(train), len(eval), len(cat), perLang["it"], perLang["en"])
}
