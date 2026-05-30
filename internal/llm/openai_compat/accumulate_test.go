package openai_compat

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// TestAccumulate_MergeByIndex asserts fragments split across ≥3 deltas merge into
// exactly one ToolCall whose concatenated arguments parse as valid JSON (Req#2).
func TestAccumulate_MergeByIndex(t *testing.T) {
	acc := newAccumulator()
	// First fragment: id + name, empty args.
	acc.add(toolCallDelta{Index: 0, ID: "call_1", Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "get_weather"}})
	// Arguments split across 3 subsequent fragments.
	for _, frag := range []string{`{"city":`, `"Roma","unit"`, `:"celsius"}`} {
		acc.add(toolCallDelta{Index: 0, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: frag}})
	}

	calls := acc.finalize()
	if len(calls) != 1 {
		t.Fatalf("finalize() = %d calls, want exactly 1", len(calls))
	}
	tc := calls[0]
	if tc.ID != "call_1" || tc.Function.Name != "get_weather" || tc.Type != "function" {
		t.Errorf("call metadata = %+v, want id=call_1 name=get_weather type=function", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("concatenated arguments are not valid JSON: %v (%q)", err, tc.Function.Arguments)
	}
	if args["city"] != "Roma" {
		t.Errorf("args[city] = %v, want Roma", args["city"])
	}
}

// TestAccumulate_MultipleIndices asserts two parallel tool calls (D-14) finalize
// in index order, each well-formed.
func TestAccumulate_MultipleIndices(t *testing.T) {
	acc := newAccumulator()
	// Deliberately add index 1 before index 0 to prove index-ordering, not arrival.
	acc.add(toolCallDelta{Index: 1, ID: "b", Function: fn("two", `{"y":2}`)})
	acc.add(toolCallDelta{Index: 0, ID: "a", Function: fn("one", `{"x":1}`)})

	calls := acc.finalize()
	if len(calls) != 2 {
		t.Fatalf("finalize() = %d calls, want 2", len(calls))
	}
	if calls[0].ID != "a" || calls[1].ID != "b" {
		t.Errorf("order = [%s,%s], want [a,b] (by index)", calls[0].ID, calls[1].ID)
	}
}

// TestAccumulate_PartitionInvariant is the property test (Req#2): for ANY
// partition of a well-formed tool call's argument bytes into ≥1 fragments,
// accumulation yields the same single ToolCall whose arguments parse as JSON.
func TestAccumulate_PartitionInvariant(t *testing.T) {
	const fullArgs = `{"city":"Roma","unit":"celsius","nested":{"a":1,"b":[1,2,3]}}`
	rapid.Check(t, func(rt *rapid.T) {
		// Choose cut points to partition fullArgs into 1..N fragments.
		cuts := rapid.SliceOfNDistinct(
			rapid.IntRange(1, len(fullArgs)-1), 0, len(fullArgs)-1,
			func(i int) int { return i },
		).Draw(rt, "cuts")
		// Sort cut points ascending.
		for i := 1; i < len(cuts); i++ {
			for j := i; j > 0 && cuts[j-1] > cuts[j]; j-- {
				cuts[j-1], cuts[j] = cuts[j], cuts[j-1]
			}
		}
		frags := splitAt(fullArgs, cuts)

		acc := newAccumulator()
		acc.add(toolCallDelta{Index: 0, ID: "call_x", Function: fn("f", "")})
		for _, frag := range frags {
			acc.add(toolCallDelta{Index: 0, Function: fn("", frag)})
		}
		calls := acc.finalize()
		if len(calls) != 1 {
			rt.Fatalf("partition %v -> %d calls, want 1", cuts, len(calls))
		}
		if calls[0].Function.Arguments != fullArgs {
			rt.Fatalf("reassembled %q != %q", calls[0].Function.Arguments, fullArgs)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &m); err != nil {
			rt.Fatalf("reassembled args not valid JSON: %v", err)
		}
	})
}

func fn(name, args string) struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
} {
	return struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: name, Arguments: args}
}

// splitAt partitions s at the given ascending cut indices.
func splitAt(s string, cuts []int) []string {
	var out []string
	prev := 0
	for _, c := range cuts {
		if c <= prev || c >= len(s) {
			continue
		}
		out = append(out, s[prev:c])
		prev = c
	}
	out = append(out, s[prev:])
	return out
}
