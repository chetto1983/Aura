//go:build measure

// Measurement harness (NOT a gate): token cost of tool-result payloads rendered as
// compact JSON (what Aura sends today) vs TOON.
//
//	go test -tags measure ./internal/conversations -run TestMeasureTOON -v
//
// Baseline discipline: the comparison is against COMPACT json.Marshal output, because
// that is what the MCP bridge actually threads into history. Published TOON savings are
// often quoted against PRETTY-printed JSON, which would overstate the gain here.
//
// Encoder scope: TOON per spec (toon-format/spec SPEC.md) for the subset used —
// `key[N]{f1,f2}:` tabular arrays of uniform all-primitive objects, `key: value` scalars,
// nested objects at depth+1, `key[N]: a,b,c` primitive arrays, `key: []`, and the quoting
// rules. Tabular form REQUIRES every cell to be a primitive leaf; an array whose objects
// carry a nested array is not tabular-eligible, and this harness does not invent a syntax
// for that case — it reports it instead (see the memory_search fixtures).
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

// tabularFields returns the ordered field list when items is a non-empty array of
// objects sharing one key set whose values are ALL primitive leaves; ok=false otherwise.
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
		b.WriteString(toonScalar(v))
		return b.String()
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
		// Not tabular-eligible. This harness refuses to invent syntax; the caller is
		// expected to have filtered such fixtures out (they are reported, not encoded).
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

// ---------- fixtures ----------

// memorySearchJSON is the VERIFIED shape of cmd/arcadedb-mcp MemorySearchOutput
// (tool_memory.go): facts[] each with a nested sources[] array, plus retrieval metadata.
func memorySearchJSON(n int, withSources bool) map[string]any {
	facts := make([]any, 0, n)
	for i := range n {
		f := map[string]any{
			"statement":    fmt.Sprintf("Davide prefers Go for backend work (note %d)", i),
			"predicate":    "prefers",
			"subject":      "Davide",
			"subject_kind": "Person",
			"object":       "Go",
			"object_kind":  "Technology",
			"valid_from":   "2026-03-14T09:12:00Z",
		}
		if withSources {
			f["sources"] = []any{
				map[string]any{"run_id": fmt.Sprintf("run-%d", i), "memory_ids": []any{"m1", "m2"}},
			}
		}
		facts = append(facts, f)
	}
	return map[string]any{
		"facts":     facts,
		"retrieval": map[string]any{"path": "hybrid", "abstained": false},
	}
}

// searchResultsJSON is the flat uniform shape a document/web search result list takes:
// the archetypal TOON sweet spot (every cell a primitive leaf).
func searchResultsJSON(n int) map[string]any {
	rows := make([]any, 0, n)
	for i := range n {
		rows = append(rows, map[string]any{
			"document_id": fmt.Sprintf("doc-%04d", i),
			"title":       fmt.Sprintf("Quarterly report section %d", i),
			"score":       0.87 - float64(i)/100,
			"snippet":     "Revenue grew across the northern region while costs stayed flat.",
		})
	}
	return map[string]any{"results": rows, "query": "revenue north", "limit": n}
}

// digestJSON is the verified memory_digest shape (serve_memory_context.go): one large
// text blob plus counters — the archetypal WORST case for TOON.
func digestJSON() map[string]any {
	return map[string]any{
		"text":     strings.Repeat("Davide located_in Caraglio. Davide prefers Go. ", 40),
		"entities": 12.0,
		"facts":    37.0,
		"covered":  true,
	}
}

func TestMeasureTOON(t *testing.T) {
	enc := mustEncoderRaw(t)

	roundtrip := func(v map[string]any) map[string]any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}

	cases := []struct {
		name    string
		payload map[string]any
		note    string
	}{
		{"search results ×10 (flat uniform — TOON sweet spot)", searchResultsJSON(10), ""},
		{"search results ×50 (flat uniform, larger)", searchResultsJSON(50), ""},
		{"memory_search ×5 WITHOUT sources (flattened)", memorySearchJSON(5, false), ""},
		{"memory_search ×5 AS-IS (nested sources[])", memorySearchJSON(5, true),
			"nested array per row ⇒ NOT tabular-eligible; TOON's tabular gain does not apply"},
		{"memory_digest (one text blob — worst case)", digestJSON(), ""},
	}

	for _, tc := range cases {
		payload := roundtrip(tc.payload)
		compact, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		pretty, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			t.Fatalf("marshal indent: %v", err)
		}
		toon := toonEncode(payload)

		cJSON := countTokens(enc, string(compact))
		pJSON := countTokens(enc, string(pretty))
		cTOON := countTokens(enc, toon)

		t.Logf("\n--- %s ---", tc.name)
		if tc.note != "" {
			t.Logf("  NOTE: %s", tc.note)
		}
		t.Logf("  compact JSON (Aura's real baseline): %d tokens", cJSON)
		t.Logf("  pretty  JSON (the flattering baseline): %d tokens", pJSON)
		if strings.Contains(toon, "<NOT-TABULAR>") {
			t.Logf("  TOON: not encodable here — %s", tc.note)
			continue
		}
		t.Logf("  TOON: %d tokens", cTOON)
		t.Logf("  → vs compact JSON: %+.1f%%   vs pretty JSON: %+.1f%%",
			delta(cTOON, cJSON), delta(cTOON, pJSON))
	}
}

// delta returns the percentage change from base to got (negative = fewer tokens).
func delta(got, base int) float64 {
	if base == 0 {
		return 0
	}
	return (float64(got) - float64(base)) / float64(base) * 100
}
