package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/pkoukk/tiktoken-go"
)

type evidence struct {
	Facts     []json.RawMessage `json:"facts"`
	Retrieval json.RawMessage   `json:"retrieval"`
}

type queryCase struct {
	Entity   string   `json:"entity"`
	Question string   `json:"question"`
	Direct   evidence `json:"direct"`
	Wide     evidence `json:"wide"`
	Capped   evidence `json:"capped"`
}

type snapshot struct {
	At    string      `json:"at"`
	Cases []queryCase `json:"cases"`
}

func readSnapshot(path string) snapshot {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var value snapshot
	if err := json.Unmarshal(data, &value); err != nil {
		panic(err)
	}
	return value
}

func fields(raw json.RawMessage) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func directCount(value evidence, entity string) int {
	n := 0
	for _, raw := range value.Facts {
		f := fields(raw)
		if f["subject"] == entity || f["object"] == entity {
			n++
		}
	}
	return n
}

func keys(value evidence) []string {
	out := []string{}
	for _, raw := range value.Facts {
		out = append(out, fmt.Sprint(fields(raw)["fact_key"]))
	}
	slices.Sort(out)
	return out
}

func main() {
	beforePath := flag.String("before", "", "private mounted-MCP baseline snapshot")
	afterPath := flag.String("after", "", "private mounted-MCP corrected snapshot")
	flag.Parse()
	before, after := readSnapshot(*beforePath), readSnapshot(*afterPath)
	if before.At != after.At || len(before.Cases) != len(after.Cases) {
		panic("snapshots must use the same validity instant and case inventory")
	}
	// Reuse Aura's vendored, offline vocabulary and existing encoder library.
	if err := conversations.InitEncoder(); err != nil {
		panic(err)
	}
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	if err != nil {
		panic(err)
	}
	count := func(value evidence) int {
		data, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		return len(enc.EncodeOrdinary(string(data)))
	}
	rows := []map[string]any{}
	cappedBefore, cappedAfter := 0, 0
	for i, old := range before.Cases {
		updated := after.Cases[i]
		if old.Entity != updated.Entity {
			panic("case order changed")
		}
		if !slices.Equal(keys(old.Direct), keys(updated.Direct)) {
			panic("direct fact set changed; cannot attribute a before/after retrieval gain")
		}
		if directCount(old.Capped, old.Entity) > 0 {
			cappedBefore++
		}
		if directCount(updated.Capped, old.Entity) > 0 {
			cappedAfter++
		}
		for _, budget := range []int{512, 1024, 2048} {
			row := map[string]any{"entity": old.Entity, "budget": budget, "direct_fact_set_unchanged": true}
			for label, value := range map[string]evidence{"before": old.Wide, "after": updated.Wide} {
				selected := evidence{Facts: []json.RawMessage{}, Retrieval: value.Retrieval}
				for _, fact := range value.Facts {
					candidate := evidence{Facts: append(slices.Clone(selected.Facts), fact), Retrieval: value.Retrieval}
					if count(candidate) > budget {
						break
					}
					selected = candidate
				}
				row[label] = map[string]any{"tokens": count(selected), "facts": len(selected.Facts), "direct_facts": directCount(selected, old.Entity)}
			}
			rows = append(rows, row)
		}
	}
	result := map[string]any{"valid_at": before.At, "queries": len(before.Cases), "codec": "cl100k_base (Aura vendored vocabulary; provider-tokenizer estimate)", "budget_unit": "full compact JSON facts and retrieval metadata; complete fact prefix", "conversation_tokens_borrowed": 0, "capped_direct_before": cappedBefore, "capped_direct_after": cappedAfter, "rows": rows}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		panic(err)
	}
}
