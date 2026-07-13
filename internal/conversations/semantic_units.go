package conversations

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// SemanticState records whether a persisted interaction lifecycle is closed.
type SemanticState string

// Closed and unsafe lifecycle states understood by the selector.
const (
	SemanticStateComplete  SemanticState = "complete"
	SemanticStateOpen      SemanticState = "open"
	SemanticStateCancelled SemanticState = "cancelled"
	SemanticStateRetried   SemanticState = "retried"
)

// SemanticTurn is the selector's immutable projection of a canonical turn.
type SemanticTurn struct {
	Seq                                                            int
	Role, Content                                                  string
	Tokens                                                         int
	ParentTurnID, InvocationID, ToolCallID, ApprovalID, ResponseID string
	ToolCallIDs                                                    []string
	ResultFor                                                      string
	State                                                          SemanticState
	Protected                                                      bool
	LegacyClosure                                                  string
	Reconstructable                                                bool
	AuthorizedReference                                            string
}

// SemanticUnit is an atomic interaction lifecycle that selection never splits.
type SemanticUnit struct {
	Key                 string
	Turns               []SemanticTurn
	Tokens              int
	Eligible, Protected bool
	Reason              string
}

// BuildSemanticUnits groups canonical turns and validates tool/stream lifecycle closure.
func BuildSemanticUnits(turns []SemanticTurn) []SemanticUnit {
	groups := make(map[string]*SemanticUnit)
	order := make([]string, 0)
	for _, turn := range turns {
		key := semanticUnitKey(turn)
		if _, ok := groups[key]; !ok {
			groups[key] = &SemanticUnit{Key: key, Eligible: true}
			order = append(order, key)
		}
		u := groups[key]
		u.Turns = append(u.Turns, turn)
		u.Tokens += max(0, turn.Tokens)
		u.Protected = u.Protected || turn.Protected || turn.Role == llm.RoleSystem
	}
	out := make([]SemanticUnit, 0, len(order))
	for _, key := range order {
		u := groups[key]
		validateSemanticUnit(u)
		out = append(out, *u)
	}
	return out
}

func semanticUnitKey(t SemanticTurn) string {
	if t.Protected || t.Role == llm.RoleSystem {
		return fmt.Sprintf("protected:%d", t.Seq)
	}
	for _, p := range []struct{ name, value string }{{"parent", t.ParentTurnID}, {"invocation", t.InvocationID}, {"approval", t.ApprovalID}, {"response", t.ResponseID}, {"tool", firstNonEmpty(t.ToolCallID, t.ResultFor)}} {
		if p.value != "" {
			return p.name + ":" + p.value
		}
	}
	if t.LegacyClosure != "" {
		return "legacy:" + t.LegacyClosure
	}
	return fmt.Sprintf("malformed:%d", t.Seq)
}
func validateSemanticUnit(u *SemanticUnit) {
	if u.Protected {
		u.Eligible = false
		u.Reason = "protected"
		return
	}
	calls := map[string]int{}
	results := map[string]int{}
	for _, t := range u.Turns {
		if t.Role != llm.RoleUser && t.Role != llm.RoleAssistant && t.Role != llm.RoleTool {
			u.Eligible = false
			u.Reason = "malformed_role"
			return
		}
		if t.State == SemanticStateOpen || t.State == SemanticStateCancelled || t.State == SemanticStateRetried {
			u.Eligible = false
			u.Reason = string(t.State)
			return
		}
		for _, id := range t.ToolCallIDs {
			if id == "" {
				u.Eligible = false
				u.Reason = "malformed_call"
				return
			}
			calls[id]++
		}
		if t.ResultFor != "" {
			results[t.ResultFor]++
		}
	}
	for id, n := range calls {
		if n != 1 || results[id] != 1 {
			u.Eligible = false
			u.Reason = "incomplete_tool_cycle"
			return
		}
	}
	for id, n := range results {
		if n != 1 || calls[id] != 1 {
			u.Eligible = false
			u.Reason = "orphan_or_duplicate_result"
			return
		}
	}
}
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// Deterministic no-op reason codes emitted by manifest selection.
const (
	NoOpNoEligiblePrefix = "no_eligible_prefix"
	NoOpTailAtomicityCap = "tail_atomicity_cap"
	NoOpMinimumSavings   = "minimum_savings"
)

// ManifestSelectionPolicy bounds recent continuity and useful savings.
type ManifestSelectionPolicy struct{ InputCapacity, RecentTailTokens, MinimumSavingTokens int }

// CompactionManifest classifies every captured turn exactly once.
type CompactionManifest struct {
	Summarized, Tail, Protected, Excluded []int
	NoOpReason                            string
}

// SelectCompactionManifests chooses a backward-expanded atomic tail and prefix.
func SelectCompactionManifests(units []SemanticUnit, p ManifestSelectionPolicy) CompactionManifest {
	m := CompactionManifest{}
	eligible := make([]int, 0)
	for i, u := range units {
		if u.Protected {
			appendSeqs(&m.Protected, u)
			continue
		}
		if !u.Eligible {
			appendSeqs(&m.Excluded, u)
			continue
		}
		eligible = append(eligible, i)
	}
	if len(eligible) == 0 {
		m.NoOpReason = NoOpNoEligiblePrefix
		return m
	}
	capTokens := p.InputCapacity / 5
	tailTokens := 0
	tailStart := len(eligible)
	for j := len(eligible) - 1; j >= 0; j-- {
		u := units[eligible[j]]
		if tailTokens == 0 || tailTokens < p.RecentTailTokens {
			if tailTokens+u.Tokens > capTokens {
				m.NoOpReason = NoOpTailAtomicityCap
				return m
			}
			tailTokens += u.Tokens
			tailStart = j
			continue
		}
		break
	}
	for j, idx := range eligible {
		if j < tailStart {
			appendSeqs(&m.Summarized, units[idx])
		} else {
			appendSeqs(&m.Tail, units[idx])
		}
	}
	saved := 0
	for _, idx := range eligible[:tailStart] {
		saved += units[idx].Tokens
	}
	if len(m.Summarized) == 0 || saved < p.MinimumSavingTokens {
		m.NoOpReason = NoOpMinimumSavings
		m.Excluded = append(m.Excluded, m.Summarized...)
		m.Summarized = nil
	}
	sort.Ints(m.Protected)
	sort.Ints(m.Excluded)
	return m
}
func appendSeqs(dst *[]int, u SemanticUnit) {
	for _, t := range u.Turns {
		*dst = append(*dst, t.Seq)
	}
}

// ContextEdit is a typed reconstructable L1 replacement.
type ContextEdit struct {
	TurnSeq         int
	Kind, Reference string
}

// L1EditsForUnits emits edits only when an authorized durable reference exists.
func L1EditsForUnits(units []SemanticUnit) []ContextEdit {
	var out []ContextEdit
	for _, u := range units {
		for _, t := range u.Turns {
			if t.Role == llm.RoleTool && t.Reconstructable && strings.TrimSpace(t.AuthorizedReference) != "" {
				out = append(out, ContextEdit{TurnSeq: t.Seq, Kind: "authorized_reference", Reference: t.AuthorizedReference})
			}
		}
	}
	return out
}
