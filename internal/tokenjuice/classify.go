package tokenjuice

import (
	"sort"
	"strings"
)

// commandForMatching returns the command string for commandIncludes checks.
// If input.Command is set it is used; otherwise argv elements are joined with spaces.
func commandForMatching(in Input) string {
	if in.Command != "" {
		return in.Command
	}
	if len(in.Argv) > 0 {
		return strings.Join(in.Argv, " ")
	}
	return ""
}

// includesAll reports whether every element of group appears as an exact
// element somewhere in argv (not a substring — exact element match per spec §2.1).
func includesAll(argv []string, group []string) bool {
	for _, want := range group {
		found := false
		for _, a := range argv {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// matchesRule tests whether rule's match predicate applies to in.
// Checks each dimension in spec §2.1 order; short-circuits on first failure.
// A nil / empty dimension is unconstrained (never fails).
func matchesRule(rule *CompiledRule, in Input) bool {
	m := rule.Rule.Match

	// 1. toolNames
	if len(m.ToolNames) > 0 {
		found := false
		for _, n := range m.ToolNames {
			if n == in.ToolName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// 2. argv0
	if len(m.Argv0) > 0 {
		argv0 := ""
		if len(in.Argv) > 0 {
			argv0 = in.Argv[0]
		}
		found := false
		for _, v := range m.Argv0 {
			if v == argv0 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// 3. argvIncludes: ALL groups must match
	for _, group := range m.ArgvIncludes {
		if !includesAll(in.Argv, group) {
			return false
		}
	}

	// 4. argvIncludesAny: at least one group must match
	if len(m.ArgvIncludesAny) > 0 {
		anyMatch := false
		for _, group := range m.ArgvIncludesAny {
			if includesAll(in.Argv, group) {
				anyMatch = true
				break
			}
		}
		if !anyMatch {
			return false
		}
	}

	// 5. commandIncludes: ALL substrings must appear in command
	cmd := commandForMatching(in)
	for _, sub := range m.CommandIncludes {
		if !strings.Contains(cmd, sub) {
			return false
		}
	}

	// 6. commandIncludesAny: at least one substring must appear
	if len(m.CommandIncludesAny) > 0 {
		anyMatch := false
		for _, sub := range m.CommandIncludesAny {
			if strings.Contains(cmd, sub) {
				anyMatch = true
				break
			}
		}
		if !anyMatch {
			return false
		}
	}

	return true
}

// scoreRule computes the specificity score for a rule.
// Per algorithm spec §2.2:
//
//	score = priority×1000 + |argv0|×100 + Σ|argvIncludes groups|×40 +
//	        Σ|argvIncludesAny groups|×35 + |commandIncludes|×25 +
//	        |commandIncludesAny|×20 + |toolNames|×10
func scoreRule(rule *CompiledRule) int {
	m := rule.Rule.Match
	score := 0
	if rule.Rule.Priority != nil {
		score += int(*rule.Rule.Priority) * 1000
	}
	score += len(m.Argv0) * 100
	for _, group := range m.ArgvIncludes {
		score += len(group) * 40
	}
	for _, group := range m.ArgvIncludesAny {
		score += len(group) * 35
	}
	score += len(m.CommandIncludes) * 25
	score += len(m.CommandIncludesAny) * 20
	score += len(m.ToolNames) * 10
	return score
}

// ClassifyExecution selects the best-matching rule for in from rules using
// score-based selection (algorithm spec §2.3).
//
// If forcedRuleID is non-nil and non-empty and a rule with that ID is found,
// it is returned with confidence 1.0 (forced-ID-not-found silently falls through
// to normal matching per spec).
//
// If no rule matches, returns family "generic" with confidence 0.2 and nil
// MatchedReducer. The "generic/fallback" rule is also assigned confidence 0.2.
// All other matched rules receive confidence 0.9.
// Equal-score ties are broken alphabetically by rule ID.
func ClassifyExecution(in Input, rules []*CompiledRule, forcedRuleID *string) ClassificationResult {
	if forcedRuleID != nil && *forcedRuleID != "" {
		for _, r := range rules {
			if r.Rule.ID == *forcedRuleID {
				id := r.Rule.ID
				return ClassificationResult{
					Family:         r.Rule.Family,
					Confidence:     1.0,
					MatchedReducer: &id,
				}
			}
		}
		// forced ID not found — fall through to normal scoring
	}

	var matched []*CompiledRule
	for _, r := range rules {
		if matchesRule(r, in) {
			matched = append(matched, r)
		}
	}

	if len(matched) == 0 {
		return ClassificationResult{Family: "generic", Confidence: 0.2}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		si := scoreRule(matched[i])
		sj := scoreRule(matched[j])
		if si != sj {
			return si > sj
		}
		return matched[i].Rule.ID < matched[j].Rule.ID
	})

	best := matched[0]
	confidence := 0.9
	if best.Rule.ID == "generic/fallback" {
		confidence = 0.2
	}
	id := best.Rule.ID
	return ClassificationResult{
		Family:         best.Rule.Family,
		Confidence:     confidence,
		MatchedReducer: &id,
	}
}
