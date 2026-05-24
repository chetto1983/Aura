package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// ActionHint maps an action name to the set of args that distinguish it.
// Used by GuessActionFromArgs to pick the most-likely action when the
// caller forgot to set the "action" field on an action-enum tool.
//
// Convention: order the schemas so the most-specific (largest RequiredKeys
// set) comes first. The matcher prefers the action with the most matching
// keys; ties go to the first listed.
type ActionHint struct {
	Name         string
	RequiredKeys []string // args that strongly suggest this action
}

// GuessActionFromArgs scores each candidate action by how many of its
// distinguishing keys appear in supplied, and returns the winner's name.
// Returns fallback when no action scores above zero (e.g. caller supplied
// no args at all — list/lint actions usually fit).
func GuessActionFromArgs(supplied map[string]any, hints []ActionHint, fallback string) string {
	best := fallback
	bestScore := 0
	for _, hint := range hints {
		score := 0
		for _, k := range hint.RequiredKeys {
			if _, ok := supplied[k]; ok {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = hint.Name
		}
	}
	return best
}

// RewriteVerbKeyAsAction salvages the common LLM mistake of using an action
// enum value as a parameter key, e.g. `web({"search":"hello"})` instead of
// `web({"action":"search","query":"hello"})`. When supplied has a string
// value under a key that matches a valid action, and that action has exactly
// ONE required key, we set action + rename the verb key to the required
// param name. Returns the new args map and true when a rewrite happened.
//
// Observed live 2026-05-24 Telegram turn #158 (web tool); the model
// emitted the action name as a param key and the legacy ActionRequiredError
// retry hint preserved the misnamed key, sending the model into a second
// failure round.
func RewriteVerbKeyAsAction(supplied map[string]any, validActions []string, hints []ActionHint) (map[string]any, bool) {
	if _, hasAction := supplied["action"]; hasAction {
		return supplied, false
	}
	for _, action := range validActions {
		val, isString := supplied[action].(string)
		if !isString || strings.TrimSpace(val) == "" {
			continue
		}
		var requiredKey string
		for _, hint := range hints {
			if hint.Name != action {
				continue
			}
			if len(hint.RequiredKeys) != 1 {
				return supplied, false // ambiguous — needs explicit rewrite path
			}
			requiredKey = hint.RequiredKeys[0]
		}
		if requiredKey == "" {
			return supplied, false
		}
		out := make(map[string]any, len(supplied)+1)
		for k, v := range supplied {
			if k == action {
				continue
			}
			out[k] = v
		}
		out["action"] = action
		out[requiredKey] = val
		return out, true
	}
	return supplied, false
}

// ActionRequiredError builds a self-correcting error for action-enum
// tools (search, file, source, task, web). Tells the model:
//  1. Which actions are valid
//  2. Which one its supplied keys most likely meant
//  3. A ready-to-copy retry JSON with the guessed action plus its
//     existing args
//
// The shape is deliberately verbose because the model reads error
// strings as instructions on the next turn. Including the example call
// inline drops the "didn't I just supply this?" retry pattern observed
// live 2026-05-13 (4 parallel `file` calls without action, all failed in
// 2 ms, then retried with action).
func ActionRequiredError(toolName string, validActions []string, supplied map[string]any, hints []ActionHint, fallbackAction string) error {
	guess := GuessActionFromArgs(supplied, hints, fallbackAction)

	// Build the retry example. Two passes:
	//   1. Drop "action" (we add the guessed one ourselves).
	//   2. When a supplied key matches a valid action name and that action
	//      has a single required param, rewrite key to param. Otherwise the
	//      retry hint would re-suggest the broken shape (observed live
	//      2026-05-24: web({"search":"X"}) -> retry hint re-included
	//      "search":"X" instead of "query":"X").
	guessRequiredKey := ""
	for _, hint := range hints {
		if hint.Name == guess && len(hint.RequiredKeys) == 1 {
			guessRequiredKey = hint.RequiredKeys[0]
		}
	}
	example := map[string]any{"action": guess}
	for k, v := range supplied {
		if k == "action" {
			continue
		}
		if guessRequiredKey != "" && k == guess {
			if s, ok := v.(string); ok {
				example[guessRequiredKey] = s
				continue
			}
		}
		example[k] = v
	}
	exJSON, _ := json.Marshal(example)

	suppliedKeys := sortedKeysOf(supplied)
	keysStr := "none"
	if len(suppliedKeys) > 0 {
		keysStr = strings.Join(suppliedKeys, ", ")
	}

	return fmt.Errorf(
		"%s: 'action' is required. Valid actions: %s. You supplied [%s] — looks like you meant '%s'. Retry with: %s(%s): %w",
		toolName,
		strings.Join(validActions, " | "),
		keysStr,
		guess,
		toolName,
		string(exJSON),
		llm.ErrSchemaValidation,
	)
}

// UnknownActionError mirrors ActionRequiredError for the case where the
// model picked an action that isn't valid for this tool. Same self-
// correcting shape: list valid actions, repeat the supplied args, hint
// the closest match.
func UnknownActionError(toolName, suppliedAction string, validActions []string, supplied map[string]any) error {
	closest := closestActionMatch(suppliedAction, validActions)
	hint := ""
	if closest != "" {
		example := map[string]any{"action": closest}
		for k, v := range supplied {
			if k != "action" {
				example[k] = v
			}
		}
		exJSON, _ := json.Marshal(example)
		hint = fmt.Sprintf(". Closest valid match: '%s'. Retry: %s(%s)", closest, toolName, string(exJSON))
	}
	return fmt.Errorf(
		"%s: unknown action %q. Valid actions: %s%s: %w",
		toolName,
		suppliedAction,
		strings.Join(validActions, " | "),
		hint,
		llm.ErrSchemaValidation,
	)
}

// closestActionMatch returns the valid action with the smallest Levenshtein
// distance to supplied, or "" when none is within 3 edits. The threshold
// keeps "read"→"red" or "write"→"writ" matched while not coercing
// completely unrelated typos.
func closestActionMatch(supplied string, valid []string) string {
	supplied = strings.ToLower(strings.TrimSpace(supplied))
	if supplied == "" || len(valid) == 0 {
		return ""
	}
	bestDist := 4
	best := ""
	for _, v := range valid {
		d := editDistance(supplied, strings.ToLower(v))
		if d < bestDist {
			bestDist = d
			best = v
		}
	}
	return best
}

// editDistance is the classic O(m*n) Levenshtein distance. Tools have
// 3-6 valid actions of ≤8 chars each — the table is tiny.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = minInt(
				prev[j]+1,      // deletion
				cur[j-1]+1,     // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func minInt(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func sortedKeysOf(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ActionVariant declares the action-specific required keys for the
// JSON Schema "oneOf" guidance block produced by ActionDispatchOneOf.
// Name is the action value; RequiredKeys lists fields the model MUST
// supply when picking this action (in addition to "action" itself,
// which the helper prepends automatically).
type ActionVariant struct {
	Name         string
	RequiredKeys []string
}

// ActionDispatchOneOf builds a JSON Schema `oneOf` block that the LLM
// reads as: "if action == X, then required = [action, ...]". This is
// the structural fix for action-enum tools (search, wiki_page, file, task,
// source, web): the top-level `required: [action]` alone tells
// the model the OTHER fields are optional, and models confidently emit
// tool_calls with missing params. The oneOf variants are the only
// reliable channel that survives prose-vs-schema conflicts.
//
// Usage in a tool's Parameters() return map:
//
//	"oneOf": ActionDispatchOneOf([]ActionVariant{
//	    {Name: "create", RequiredKeys: []string{"title", "body"}},
//	    {Name: "edit",   RequiredKeys: []string{"slug", "old_text", "new_text", "expected_updated_at"}},
//	    ...
//	}),
//
// The helper guarantees:
//   - every variant requires "action" first (positional clarity);
//   - the action property is locked via `{"const": "<name>"}` so the
//     model can't satisfy a wrong variant by accident.
func ActionDispatchOneOf(variants []ActionVariant) []map[string]any {
	out := make([]map[string]any, 0, len(variants))
	for _, v := range variants {
		req := make([]string, 0, len(v.RequiredKeys)+1)
		req = append(req, "action")
		req = append(req, v.RequiredKeys...)
		out = append(out, map[string]any{
			"properties": map[string]any{
				"action": map[string]any{"const": v.Name},
			},
			"required": req,
		})
	}
	return out
}
