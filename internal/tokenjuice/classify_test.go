package tokenjuice_test

import (
	"testing"

	"github.com/aura/aura/internal/tokenjuice"
)

func makeRule(id, family string, match tokenjuice.RuleMatch, priority *int32) *tokenjuice.CompiledRule {
	return &tokenjuice.CompiledRule{
		Rule: tokenjuice.Rule{
			ID:       id,
			Family:   family,
			Match:    match,
			Priority: priority,
		},
	}
}

// Test 1: empty rule set — no matched reducer, confidence 0.2
func TestClassifyEmptyRules(t *testing.T) {
	cls := tokenjuice.ClassifyExecution(tokenjuice.Input{ToolName: "bash"}, nil, nil)
	if cls.MatchedReducer != nil {
		t.Errorf("want no matched reducer, got %q", *cls.MatchedReducer)
	}
	if cls.Confidence != 0.2 {
		t.Errorf("want confidence 0.2, got %v", cls.Confidence)
	}
	if cls.Family != "generic" {
		t.Errorf("want family 'generic', got %q", cls.Family)
	}
}

// Test 2: single matching rule
func TestClassifySingleMatch(t *testing.T) {
	rule := makeRule("git/status", "git-status", tokenjuice.RuleMatch{
		ToolNames: []string{"bash"},
		Argv0:     []string{"git"},
	}, nil)
	in := tokenjuice.Input{ToolName: "bash", Argv: []string{"git", "status"}}
	cls := tokenjuice.ClassifyExecution(in, []*tokenjuice.CompiledRule{rule}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "git/status" {
		t.Errorf("want git/status, got %v", cls.MatchedReducer)
	}
	if cls.Confidence != 0.9 {
		t.Errorf("want confidence 0.9, got %v", cls.Confidence)
	}
}

// Test 3: competing rules — highest score wins
// ruleA toolNames only → score=10; ruleB toolNames+argv0 → score=110
func TestClassifyCompetingRulesHighestScoreWins(t *testing.T) {
	ruleA := makeRule("generic/tool", "generic", tokenjuice.RuleMatch{
		ToolNames: []string{"bash"},
	}, nil)
	ruleB := makeRule("git/status", "git-status", tokenjuice.RuleMatch{
		ToolNames: []string{"bash"},
		Argv0:     []string{"git"},
	}, nil)
	in := tokenjuice.Input{ToolName: "bash", Argv: []string{"git", "status"}}
	cls := tokenjuice.ClassifyExecution(in, []*tokenjuice.CompiledRule{ruleA, ruleB}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "git/status" {
		t.Errorf("want git/status (score 110 > 10), got %v", cls.MatchedReducer)
	}
}

// Test 4: toolNames filter — match and no-match paths
func TestClassifyToolNamesFilter(t *testing.T) {
	rule := makeRule("exec/shell", "generic", tokenjuice.RuleMatch{
		ToolNames: []string{"execute_shell"},
	}, nil)
	cls := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "execute_shell"},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "exec/shell" {
		t.Errorf("execute_shell should match exec/shell, got %v", cls.MatchedReducer)
	}
	cls2 := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "web_fetch"},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls2.MatchedReducer != nil {
		t.Errorf("web_fetch should not match, got %v", *cls2.MatchedReducer)
	}
}

// Test 5: commandIncludes filter — explicit command and argv synthesis
func TestClassifyCommandIncludesFilter(t *testing.T) {
	rule := makeRule("test/pytest", "test-results", tokenjuice.RuleMatch{
		CommandIncludes: []string{"pytest"},
	}, nil)
	// explicit command field
	cls := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Command: "python -m pytest ./tests"},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "test/pytest" {
		t.Errorf("command with pytest should match, got %v", cls.MatchedReducer)
	}
	// argv synthesized into command (no Command field)
	cls2 := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Argv: []string{"python", "-m", "pytest"}},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls2.MatchedReducer == nil || *cls2.MatchedReducer != "test/pytest" {
		t.Errorf("argv synthesis should match pytest, got %v", cls2.MatchedReducer)
	}
}

