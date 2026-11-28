// Probe of deferred tool loading on the Claude route (ledger 2026-11-27-anthropic-provider,
// "VM read-back 2"). It measures whether sending every deferred tool from the first request
// with defer_loading: true, and answering Aura's own tool_search with tool_reference blocks,
// keeps the cached prefix and the signed thinking that today's growing tools array loses.
// Bodies are raw JSON through the SDK's Client.Post; the key is read from -keyfile and never
// printed.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

type obj = map[string]any

const bindingBeta = "thinking-binding-controls-2026-08-01"

// rates are USD per million tokens. The defaults are Sonnet 5.5's list prices as recorded in
// docs/superpowers/specs (input 2, output 10, cache read 0.10, 5-minute cache write 2.50).
type rates struct{ in, out, read, write float64 }

var (
	client  anthropic.Client
	model   string
	display string
	effort  string
	shape   string
	price   rates
	spent   float64
	salt    string
)

func main() {
	keyfile := flag.String("keyfile", "", "file holding the API key")
	flag.StringVar(&model, "model", "claude-sonnet-5-5", "model id")
	caseName := flag.String("case", "all", "contract, baseline, defer (runs the replay too), unreferenced, or all")
	flag.StringVar(&display, "display", "omitted", "thinking.display: omitted (Aura's default) or summarized")
	flag.StringVar(&effort, "effort", "high", "output_config.effort")
	flag.StringVar(&shape, "result", "mixed", "tool_search result in the defer case: mixed (short text + reference), refs (reference only), spec (Aura's full spec text + reference)")
	flag.Float64Var(&price.in, "price-in", 2, "input USD per 1M")
	flag.Float64Var(&price.out, "price-out", 10, "output USD per 1M")
	flag.Float64Var(&price.read, "price-read", 0.10, "cache read USD per 1M")
	flag.Float64Var(&price.write, "price-write", 2.50, "cache write USD per 1M")
	flag.Parse()
	raw, err := os.ReadFile(*keyfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read keyfile:", err)
		os.Exit(2)
	}
	hc := &http.Client{Transport: &http.Transport{
		DialContext:       (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		DisableKeepAlives: true,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client = anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(strings.TrimSpace(string(raw))),
		option.WithMaxRetries(0),
		option.WithHTTPClient(hc),
	)
	cases := []struct {
		name string
		run  func()
	}{
		{"contract", caseContract},
		{"baseline", caseBaseline},
		{"defer", caseDefer},
		{"unreferenced", caseUnreferenced},
	}
	for _, c := range cases {
		if *caseName == "all" || *caseName == c.name {
			salt = newSalt(c.name)
			fmt.Printf("\n=== %s · %s · salt %s\n", model, c.name, salt)
			c.run()
		}
	}
	fmt.Printf("\ntotal billed by this run: $%.4f\n", spent)
}

func newSalt(name string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error on supported platforms
	return name + "-" + hex.EncodeToString(b)
}

// ---------- wire ----------

type reply struct {
	status string
	body   obj
	err    string
}

func send(body obj) reply {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var out obj
	err := client.Post(ctx, "v1/messages", body, &out, option.WithHeader("anthropic-beta", bindingBeta))
	if err != nil {
		if apiErr, ok := errors.AsType[*anthropic.Error](err); ok {
			return reply{status: fmt.Sprintf("HTTP %d", apiErr.StatusCode), err: string(apiErr.Type()) + ": " + clip(apiErr.Message, 300)}
		}
		return reply{status: "transport error", err: clip(err.Error(), 300)}
	}
	return reply{status: "200", body: out}
}

// show prints one call: usage, cost, the thinking blocks dropped by the binding check, and
// a digest of the tools array, which must not move across the rounds of the defer case.
func show(label string, tools []any, r reply) {
	if r.body == nil {
		fmt.Printf("  %-30s %s %s\n", label, r.status, r.err)
		return
	}
	u, _ := r.body["usage"].(obj)
	in, read, write, out := num(u["input_tokens"]), num(u["cache_read_input_tokens"]), num(u["cache_creation_input_tokens"]), num(u["output_tokens"])
	usd := (in*price.in + read*price.read + write*price.write + out*price.out) / 1e6
	spent += usd
	fmt.Printf("  %-30s 200 stop=%v blocks=%s in=%.0f read=%.0f write=%.0f out=%.0f prompt=%.0f dropped=%d transforms=%s tools=%s cost=$%.4f\n",
		label, r.body["stop_reason"], kinds(r.body), in, read, write, out, in+read+write,
		dropped(r.body), transforms(r.body), digest(tools), usd)
}

func num(v any) float64 { f, _ := v.(float64); return f }

func blocks(resp obj) []obj {
	list, _ := resp["content"].([]any)
	out := make([]obj, 0, len(list))
	for _, b := range list {
		if m, ok := b.(obj); ok {
			out = append(out, m)
		}
	}
	return out
}

func kinds(resp obj) string {
	var names []string
	for _, b := range blocks(resp) {
		names = append(names, fmt.Sprint(b["type"]))
	}
	return "[" + strings.Join(names, ",") + "]"
}

func countThinking(resp obj) int {
	n := 0
	for _, b := range blocks(resp) {
		if b["type"] == "thinking" || b["type"] == "redacted_thinking" {
			n++
		}
	}
	return n
}

func transformList(resp obj) []obj {
	list, _ := resp["input_transformations"].([]any)
	out := make([]obj, 0, len(list))
	for _, e := range list {
		if m, ok := e.(obj); ok {
			out = append(out, m)
		}
	}
	return out
}

func dropped(resp obj) int {
	n := 0
	for _, t := range transformList(resp) {
		if t["type"] == "thinking_dropped" {
			n++
		}
	}
	return n
}

func transforms(resp obj) string {
	if _, ok := resp["input_transformations"]; !ok {
		return "absent"
	}
	var parts []string
	for _, t := range transformList(resp) {
		parts = append(parts, fmt.Sprintf("%v@%v", t["type"], t["path"]))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func digest(tools []any) string {
	b, _ := json.Marshal(tools) // maps marshal with sorted keys: equal values give equal bytes
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---------- catalogue ----------

func object(props obj, required ...string) obj {
	req := make([]any, 0, len(required))
	for _, r := range required {
		req = append(req, r)
	}
	return obj{"type": "object", "properties": props, "required": req}
}

func tool(name, description string, schema obj) obj {
	return obj{"name": name, "description": description, "input_schema": schema}
}

func with(t obj, key string, value any) obj {
	cp := maps.Clone(t)
	cp[key] = value
	return cp
}

func without(t obj, key string) obj {
	cp := with(t, key, nil)
	delete(cp, key)
	return cp
}

var ephemeral = obj{"type": "ephemeral"}

var calcTool = tool("calc", "Evaluate one arithmetic expression and return the number.",
	object(obj{"expression": obj{"type": "string"}}, "expression"))

var searchTool = tool("tool_search", "Load deferred tools by name or keyword. Use 'select:name' when you know the name.",
	object(obj{"query": obj{"type": "string", "description": "'select:Name1,Name2' or a keyword phrase."}}, "query"))

// quoteTool's argument names and the px_basis rule cannot be guessed from its name: a call
// that gets them right read the schema.
var quoteTool = tool("stock_quote",
	"Return the latest quote of one listed instrument. px_basis MUST be \"vwap_5m\" unless the user names another basis.",
	object(obj{
		"instrument_code": obj{"type": "string", "description": "Exchange ticker, upper case."},
		"fx_leg":          obj{"type": "string", "enum": []any{"EUR", "USD", "GBP"}, "description": "Currency to quote in."},
		"px_basis":        obj{"type": "string", "enum": []any{"mid", "last_trade", "vwap_5m"}},
	}, "instrument_code", "fx_leg", "px_basis"))

// padding is a deferred catalogue large enough that its cost would show in round 1 if
// unreferenced deferred tools were billed; half sorts before tool_search and half after, so
// the tools breakpoint sits mid-array as it would in Aura's alphabetical manifest.
func padding() []obj {
	var out []obj
	for i := range 20 {
		prefix := "archive"
		if i%2 == 1 {
			prefix = "warehouse"
		}
		name := fmt.Sprintf("%s_%02d", prefix, i)
		desc := strings.Repeat(fmt.Sprintf("Manages %s records of shard %d: list, filter by date range, export as CSV, and reconcile against the ledger. ", prefix, i), 12)
		out = append(out, tool(name, desc, object(obj{
			"shard_ref":  obj{"type": "string", "description": "Shard reference."},
			"from_date":  obj{"type": "string", "description": "ISO date, inclusive."},
			"until_date": obj{"type": "string", "description": "ISO date, exclusive."},
		}, "shard_ref")))
	}
	return out
}

func byName(tools []obj) []any {
	slices.SortFunc(tools, func(a, b obj) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, t)
	}
	return out
}

// manifest is today's shape: deferred tools are absent until promoted, and the breakpoint
// goes on the last tool (request.go marks tools[len-1]).
func manifest(promoted ...obj) []any {
	list := byName(append([]obj{calcTool, searchTool}, promoted...))
	list[len(list)-1] = with(list[len(list)-1].(obj), "cache_control", ephemeral)
	return list
}

// deferredCatalogue is the candidate shape: every tool from the first request, deferred ones
// flagged, the breakpoint on the last non-deferred tool (a deferred one with cache_control
// is a 400).
func deferredCatalogue() []any {
	var all []obj
	for _, t := range append([]obj{quoteTool}, padding()...) {
		all = append(all, with(t, "defer_loading", true))
	}
	list := byName(append(all, calcTool, searchTool))
	for i := len(list) - 1; i >= 0; i-- {
		if t := list[i].(obj); t["defer_loading"] != true {
			list[i] = with(t, "cache_control", ephemeral)
			break
		}
	}
	return list
}

func rosterNames() string {
	names := []string{quoteTool["name"].(string)}
	for _, t := range padding() {
		names = append(names, t["name"].(string))
	}
	return strings.Join(names, ", ")
}

// ---------- request shapes ----------

var systemText = "You are Aura, a careful assistant. Your manifest does NOT list every tool: the rest are deferred. " +
	"CALL tool_search FIRST for any tool you do not see in your manifest, even one whose name you know; a deferred " +
	"tool called before its schema is loaded is refused. " + strings.Repeat(
	"Report numbers plainly and keep answers short. Never invent a tool's arguments: read its schema. ", 60)

func body(tools []any, messages []any) obj {
	return obj{
		"model":         model,
		"max_tokens":    16000,
		"system":        []any{obj{"type": "text", "text": "Probe " + salt + ". " + systemText, "cache_control": ephemeral}},
		"tools":         tools,
		"thinking":      obj{"type": "adaptive", "display": display, "block_binding": obj{"prefix_mismatch_behavior": "drop_block"}},
		"output_config": obj{"effort": effort},
		"messages":      messages,
	}
}

func user(content ...any) obj     { return obj{"role": "user", "content": content} }
func assistant(content []any) obj { return obj{"role": "assistant", "content": content} }
func text(s string) obj           { return obj{"type": "text", "text": s} }
func reference(name string) obj   { return obj{"type": "tool_reference", "tool_name": name} }

func toolResult(id any, content ...any) obj {
	return obj{"type": "tool_result", "tool_use_id": id, "content": content}
}

// renderSpec is tools.RenderSpec's layout: what Aura's tool_search returns today.
func renderSpec(t obj) string {
	params, _ := json.Marshal(t["input_schema"])
	return fmt.Sprintf("## %s\n%s\n\nParameters:\n%s\n\n", t["name"], t["description"], params)
}

const firstAsk = "Get the current quote for ACME in euros with the stock_quote tool and tell me the price."

func opening() []any {
	return []any{user(text(firstAsk), text("<deferred_tools>\n"+rosterNames()+"\n</deferred_tools>"))}
}

// marked copies messages and puts a breakpoint on the last block of message at, as
// conv.mark does for the stable prefix and the loaded history.
func marked(messages []any, at ...int) []any {
	out := slices.Clone(messages)
	for _, i := range at {
		if i < 0 || i >= len(out) {
			continue
		}
		m := out[i].(obj)
		content := slices.Clone(m["content"].([]any))
		for j := len(content) - 1; j >= 0; j-- {
			if b := content[j].(obj); b["type"] != "thinking" && b["type"] != "redacted_thinking" {
				content[j] = with(b, "cache_control", ephemeral)
				break
			}
		}
		out[i] = obj{"role": m["role"], "content": content}
	}
	return out
}

func uses(resp obj) []obj {
	var out []obj
	for _, b := range blocks(resp) {
		if b["type"] == "tool_use" {
			out = append(out, b)
		}
	}
	return out
}

// answer runs every tool_use of a response. search is what tool_search returns.
func answer(resp obj, search []any, wantCode, wantFx string) []any {
	var out []any
	for _, u := range uses(resp) {
		in, _ := u["input"].(obj)
		fmt.Printf("      call %v(%v)\n", u["name"], in)
		switch u["name"] {
		case "tool_search":
			out = append(out, toolResult(u["id"], search...))
		case "stock_quote":
			fmt.Printf("      schema reached the model: %s\n", quoteVerdict(in, wantCode, wantFx))
			out = append(out, toolResult(u["id"], text(`{"price": 118.42, "as_of": "2026-11-28T10:00:00Z"}`)))
		default:
			out = append(out, toolResult(u["id"], text("error: not available in this probe")))
		}
	}
	return out
}

func quoteVerdict(in obj, wantCode, wantFx string) string {
	ok := in["instrument_code"] == wantCode && in["fx_leg"] == wantFx && in["px_basis"] == "vwap_5m"
	if ok {
		return "yes (instrument_code, fx_leg and px_basis=vwap_5m all as the schema asks)"
	}
	return "NO: arguments do not match the schema"
}

func calls(resp obj, name string) bool {
	return slices.ContainsFunc(uses(resp), func(u obj) bool { return u["name"] == name })
}

// ---------- cases ----------

// caseContract sends the three shapes the docs say are refused. A 400 is not billed; a 200
// would be, and would contradict the docs.
func caseContract() {
	all := deferredCatalogue()
	var onDeferred []any
	for _, t := range all {
		m := without(t.(obj), "cache_control")
		if m["name"] == "stock_quote" {
			m["cache_control"] = ephemeral
		}
		onDeferred = append(onDeferred, m)
	}
	show("cache_control on a deferred tool", onDeferred, send(body(onDeferred, opening())))
	unknown := []any{
		user(text("Find a tool for the weather.")),
		assistant([]any{obj{"type": "tool_use", "id": "toolu_probe_unknown", "name": "tool_search", "input": obj{"query": "weather"}}}),
		user(toolResult("toolu_probe_unknown", reference("no_such_tool"))),
	}
	show("tool_reference to an unknown tool", all, send(body(all, unknown)))
	var everything []any
	for _, t := range all {
		everything = append(everything, with(without(t.(obj), "cache_control"), "defer_loading", true))
	}
	show("every tool deferred", everything, send(body(everything, opening())))
}

// caseBaseline is today's wire: the search result carries the schema as text and the
// promoted tool joins the tools array on the next round.
func caseBaseline() {
	msgs := opening()
	tools := manifest()
	r1 := send(body(tools, marked(msgs, 0)))
	show("round 1 (manifest only)", tools, r1)
	if r1.body == nil || !calls(r1.body, "tool_search") {
		fmt.Println("  round 1 did not call tool_search: stopped")
		return
	}
	fmt.Printf("  round 1 thinking blocks: %d (a drop needs one)\n", countThinking(r1.body))
	msgs = append(msgs, assistant(r1.body["content"].([]any)), user(answer(r1.body, []any{text(renderSpec(quoteTool))}, "ACME", "EUR")...))
	tools = manifest(quoteTool)
	r2 := send(body(tools, marked(msgs, len(msgs)-1)))
	show("round 2 (tools array grew)", tools, r2)
}

func searchReply() []any {
	switch shape {
	case "refs":
		return []any{reference("stock_quote")}
	case "spec":
		return []any{text(renderSpec(quoteTool)), reference("stock_quote")}
	default:
		return []any{text("Loaded 1 tool: stock_quote."), reference("stock_quote")}
	}
}

// caseDefer plays one turn with the deferred catalogue to its answer, then replays it in a
// second turn (verbatim and in Aura's reload shape) to see whether the reference outlives it.
func caseDefer() {
	tools := deferredCatalogue()
	first := digest(tools)
	msgs := opening()
	var final obj
	for round := 1; round <= 3; round++ {
		at := len(msgs) - 1
		r := send(body(tools, marked(msgs, at)))
		show(fmt.Sprintf("round %d (deferred, %s)", round, shape), tools, r)
		if r.body == nil {
			return
		}
		if round == 1 {
			fmt.Printf("  round 1 thinking blocks: %d (a drop needs one)\n", countThinking(r.body))
			if !calls(r.body, "tool_search") {
				fmt.Println("  round 1 did not call tool_search: stopped")
				return
			}
		}
		if r.body["stop_reason"] != "tool_use" {
			final = r.body
			break
		}
		msgs = append(msgs, assistant(r.body["content"].([]any)), user(answer(r.body, searchReply(), "ACME", "EUR")...))
	}
	fmt.Printf("  tools array identical on every round: %v\n", digest(tools) == first)
	if final == nil {
		fmt.Println("  no final answer within 3 rounds: replay skipped")
		return
	}
	history := append(slices.Clone(msgs), assistant(final["content"].([]any)))
	replay("turn 2 verbatim", tools, history)
	replay("turn 2 reload shape", tools, stripThinking(history))
}

// replay asks for a second quote with no reminder to search; a direct stock_quote call with
// the right arguments means the reference in the earlier tool_result still loads the tool.
func replay(label string, tools []any, history []any) {
	loaded := len(history) - 1
	msgs := append(slices.Clone(history), user(text("Now the same for GLOBEX, in pounds.")))
	r := send(body(tools, marked(msgs, loaded, len(msgs)-1)))
	show(label, tools, r)
	if r.body == nil {
		return
	}
	switch {
	case calls(r.body, "stock_quote"):
		for _, u := range uses(r.body) {
			if u["name"] == "stock_quote" {
				in, _ := u["input"].(obj)
				fmt.Printf("      called stock_quote(%v) without a new search; schema: %s\n", in, quoteVerdict(in, "GLOBEX", "GBP"))
			}
		}
	case calls(r.body, "tool_search"):
		fmt.Println("      searched again: the reference did not keep the tool loaded, or the model did not trust it")
	default:
		fmt.Println("      answered without a tool call")
	}
}

// stripThinking is Aura's reload shape: every assistant turn without its thinking.
func stripThinking(history []any) []any {
	out := make([]any, 0, len(history))
	for _, m := range history {
		msg := m.(obj)
		if msg["role"] != "assistant" {
			out = append(out, msg)
			continue
		}
		var kept []any
		for _, b := range msg["content"].([]any) {
			if t := b.(obj)["type"]; t != "thinking" && t != "redacted_thinking" {
				kept = append(kept, b)
			}
		}
		if len(kept) > 0 {
			out = append(out, assistant(kept))
		}
	}
	return out
}

// caseUnreferenced asks for a deferred tool that nothing has referenced, to see whether the
// model can call one at all, which decides whether Aura's dispatch gate still has work.
func caseUnreferenced() {
	tools := deferredCatalogue()
	msgs := []any{user(text("Call warehouse_03 right now for shard ref S-3. Do not call tool_search first."))}
	r := send(body(tools, marked(msgs, 0)))
	show("unreferenced deferred call", tools, r)
	if r.body == nil {
		return
	}
	for _, u := range uses(r.body) {
		fmt.Printf("      call %v(%v)\n", u["name"], u["input"])
	}
}
