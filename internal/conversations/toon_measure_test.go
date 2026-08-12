//go:build measure

// Measurement harness (NOT a gate): token cost of EVERY model-visible tool result in
// Aura, rendered as compact JSON (what Aura sends today) vs TOON.
//
//	go test -tags measure ./internal/conversations -run TestMeasureTOON -v
//
// Baseline discipline: the comparison is against COMPACT json.Marshal output, because
// that is what actually reaches the model. Published TOON savings are usually quoted
// against PRETTY-printed JSON, which overstates the gain here.
//
// Inventory (grep of every NewResult call in internal/agent/tools + internal/swarm):
// the MAJORITY of Aura's tools return RENDERED TEXT, not JSON — patch, read_file,
// search_files_*, shell_exec, shell_poll/kill, skill_*, tool_search, todo_write
// (renderTodos), ask_user, current_time, text_response, write_file, send_file, xlsx,
// web_fetch, web_search. TOON cannot apply to those at all: there is no JSON to re-encode.
// Only these emit JSON as the model-visible content, and they are the fixtures below:
//   - document_search  -> documents.RetrievalResponse   (retrieval.go:64-80)
//   - document_open    -> flat map[string]any            (document_open.go:143-153)
//   - swarm_spawn      -> []swarm.ChildReport            (report.go:32, swarm.go:85)
//   - MCP-bridged      -> whatever the server emits      (memory_* via mcptools/bridge.go)
//
// Encoder scope: TOON per toon-format/spec SPEC.md for the subset used — `key[N]{f}:`
// tabular arrays of uniform all-primitive objects, `key: value` scalars, nested objects
// at depth+1, `key[N]: a,b,c` primitive arrays, `key: []`, and the quoting rules. Tabular
// form REQUIRES every cell to be a primitive leaf; this harness refuses to invent syntax
// for non-eligible arrays and reports them instead.
package conversations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------- TOON encoder (spec subset) ----------

const toonDelim = ","

func toonQuote(s string) string {
	needs := s == "" ||
		strings.TrimSpace(s) != s ||
		s == "true" || s == "false" || s == "null" ||
		strings.ContainsAny(s, ":\"\\[]{}") ||
		strings.Contains(s, toonDelim) ||
		strings.HasPrefix(s, "-") || strings.HasPrefix(s, "#")
	if !needs {
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			needs = true // numeric-like tokens must be quoted
		}
	}
	if !needs {
		return s
	}
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return "\"" + r.Replace(s) + "\""
}

func toonScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return toonQuote(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return toonQuote(fmt.Sprint(t))
	}
}