// Test 6: argvIncludes filter — element-exact matching, not substring
func TestClassifyArgvIncludesFilter(t *testing.T) {
	rule := makeRule("cargo/test", "test-results", tokenjuice.RuleMatch{
		Argv0:        []string{"cargo"},
		ArgvIncludes: [][]string{{"test"}},
	}, nil)
	// matches: argv[0]=cargo AND "test" present as element
	cls := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Argv: []string{"cargo", "test", "--release"}},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "cargo/test" {
		t.Errorf("cargo test should match, got %v", cls.MatchedReducer)
	}
	// does not match: "test" absent from argv
	cls2 := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Argv: []string{"cargo", "build"}},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls2.MatchedReducer != nil {
		t.Errorf("cargo build should not match, got %v", *cls2.MatchedReducer)
	}
}

// Test 7: generic/fallback rule receives confidence 0.2
func TestClassifyFallbackRuleLowConfidence(t *testing.T) {
	fallback := makeRule("generic/fallback", "generic", tokenjuice.RuleMatch{}, nil)
	cls := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "something_unknown"},
		[]*tokenjuice.CompiledRule{fallback}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "generic/fallback" {
		t.Errorf("fallback should match everything, got %v", cls.MatchedReducer)
	}
	if cls.Confidence != 0.2 {
		t.Errorf("fallback confidence should be 0.2, got %v", cls.Confidence)
	}
}

// Test 8: forced rule ID overrides scoring
func TestClassifyForcedRuleID(t *testing.T) {
	ruleA := makeRule("git/status", "git-status", tokenjuice.RuleMatch{Argv0: []string{"git"}}, nil)
	ruleB := makeRule("git/log", "git-log", tokenjuice.RuleMatch{Argv0: []string{"git"}}, nil)
	in := tokenjuice.Input{ToolName: "bash", Argv: []string{"git", "status"}}
	forced := "git/log"
	cls := tokenjuice.ClassifyExecution(in, []*tokenjuice.CompiledRule{ruleA, ruleB}, &forced)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "git/log" {
		t.Errorf("forced rule should win, got %v", cls.MatchedReducer)
	}
	if cls.Confidence != 1.0 {
		t.Errorf("forced confidence should be 1.0, got %v", cls.Confidence)
	}
}

// Test 9: forced rule not found falls through to normal matching
func TestClassifyForcedRuleNotFoundFallsThrough(t *testing.T) {
	rule := makeRule("git/status", "git-status", tokenjuice.RuleMatch{Argv0: []string{"git"}}, nil)
	in := tokenjuice.Input{ToolName: "bash", Argv: []string{"git", "status"}}
	forced := "nonexistent/rule"
	cls := tokenjuice.ClassifyExecution(in, []*tokenjuice.CompiledRule{rule}, &forced)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "git/status" {
		t.Errorf("should fall through to normal matching, got %v", cls.MatchedReducer)
	}
}

// Test 10: commandIncludesAny filter
func TestClassifyCommandIncludesAnyFilter(t *testing.T) {
	rule := makeRule("build/compiler", "build", tokenjuice.RuleMatch{
		CommandIncludesAny: []string{"tsc", "esbuild"},
	}, nil)
	cls := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Command: "npx tsc --noEmit"},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls.MatchedReducer == nil || *cls.MatchedReducer != "build/compiler" {
		t.Errorf("tsc command should match, got %v", cls.MatchedReducer)
	}
	cls2 := tokenjuice.ClassifyExecution(
		tokenjuice.Input{ToolName: "bash", Command: "go build ./..."},
		[]*tokenjuice.CompiledRule{rule}, nil)
	if cls2.MatchedReducer != nil {
		t.Errorf("go build should not match tsc/esbuild filter, got %v", *cls2.MatchedReducer)
	}
}
