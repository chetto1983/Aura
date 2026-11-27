// Probe of the Claude Messages wire Aura will send (plan 2026-11-27-anthropic-provider, Task 1).
// Transport and headers come from the official SDK; bodies are raw JSON so each case sends exactly
// the shape under test. The key is read from -keyfile and never printed.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

type obj = map[string]any

var (
	client  anthropic.Client
	model   string
	pdfPath string
	sentHdr http.Header
)

const bindingBeta = "thinking-binding-controls-2026-08-01"

func main() {
	keyfile := flag.String("keyfile", "", "file holding the API key")
	flag.StringVar(&model, "model", "claude-sonnet-5-5", "model id")
	caseName := flag.String("case", "all", "case name or all")
	flag.StringVar(&pdfPath, "pdf", "", "small PDF for the pdf case")
	flag.Parse()
	raw, err := os.ReadFile(*keyfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read keyfile:", err)
		os.Exit(2)
	}
	key := strings.TrimSpace(string(raw))
	hc := &http.Client{Transport: &http.Transport{
		DialContext:       (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		DisableKeepAlives: true,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client = anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(key),
		option.WithMaxRetries(0),
		option.WithHTTPClient(hc),
		option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			sentHdr = r.Header.Clone()
			return next(r)
		}),
	)
	cases := []struct {
		name string
		run  func()
	}{
		{"headers", caseHeaders},
		{"sampling", caseSampling},
		{"tool-choice", caseToolChoice},
		{"loop", func() { caseLoop("summarized") }},
		{"loop-omitted", func() { caseLoop("omitted") }},
		{"steady", caseSteady},
		{"foreign-history", caseForeign},
		{"pdf-in-tool-result", casePDF},
	}
	for _, c := range cases {
		if *caseName == "all" || *caseName == c.name {
			fmt.Printf("\n=== %s · %s\n", model, c.name)
			c.run()
		}
	}
}

// ---------- wire helpers ----------

type result struct {
	status  string
	resp    obj
	errType string
}

func send(body obj, beta bool) result {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var out obj
	opts := []option.RequestOption{}
	if beta {
		opts = append(opts, option.WithHeader("anthropic-beta", bindingBeta))
	}
	err := client.Post(ctx, "v1/messages", body, &out, opts...)
	if err != nil {
		if apiErr, ok := errors.AsType[*anthropic.Error](err); ok {
			return result{status: fmt.Sprintf("HTTP %d", apiErr.StatusCode), errType: string(apiErr.Type()) + ": " + trunc(apiErr.Message, 300)}
		}
		return result{status: "transport error", errType: trunc(err.Error(), 300)}
	}
	return result{status: "200", resp: out}
}

func report(label string, r result) {
	if r.resp == nil {
		fmt.Printf("  %-34s %s %s\n", label, r.status, r.errType)
		return
	}
	u, _ := r.resp["usage"].(obj)
	details, _ := u["output_tokens_details"].(obj)
	fmt.Printf("  %-34s 200 stop=%v blocks=%s in=%v read=%v write=%v out=%v think=%v transforms=%s\n",
		label, r.resp["stop_reason"], blockTypes(r.resp), u["input_tokens"], u["cache_read_input_tokens"],
		u["cache_creation_input_tokens"], u["output_tokens"], details["thinking_tokens"], transforms(r.resp))
}