func isPrimitive(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

func tabularFields(items []any) ([]string, bool) {
	if len(items) == 0 {
		return nil, false
	}
	var fields []string
	for i, item := range items {
		obj, isObj := item.(map[string]any)
		if !isObj {
			return nil, false
		}
		keys := make([]string, 0, len(obj))
		for k, v := range obj {
			if !isPrimitive(v) {
				return nil, false
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if i == 0 {
			fields = keys
			continue
		}
		if strings.Join(keys, "\x00") != strings.Join(fields, "\x00") {
			return nil, false
		}
	}
	return fields, true
}

func toonEncode(v any) string {
	var b strings.Builder
	obj, ok := v.(map[string]any)
	if !ok {
		if items, isArr := v.([]any); isArr {
			toonArray(&b, "", items, 0)
			return strings.TrimRight(b.String(), "\n")
		}
		return toonScalar(v)
	}
	toonObject(&b, obj, 0)
	return strings.TrimRight(b.String(), "\n")
}

func toonObject(b *strings.Builder, obj map[string]any, depth int) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pad := strings.Repeat("  ", depth)
	for _, k := range keys {
		switch val := obj[k].(type) {
		case map[string]any:
			fmt.Fprintf(b, "%s%s:\n", pad, k)
			toonObject(b, val, depth+1)
		case []any:
			toonArray(b, k, val, depth)
		default:
			fmt.Fprintf(b, "%s%s: %s\n", pad, k, toonScalar(val))
		}
	}
}

func toonArray(b *strings.Builder, key string, items []any, depth int) {
	pad := strings.Repeat("  ", depth)
	if len(items) == 0 {
		fmt.Fprintf(b, "%s%s: []\n", pad, key)
		return
	}
	allPrim := true
	for _, item := range items {
		if !isPrimitive(item) {
			allPrim = false
			break
		}
	}
	if allPrim {
		cells := make([]string, 0, len(items))
		for _, item := range items {
			cells = append(cells, toonScalar(item))
		}
		fmt.Fprintf(b, "%s%s[%d]: %s\n", pad, key, len(items), strings.Join(cells, toonDelim))
		return
	}
	fields, ok := tabularFields(items)
	if !ok {
		fmt.Fprintf(b, "%s%s: <NOT-TABULAR>\n", pad, key)
		return
	}
	fmt.Fprintf(b, "%s%s[%d]{%s}:\n", pad, key, len(items), strings.Join(fields, toonDelim))
	rowPad := strings.Repeat("  ", depth+1)
	for _, item := range items {
		obj := item.(map[string]any)
		cells := make([]string, 0, len(fields))
		for _, f := range fields {
			cells = append(cells, toonScalar(obj[f]))
		}
		fmt.Fprintf(b, "%s%s\n", rowPad, strings.Join(cells, toonDelim))
	}
}

// ---------- fixtures: the REAL model-visible JSON shapes ----------

// documentSearchResult mirrors documents.RetrievalResponse (retrieval.go:64-80):
// scalars + a documents[] array whose fields are all primitive => tabular-eligible.
func documentSearchResult(n int) map[string]any {
	docs := make([]any, 0, n)
	for i := range n {
		docs = append(docs, map[string]any{
			"document_id": fmt.Sprintf("doc-%04d", i),
			"title":       fmt.Sprintf("Relazione trimestrale sezione %d", i),
			"card":        "Foglio con ricavi e costi per regione, 12 colonne, 340 righe.",
			"score":       0.87 - float64(i)/100,
			"source_kind": "s3",
		})
	}
	return map[string]any{
		"query": "ricavi nord", "profile": "standard", "status": "ok", "documents": docs,
	}
}

// documentOpenResult mirrors document_open.go:143-153 — one flat object, no array.
func documentOpenResult() map[string]any {
	return map[string]any{
		"path": "/workspace/documents/relazione.xlsx", "file_name": "relazione.xlsx",
		"mime_type":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"size_bytes": 481232.0, "sha256": strings.Repeat("ab", 32), "document_id": "doc-0001",
	}
}

// swarmReports mirrors swarm.ChildReport (report.go:32) marshaled by marshalReports
// (swarm.go:85). withOptions adds the options[] array that breaks tabular eligibility.
func swarmReports(n int, withOptions bool) []any {
	out := make([]any, 0, n)
	for i := range n {
		r := map[string]any{
			"goal_index": float64(i), "child_id": fmt.Sprintf("child-%d", i),
			"status":  "done",
			"summary": "Ho letto i file richiesti e riassunto i risultati principali del trimestre.",
		}
		if withOptions {
			r["options"] = []any{"riprova", "salta"}
			r["question"] = "Come procedo?"
			r["status"] = "paused"
		}
		out = append(out, r)
	}
	return out
}

// memoryRecall mirrors cmd/arcadedb-mcp MemorySearchOutput (tool_memory.go:105-137),
// reached through the MCP bridge. withSources keeps the nested provenance array.
func memoryRecall(n int, withSources bool) map[string]any {
	facts := make([]any, 0, n)
	for i := range n {
		f := map[string]any{
			"statement": fmt.Sprintf("Davide preferisce Go per il backend (nota %d)", i),
			"predicate": "prefers", "subject": "Davide", "subject_kind": "Person",
			"object": "Go", "object_kind": "Technology", "valid_from": "2026-03-14T09:12:00Z",
		}
		if withSources {
			f["sources"] = []any{map[string]any{
				"run_id": fmt.Sprintf("run-%d", i), "memory_ids": []any{"m1", "m2"},
			}}
		}
		facts = append(facts, f)
	}
	return map[string]any{
		"facts": facts, "retrieval": map[string]any{"path": "hybrid", "abstained": false},
	}
}

// memoryDigest mirrors the digest shape decoded in serve_memory_context.go:40-45.
func memoryDigest() map[string]any {
	return map[string]any{
		"text":     strings.Repeat("Davide located_in Caraglio. Davide prefers Go. ", 40),
		"entities": 12.0, "facts": 37.0, "covered": true,
	}
}

func TestMeasureTOON(t *testing.T) {
	enc := mustEncoderRaw(t)

	roundtrip := func(v any) any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}

	cases := []struct {
		tool, shape string
		payload     any
	}{
		{"document_search", "documents[] ×5, all-primitive fields", documentSearchResult(5)},
		{"document_search", "documents[] ×20, all-primitive fields", documentSearchResult(20)},
		{"document_open", "single flat object, 6 scalars", documentOpenResult()},
		{"swarm_spawn", "ChildReport[] ×4, no options[]", swarmReports(4, false)},
		{"swarm_spawn", "ChildReport[] ×4, WITH options[] (paused)", swarmReports(4, true)},
		{"mcp: memory_recall", "facts[] ×5 WITHOUT sources", memoryRecall(5, false)},
		{"mcp: memory_recall", "facts[] ×5 AS-IS (nested sources[])", memoryRecall(5, true)},
		{"mcp: memory_digest", "one text blob + 3 counters", memoryDigest()},
	}

	t.Log("NOTE: every other Aura tool returns RENDERED TEXT, not JSON — TOON is inapplicable there.")
	for _, tc := range cases {
		payload := roundtrip(tc.payload)
		compact, _ := json.Marshal(payload)
		pretty, _ := json.MarshalIndent(payload, "", "  ")
		toon := toonEncode(payload)

		cJSON := countTokens(enc, string(compact))
		pJSON := countTokens(enc, string(pretty))

		t.Logf("\n--- %s | %s ---", tc.tool, tc.shape)
		if strings.Contains(toon, "<NOT-TABULAR>") {
			t.Logf("  compact JSON: %d tokens", cJSON)
			t.Logf("  TOON: NOT TABULAR-ELIGIBLE (a row carries a nested array) — no tabular gain")
			continue
		}
		cTOON := countTokens(enc, toon)
		t.Logf("  compact JSON: %d   pretty JSON: %d   TOON: %d", cJSON, pJSON, cTOON)
		t.Logf("  → vs compact: %+.1f%%   (vs pretty: %+.1f%%)", delta(cTOON, cJSON), delta(cTOON, pJSON))
	}
}

func delta(got, base int) float64 {
	if base == 0 {
		return 0
	}
	return (float64(got) - float64(base)) / float64(base) * 100
}
