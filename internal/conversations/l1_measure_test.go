//go:build measure

// Measurement harness (NOT a gate): quantifies what L1 microcompact actually does
// across realistic conversation shapes — how many tool turns are even ELIGIBLE for
// eviction, how many tokens the eviction saves, and how those savings compare to the
// hard cap at different context-window sizes.
//
// Run:
//
//	go test -tags measure ./internal/conversations -run TestMeasureL1 -v
//
// It asserts nothing about policy; it prints numbers. Build-tagged so it never runs in
// the normal suite or the coverage gate.
package conversations

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// previewCapBytes mirrors AURA_CONTEXT_PREVIEW_CAP_BYTES (tools.NewResult default): a
// tool output above it is truncated to this many bytes + a footer, with the full bytes
// in a sidecar. Only such turns are L1-eligible (isSidecarBacked).
const previewCapBytes = 30000

// spillFooter mirrors the footer tools.NewResult appends to a spilled preview. Its
// marker is what isSidecarBacked keys off when ContentSidecarPath is empty.
func spillFooter(id string) string {
	return fmt.Sprintf("\n\n[output truncated: 250000 bytes total, showing first %d. "+
		"Page the rest via read_tool_output(tool_call_id=%q)]", previewCapBytes, id)
}

// spilledToolTurn is a large tool result: a full-cap preview + the spill footer, i.e.
// what a web_fetch of a big page or a chatty shell_exec leaves in history.
func spilledToolTurn(seq int, id string) Turn {
	return Turn{
		Seq:                seq,
		Role:               llm.RoleTool,
		ToolCallID:         id,
		Content:            strings.Repeat("lorem ipsum dolor sit amet ", previewCapBytes/27) + spillFooter(id),
		ContentSidecarPath: "/run/sidecar/" + id,
	}
}

// smallToolTurn is an ordinary tool result under the preview cap: NOT sidecar-backed,
// so L1 can never evict it no matter how old it gets.
func smallToolTurn(seq int, id string, bytes int) Turn {
	return Turn{
		Seq:        seq,
		Role:       llm.RoleTool,
		ToolCallID: id,
		Content:    strings.Repeat("result line here\n", bytes/17),
	}
}

// shape builds `rounds` user/assistant/tool rounds; spilledEvery N-th round carries a
// large (sidecar-backed) tool result, the rest carry small ones.
func shape(rounds int, spilledEvery int, smallBytes int) []Turn {
	turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"}}
	seq := 2
	for r := range rounds {
		id := fmt.Sprintf("call_%d", r)
		turns = append(turns,
			Turn{Seq: seq, Role: llm.RoleUser, Content: "do the thing please, with some detail"},
			Turn{Seq: seq + 1, Role: llm.RoleAssistant, Content: "calling a tool"},
		)
		seq += 2
		if spilledEvery > 0 && r%spilledEvery == 0 {
			turns = append(turns, spilledToolTurn(seq, id))
		} else {
			turns = append(turns, smallToolTurn(seq, id, smallBytes))
		}
		seq++
		turns = append(turns, Turn{Seq: seq, Role: llm.RoleAssistant, Content: "here is the answer to your question"})
		seq++
	}
	return turns
}

func eligibleCount(turns []Turn, evictAfter int) (eligible, toolTurns int) {
	if len(turns) == 0 {
		return 0, 0
	}
	threshold := turns[len(turns)-1].Seq - evictAfter
	for _, t := range turns {
		if t.Role != llm.RoleTool {
			continue
		}
		toolTurns++
		if t.Seq != 1 && t.Seq < threshold && isSidecarBacked(t) {
			eligible++
		}
	}
	return eligible, toolTurns
}

// TestMeasureL1 prints L1's real effect per conversation shape.
func TestMeasureL1(t *testing.T) {
	enc := mustEncoderRaw(t)
	const evictAfter = 10 // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS default

	cases := []struct {
		name         string
		rounds       int
		spilledEvery int
		smallBytes   int
	}{
		{"all-small-tools (typical: fs_read, current_time, small greps)", 30, 0, 2000},
		{"1-in-4-spilled (mixed: occasional web_fetch/shell dump)", 30, 4, 2000},
		{"all-spilled (worst case: every tool output over cap)", 30, 1, 2000},
		{"long-session all-small (100 rounds)", 100, 0, 2000},
		{"long-session 1-in-4-spilled (100 rounds)", 100, 4, 2000},
	}

	// Hard caps for two representative windows (SPEC Req#10 formula, MaxOutput at the
	// 20k floor, 13k headroom): 1M-class local model vs a 200K-class cloud model.
	caps := map[string]int{
		"1M window":   ContextConfig{ContextWindow: 1048576, MaxOutputTokens: 32768}.HardCap(),
		"200K window": ContextConfig{ContextWindow: 200000, MaxOutputTokens: 8192}.HardCap(),
	}

	t.Logf("L1 evictAfter=%d  previewCap=%dB", evictAfter, previewCapBytes)
	for name, c := range caps {
		t.Logf("hard cap %s = %d tokens", name, c)
	}

	for _, tc := range cases {
		turns := shape(tc.rounds, tc.spilledEvery, tc.smallBytes)
		before := totalTokens(enc, turns)
		after := totalTokens(enc, applyL1(turns, evictAfter))
		saved := before - after
		eligible, toolTurns := eligibleCount(turns, evictAfter)

		pct := 0.0
		if before > 0 {
			pct = float64(saved) / float64(before) * 100
		}
		t.Logf("\n--- %s ---", tc.name)
		t.Logf("  turns=%d toolTurns=%d L1-eligible=%d (%.0f%% of tool turns)",
			len(turns), toolTurns, eligible, pctOf(eligible, toolTurns))
		t.Logf("  tokens before=%d after=%d  saved=%d (%.1f%% of history)", before, after, saved, pct)
		for name, cap := range caps {
			t.Logf("  vs %s: history is %.1f%% of cap before L1, %.1f%% after; saving is %.2f%% of cap",
				name, pctOf(before, cap), pctOf(after, cap), pctOf(saved, cap))
			if before <= cap {
				t.Logf("    ^ ALREADY UNDER CAP before L1 — eviction bought no budget headroom here")
			}
		}
	}
}

func pctOf(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}