func blockTypes(resp obj) string {
	var parts []string
	for _, b := range content(resp) {
		t := b["type"].(string)
		if t == "thinking" {
			if s, _ := b["thinking"].(string); s == "" {
				t = "thinking(empty)"
			}
		}
		parts = append(parts, t)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func transforms(resp obj) string {
	v, ok := resp["input_transformations"]
	if !ok {
		return "absent"
	}
	list, _ := v.([]any)
	if len(list) == 0 {
		return "[]"
	}
	var parts []string
	for _, e := range list {
		m := e.(obj)
		parts = append(parts, fmt.Sprintf("%v@%v", m["type"], m["path"]))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func content(resp obj) []obj {
	list, _ := resp["content"].([]any)
	out := make([]obj, 0, len(list))
	for _, b := range list {
		out = append(out, b.(obj))
	}
	return out
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---------- request shapes ----------

// A system prompt long enough to be cacheable (min 512 tokens on Claude 5), as Aura's is.
var systemText = "You are Aura, a careful assistant. " + strings.Repeat(
	"Always use the calc tool for arithmetic, one call per message, and never compute in your head. "+
		"Report the final number plainly. Keep answers short. ", 60)

var tools = []any{
	obj{"name": "calc", "description": "Evaluate one arithmetic expression and return the number.",
		"input_schema": obj{"type": "object", "properties": obj{"expression": obj{"type": "string"}}, "required": []any{"expression"}}},
	obj{"name": "lookup", "description": "Look up a stored value by key.",
		"input_schema":  obj{"type": "object", "properties": obj{"key": obj{"type": "string"}}, "required": []any{"key"}},
		"cache_control": obj{"type": "ephemeral"}},
}

func baseBody(display, binding string, messages []any) obj {
	thinking := obj{"type": "adaptive", "display": display}
	if binding != "" {
		thinking["block_binding"] = obj{"prefix_mismatch_behavior": binding}
	}
	return obj{
		"model":         model,
		"max_tokens":    16000,
		"system":        []any{obj{"type": "text", "text": systemText, "cache_control": obj{"type": "ephemeral"}}},
		"tools":         tools,
		"thinking":      thinking,
		"output_config": obj{"effort": "high"},
		"messages":      messages,
	}
}

func hint(round int) obj {
	return obj{"type": "text", "text": fmt.Sprintf("<current_time>2026-11-27T10:%02d:00Z</current_time> <budget>round %d of 12</budget>", round, round)}
}

const task = "Using the calc tool one step per message: (a) compute 1234*5678; (b) subtract 99991 from that; (c) divide that by 7. Before each call, think about whether the previous result is plausible. Then give the final number, rounded down."

// ---------- cases ----------

func caseHeaders() {
	r := send(obj{"model": model, "max_tokens": 64, "messages": []any{obj{"role": "user", "content": "Say ok."}}}, false)
	report("one call through the SDK", r)
	auth := sentHdr.Get("Authorization")
	fmt.Printf("  sent: authorization=%q anthropic-version=%q x-api-key-present=%v\n",
		strings.SplitN(auth, " ", 2)[0]+" <redacted>", sentHdr.Get("Anthropic-Version"), sentHdr.Get("X-Api-Key") != "")
}

func caseSampling() {
	b := obj{"model": model, "max_tokens": 64, "temperature": 1.0, "messages": []any{obj{"role": "user", "content": "Say ok."}}}
	report("temperature 1.0", send(b, false))
}

func caseToolChoice() {
	msgs := []any{obj{"role": "user", "content": "Say ok."}}
	b := baseBody("summarized", "", msgs)
	b["tool_choice"] = obj{"type": "none"}
	b["max_tokens"] = 2000
	report("tool_choice none, tools present", send(b, false))
	b2 := obj{"model": model, "max_tokens": 2000, "messages": msgs}
	report("no tools, no tool_choice", send(b2, false))
	b3 := obj{"model": model, "max_tokens": 2000, "messages": msgs, "tool_choice": obj{"type": "none"}}
	report("tool_choice none, no tools", send(b3, false))
}

// toolAnswer computes the tool result the way a real calc would.
func toolAnswer(b obj) string {
	in, _ := b["input"].(obj)
	expr, _ := in["expression"].(string)
	expr = strings.NewReplacer("×", "*", "÷", "/", ",", "").Replace(expr)
	for _, fn := range []string{"floor", "round", "int", "math.floor", "Math.floor"} {
		expr = strings.ReplaceAll(expr, fn+"(", "(")
	}
	node, err := parser.ParseExpr(expr)
	if err != nil {
		return "error: cannot parse " + expr
	}
	v, err := eval(node)
	if err != nil {
		return "error: " + err.Error()
	}
	if v == float64(int64(v)) {
		return fmt.Sprint(int64(v))
	}
	return strconv.FormatFloat(v, 'f', 4, 64)
}

func eval(n ast.Expr) (float64, error) {
	switch e := n.(type) {
	case *ast.BasicLit:
		return strconv.ParseFloat(e.Value, 64)
	case *ast.ParenExpr:
		return eval(e.X)
	case *ast.UnaryExpr:
		x, err := eval(e.X)
		if e.Op == token.SUB {
			x = -x
		}
		return x, err
	case *ast.BinaryExpr:
		x, err := eval(e.X)
		if err != nil {
			return 0, err
		}
		y, err := eval(e.Y)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.ADD:
			return x + y, nil
		case token.SUB:
			return x - y, nil
		case token.MUL:
			return x * y, nil
		case token.QUO:
			if y == 0 {
				return 0, errors.New("division by zero")
			}
			return x / y, nil
		}
	}
	return 0, errors.New("unsupported expression")
}

const (
	hintsNone = iota
	hintsNewest
	hintsAll
)

type round struct {
	assistant []any // verbatim content blocks
	user      []any // tool_results that answered it (without hint)
}

// stripThinking rebuilds an assistant turn the way Aura's history reload does: tool_use only.
func stripThinking(blocks []any) []any {
	var out []any
	for _, b := range blocks {
		if m := b.(obj); m["type"] == "tool_use" {
			out = append(out, m)
		}
	}
	return out
}

// build assembles the conversation. keepHints: every round's hint stays in place;
// otherwise only the newest hint is sent (Aura's v1 shape). rebuilt: assistant turns
// without their thinking (Aura's reload shape). tail: mark the last stable block.
func build(rounds []round, hints int, rebuilt, tail bool) []any {
	keepHints := hints == hintsAll
	first := []any{obj{"type": "text", "text": task}}
	if keepHints || (hints == hintsNewest && len(rounds) == 0) {
		first = append(first, hint(0))
	}
	msgs := []any{obj{"role": "user", "content": first}}
	for i, r := range rounds {
		a := r.assistant
		if rebuilt {
			a = stripThinking(a)
		}
		msgs = append(msgs, obj{"role": "assistant", "content": a})
		u := append([]any{}, r.user...)
		if keepHints || (hints == hintsNewest && i == len(rounds)-1) {
			u = append(u, hint(i+1))
		}
		msgs = append(msgs, obj{"role": "user", "content": u})
	}
	if tail && len(rounds) > 0 {
		last := msgs[len(msgs)-1].(obj)["content"].([]any)
		// the last tool_result: the stable block before the hint
		for i := len(last) - 1; i >= 0; i-- {
			if b := last[i].(obj); b["type"] == "tool_result" {
				cp := obj{}
				maps.Copy(cp, b)
				cp["cache_control"] = obj{"type": "ephemeral"}
				last[i] = cp
				break
			}
		}
	}
	return msgs
}

func answer(resp obj) []any {
	var out []any
	for _, b := range content(resp) {
		if b["type"] == "tool_use" {
			ans := toolAnswer(b)
			fmt.Printf("      tool %v(%v) -> %s\n", b["name"], b["input"], ans)
			out = append(out, obj{"type": "tool_result", "tool_use_id": b["id"], "content": ans})
		}
	}
	return out
}

func caseLoop(display string) {
	var rounds []round
	var final obj
	for i := range 6 {
		r := send(baseBody(display, "drop_block", build(rounds, hintsAll, false, true)), true)
		report(fmt.Sprintf("kept-hint round %d", i+1), r)
		if r.resp == nil {
			return
		}
		if r.resp["stop_reason"] != "tool_use" {
			final = r.resp
			break
		}
		rounds = append(rounds, round{assistant: anySlice(r.resp["content"]), user: answer(r.resp)})
	}
	if len(rounds) < 2 {
		fmt.Println("  fewer than two tool rounds: branches skipped")
		return
	}
	// Branches from the full trajectory, each one request.
	at := rounds[:]
	report("branch rotated-hint binding=error", send(baseBody(display, "error", build(at, hintsNewest, false, false)), true))
	report("branch rotated-hint binding=drop_block", send(baseBody(display, "drop_block", build(at, hintsNewest, false, false)), true))
	report("branch rotated-hint no beta (GA)", send(gaBody(baseBody(display, "", build(at, hintsNewest, false, false))), false))
	report("branch reload-shape (no blocks, no old hints)", send(baseBody(display, "drop_block", build(at, hintsNewest, true, false)), true))
	rebuiltLast := append([]round{}, at...)
	rebuiltLast[len(rebuiltLast)-1] = round{assistant: stripThinking(at[len(at)-1].assistant), user: at[len(at)-1].user}
	report("branch last turn rebuilt from calls", send(baseBody(display, "drop_block", build(rebuiltLast, hintsAll, false, false)), true))
	if final == nil || display != "summarized" {
		return
	}
	// Cross-turn cache: a second user turn after the loop.
	next := func(rebuilt bool) []any {
		mode := hintsAll
		if rebuilt {
			mode = hintsNone
		}
		msgs := build(rounds, mode, rebuilt, false)
		msgs = append(msgs, obj{"role": "assistant", "content": textOnly(final)})
		msgs = append(msgs, obj{"role": "user", "content": []any{
			obj{"type": "text", "text": "Now add 1 to that with the calc tool.", "cache_control": obj{"type": "ephemeral"}}}})
		return msgs
	}
	report("turn 2 reload shape", send(baseBody(display, "drop_block", next(true)), true))
	report("turn 2 verbatim shape", send(baseBody(display, "drop_block", next(false)), true))
}

// caseSteady measures caching when every earlier turn is sent in the reload shape (no thinking,
// no hints), with a breakpoint at the end of the loaded history and one on the stable tail.
func caseSteady() {
	ephemeral := obj{"type": "ephemeral"}
	marked := func(b obj) obj {
		cp := obj{}
		maps.Copy(cp, b)
		cp["cache_control"] = ephemeral
		return cp
	}
	// runTurn plays one user turn to its final answer. loaded is the reload-shape history;
	// its last block carries the loaded-history breakpoint.
	runTurn := func(label string, loaded []any, question string) (stripped []any) {
		msgs := append([]any{}, loaded...)
		if len(msgs) > 0 {
			last := msgs[len(msgs)-1].(obj)
			blocks := append([]any{}, last["content"].([]any)...)
			blocks[len(blocks)-1] = marked(blocks[len(blocks)-1].(obj))
			msgs[len(msgs)-1] = obj{"role": last["role"], "content": blocks}
		}
		turn := []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": question}}}}
		for i := range 6 {
			req := append(append([]any{}, msgs...), turn...)
			// tail: the last stable block of this turn, then the hint
			tail := req[len(req)-1].(obj)
			tb := append([]any{}, tail["content"].([]any)...)
			tb[len(tb)-1] = marked(tb[len(tb)-1].(obj))
			tb = append(tb, hint(i))
			req[len(req)-1] = obj{"role": "user", "content": tb}
			r := send(baseBody("summarized", "drop_block", req), true)
			report(fmt.Sprintf("%s round %d", label, i+1), r)
			if r.resp == nil {
				return nil
			}
			if r.resp["stop_reason"] != "tool_use" {
				stripped = append(stripped, turn...)
				stripped = append(stripped, obj{"role": "assistant", "content": textOnly(r.resp)})
				return stripped
			}
			// in-run: verbatim blocks and the hint kept
			turn[len(turn)-1] = obj{"role": "user", "content": append(append([]any{}, turn[len(turn)-1].(obj)["content"].([]any)...), hint(i))}
			turn = append(turn, obj{"role": "assistant", "content": anySlice(r.resp["content"])}, obj{"role": "user", "content": answer(r.resp)})
		}
		return nil
	}
	reload := func(turnMsgs []any) []any {
		var out []any
		for _, m := range turnMsgs {
			mm := m.(obj)
			var blocks []any
			for _, b := range mm["content"].([]any) {
				bb := b.(obj)
				if bb["type"] == "thinking" || bb["type"] == "redacted_thinking" {
					continue
				}
				if bb["type"] == "text" && strings.HasPrefix(fmt.Sprint(bb["text"]), "<current_time>") {
					continue
				}
				blocks = append(blocks, bb)
			}
			if len(blocks) > 0 {
				out = append(out, obj{"role": mm["role"], "content": blocks})
			}
		}
		return out
	}
	t1 := runTurn("turn 1", nil, task)
	if t1 == nil {
		return
	}
	h := reload(t1)
	t2 := runTurn("turn 2", h, "Now multiply that final number by 3 with the calc tool, then subtract 17 with the calc tool.")
	if t2 == nil {
		return
	}
	h = append(h, reload(t2)...)
	runTurn("turn 3", h, "Now add 1000 to that with the calc tool.")
}

func gaBody(b obj) obj {
	if t, ok := b["thinking"].(obj); ok {
		delete(t, "block_binding")
	}
	return b
}

func textOnly(resp obj) []any {
	var out []any
	for _, b := range content(resp) {
		if b["type"] == "text" {
			if s, _ := b["text"].(string); strings.TrimSpace(s) != "" {
				out = append(out, obj{"type": "text", "text": s})
			}
		}
	}
	if len(out) == 0 {
		out = []any{obj{"type": "text", "text": "(done)"}}
	}
	return out
}

func anySlice(v any) []any { s, _ := v.([]any); return s }

func caseForeign() {
	msgs := []any{
		obj{"role": "user", "content": "What is 2+2? Use calc."},
		obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "functions_calc_0", "name": "calc", "input": obj{"expression": "2+2"}}}},
		obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "functions_calc_0", "content": "4"}}},
		obj{"role": "assistant", "content": []any{obj{"type": "text", "text": "4"}}},
		obj{"role": "user", "content": "And 3+3? Answer without tools."},
	}
	report("foreign ids, no thinking, sanitized", send(baseBody("summarized", "drop_block", msgs), true))
	raw := []any{
		msgs[0],
		obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "functions.calc:0", "name": "calc", "input": obj{"expression": "2+2"}}}},
		obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "functions.calc:0", "content": "4"}}},
		msgs[3], msgs[4],
	}
	report("foreign ids unsanitized (functions.calc:0)", send(baseBody("summarized", "drop_block", raw), true))
}

func casePDF() {
	if pdfPath == "" {
		fmt.Println("  no -pdf given")
		return
	}
	data, err := os.ReadFile(pdfPath) //nolint:gosec // G304: operator-supplied fixture path for a local probe
	if err != nil {
		fmt.Println("  read pdf:", err)
		return
	}
	msgs := []any{
		obj{"role": "user", "content": "Look up key 'sheet' and tell me the product name in it."},
		obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "toolu_pdf1", "name": "lookup", "input": obj{"key": "sheet"}}}},
		obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "toolu_pdf1", "content": []any{
			obj{"type": "document", "source": obj{"type": "base64", "media_type": "application/pdf", "data": base64.StdEncoding.EncodeToString(data)}},
		}}}},
	}
	r := send(baseBody("summarized", "drop_block", msgs), true)
	report("PDF inside tool_result", r)
	if r.resp != nil {
		for _, b := range content(r.resp) {
			if b["type"] == "text" {
				fmt.Printf("  answer: %s\n", trunc(fmt.Sprint(b["text"]), 160))
			}
		}
	}
}
