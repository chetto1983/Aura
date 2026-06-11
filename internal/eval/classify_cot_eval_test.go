//go:build cot_eval

// classify_cot_eval_test.go is the NO-KEY structural slot for the Phase 11 xlsx
// North-Star eval (D-35, RISCRITTO #51/D-40, host surface #52/D-41). It holds:
//
//   - classifyCall: the PURE action-aware capture (parses a tool call's structured
//     JSON arguments — the spike-012a pattern — and flags self-install evidence off a
//     shell_exec command line: `npx skills add` (selfInstall), targeting a concrete
//     skills source (installTarget), with xlsx/excel/spreadsheet capability evidence
//     in the add command (installSel)).
//     No I/O, no network.
//   - captureSkillCalls / makeAskCall: thin glue the live harness uses.
//   - TestClassifyCall_* / TestRegistry_SeamFree: table-driven pure tests that run
//     WITHOUT OPENROUTER_API_KEY (they touch no network and MUST NOT t.Skip on the key),
//     so a structural break — a deleted-seam reference, a regressed arg parser, a skill
//     tool sneaking back into the registry, or sandbox_exec replacing the host terminal —
//     is caught even when the paid live tier is gated off. These tests are the answer to
//     T-11-10-R1 (the old name-only ordered subsequence assertion was structurally
//     unsatisfiable; this proves the new arg-aware capture parses real command lines and
//     the registry mirrors production: shell_exec in, sandbox_exec out).
package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// classifyCall records one tool call action-aware and flags self-install evidence off
// the STRUCTURED arguments (spike 012a, host surface #52/D-41). It is a PURE function of
// (name, rawArgs): it only appends to res.toolCalls and sets the boolean flags — no I/O,
// no network — so the table-driven TestClassifyCall_* tests can drive it directly.
// Self-install evidence comes from a shell_exec command LINE running `npx skills add`,
// never from the model's prose (T-11-10-T1). shell_exec's wire is a single `command`
// string; an args-array shape is still tolerated for robustness (a model that splits the
// line, or a forward-compat tool), so both flatten to one command line before matching.
func classifyCall(res *skillsResult, name, rawArgs string) {
	switch name {
	case "shell_exec":
		cmd := shellCommandLine(rawArgs)
		res.toolCalls = append(res.toolCalls, "shell_exec("+oneLine(cmd)+")")
		flat := strings.ToLower(strings.Join(strings.Fields(cmd), " "))
		// A discovery/listing call (`skills find`, `skills add --list`) is NOT an install.
		isList := strings.Contains(flat, "skills find") ||
			(strings.Contains(flat, "skills add") && strings.Contains(flat, "--list"))
		if strings.Contains(flat, "skills add") && !isList {
			res.selfInstall = true
			if hasSkillsSource(flat) {
				res.installTarget = true
			}
			if hasSpreadsheetCapability(flat) {
				res.installSel = true
			}
		}
	default:
		res.toolCalls = append(res.toolCalls, name)
	}
}

// shellCommandLine extracts the command line from shell_exec's structured arguments. The
// canonical wire is `{"command":"<full line>"}`; an args-array shape (`{"command":"...",
// "args":[...]}`) is tolerated as a robustness branch and flattened into the same line.
// Malformed JSON yields the empty string (no panic — the malformed-args no-op test).
func shellCommandLine(rawArgs string) string {
	var a struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &a)
	return strings.TrimSpace(a.Command + " " + strings.Join(a.Args, " "))
}

// hasSkillSelector reports whether the flattened command line carries `--skill <sel>`.
// It checks both the spaced (`--skill xlsx`) and `=` forms.
func hasSkillSelector(flat, sel string) bool {
	return strings.Contains(flat, "--skill "+sel) || strings.Contains(flat, "--skill="+sel)
}

func hasSkillsSource(flat string) bool {
	fields := strings.Fields(flat)
	for i, f := range fields {
		if f != "add" || i+1 >= len(fields) {
			continue
		}
		spec := fields[i+1]
		return strings.Contains(spec, "/") || strings.Contains(spec, "@")
	}
	return false
}

func hasSpreadsheetCapability(flat string) bool {
	return hasSkillSelector(flat, "xlsx") ||
		strings.Contains(flat, "xlsx") ||
		strings.Contains(flat, "excel") ||
		strings.Contains(flat, "spreadsheet")
}

// captureSkillCalls feeds every captured tool call (name + raw args, aligned slices
// from the turnCapture) through classifyCall so the live harness records action-aware
// evidence. ask_user calls carry an empty args slot and fall to the default branch.
func captureSkillCalls(res *skillsResult, c *turnCapture) {
	for i := range c.toolNames {
		args := ""
		if i < len(c.toolArgs) {
			args = c.toolArgs[i]
		}
		classifyCall(res, c.toolNames[i], args)
	}
}

// makeAskCall reconstructs the assistant ask_user tool_call from a pause payload so the
// drive loop can append the wire-correct assistant message before the resume answer
// (D-A1-07 wire-correctness).
func makeAskCall(ai *agent.AwaitingInput) llm.ToolCall {
	return agenttest.MakeToolCall(ai.ToolCallID, "ask_user",
		mustJSONString(map[string]any{"question": ai.Question, "kind": ai.Kind}))
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// oneLine collapses newlines and truncates for compact log/report lines.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

// shellArgs renders a shell_exec arguments JSON string from a full command LINE — the
// canonical wire shape (`{"command":"<line>"}`).
func shellArgs(commandLine string) string {
	return mustJSONString(map[string]any{"command": commandLine})
}

// shellArgsArray renders the args-array robustness shape (`{"command":...,"args":[...]}`)
// that classifyCall also flattens — a model that splits the line, or a forward-compat
// tool. The table exercises this branch so the parser stays robust.
func shellArgsArray(command string, args ...string) string {
	return mustJSONString(map[string]any{"command": command, "args": args})
}

// TestClassifyCall_SelfInstall is the no-key structural proof that the action-aware
// capture parses REAL shell_exec command lines: a clean `npx skills add anthropics/skills
// --skill xlsx` flags selfInstall + installTarget + installSel; a bare add to another
// source flags selfInstall + installTarget but not installSel; current skills.sh specs
// like `owner/repo@document-xlsx` also satisfy installSel. Discovery `skills find` /
// `add --list` and non-install shell_exec flag NONE. It exercises both the canonical
// single-`command`-string wire and the args-array robustness branch. This catches the
// spike-012b regression (name-only
// capture → structurally-unsatisfiable assertion) without a live call.
func TestClassifyCall_SelfInstall(t *testing.T) {
	tests := []struct {
		name       string
		callName   string
		rawArgs    string
		wantSelf   bool
		wantTarget bool
		wantSel    bool
	}{
		{
			name:       "clean anthropics xlsx add (command line)",
			callName:   "shell_exec",
			rawArgs:    shellArgs("cd ~/.aura/skills/export && npx skills add anthropics/skills --skill xlsx --copy -y"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    true,
		},
		{
			name:       "bare add command line",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills add anthropics/skills --skill xlsx"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    true,
		},
		{
			name:       "selector with equals form",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills add anthropics/skills --skill=xlsx"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    true,
		},
		{
			name:       "args-array robustness shape still flattens",
			callName:   "shell_exec",
			rawArgs:    shellArgsArray("npx", "skills", "add", "anthropics/skills", "--skill", "xlsx", "--copy", "-y"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    true,
		},
		{
			name:       "bare add to another repo",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills add other/repo"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    false,
		},
		{
			name:       "current skills.sh document-xlsx source",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills add vasilyu1983/ai-agents-public@document-xlsx --copy -y"),
			wantSelf:   true,
			wantTarget: true,
			wantSel:    true,
		},
		{
			name:       "discovery skills find is not an install",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills find xlsx"),
			wantSelf:   false,
			wantTarget: false,
			wantSel:    false,
		},
		{
			name:       "add --list enumerates, does not install",
			callName:   "shell_exec",
			rawArgs:    shellArgs("npx skills add anthropics/skills --list"),
			wantSelf:   false,
			wantTarget: false,
			wantSel:    false,
		},
		{
			name:       "non-install shell_exec",
			callName:   "shell_exec",
			rawArgs:    shellArgs("python3 ~/.aura/skills/export/.agents/skills/xlsx/scripts/build.py"),
			wantSelf:   false,
			wantTarget: false,
			wantSel:    false,
		},
		{
			name:       "non-shell tool flagged none",
			callName:   "web_search",
			rawArgs:    `{"query":"yahoo finance today"}`,
			wantSelf:   false,
			wantTarget: false,
			wantSel:    false,
		},
		{
			name:       "malformed json flags none, no panic",
			callName:   "shell_exec",
			rawArgs:    `{not valid json`,
			wantSelf:   false,
			wantTarget: false,
			wantSel:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := &skillsResult{}
			classifyCall(res, tc.callName, tc.rawArgs)
			if res.selfInstall != tc.wantSelf {
				t.Errorf("selfInstall = %v, want %v (args=%s)", res.selfInstall, tc.wantSelf, tc.rawArgs)
			}
			if res.installTarget != tc.wantTarget {
				t.Errorf("installTarget = %v, want %v (args=%s)", res.installTarget, tc.wantTarget, tc.rawArgs)
			}
			if res.installSel != tc.wantSel {
				t.Errorf("installSel = %v, want %v (args=%s)", res.installSel, tc.wantSel, tc.rawArgs)
			}
			if len(res.toolCalls) != 1 {
				t.Errorf("expected exactly 1 recorded tool call, got %d (%v)", len(res.toolCalls), res.toolCalls)
			}
		})
	}
}

// TestClassifyCall_Accumulates proves a full find→add→use sequence accumulates the
// self-install flags exactly once and records every call action-aware — the realistic
// one-turn loop shape (spike 012a) without a live model.
func TestClassifyCall_Accumulates(t *testing.T) {
	res := &skillsResult{}
	seq := []struct{ name, args string }{
		{"shell_exec", shellArgs("npx skills find xlsx")},
		{"shell_exec", shellArgs("cd ~/.aura/skills/export && npx skills add anthropics/skills --skill xlsx --copy -y")},
		{"web_search", `{"query":"yahoo finance market today"}`},
		{"shell_exec", shellArgs("python3 ~/.aura/skills/export/.agents/skills/xlsx/scripts/create.py")},
	}
	for _, s := range seq {
		classifyCall(res, s.name, s.args)
	}
	if !res.selfInstall || !res.installTarget || !res.installSel {
		t.Fatalf("expected self-install evidence after the add, got self=%v target=%v sel=%v",
			res.selfInstall, res.installTarget, res.installSel)
	}
	if len(res.toolCalls) != len(seq) {
		t.Fatalf("expected %d recorded calls, got %d (%v)", len(seq), len(res.toolCalls), res.toolCalls)
	}
	// The discovery call must be recorded but NOT counted as an install.
	if !strings.Contains(res.toolCalls[0], "skills find") {
		t.Errorf("first recorded call should be the discovery find, got %q", res.toolCalls[0])
	}
}

// TestRegistry_SeamFree builds the seam-free skills registry (no live run, no key) and
// asserts (a) the production tool set is present — including shell_exec, the HOST full
// terminal — (b) sandbox_exec is NOT registered (the loop rides the host terminal, not the
// sandbox: #52/D-41), and (c) NO `skill` tool / catalog / installer seam (deleted in
// 11-09, #51/D-40). This is the structural guard that the eval registry mirrors
// production: shell_exec in, sandbox_exec out, no resurrected SkillTool.
func TestRegistry_SeamFree(t *testing.T) {
	cfg := config.LoadDB()
	reg := buildSeamFreeSkillsRegistry(cfg, "")

	got := map[string]bool{}
	for _, tool := range reg.All() {
		got[tool.Spec().Name] = true
	}
	want := []string{
		"text_response", "tool_search", "read_tool_output", "current_time",
		"ask_user", "web_search", "web_fetch", "shell_exec",
		// fs parity #53/D-42: production registers the native fs tools (#50/D-15c);
		// the eval mirror must too — fs_write is how the model authors file content.
		"fs_read", "fs_write", "fs_edit", "fs_grep", "fs_glob",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("seam-free registry is missing the expected tool %q (have %v)", name, keysOf(got))
		}
	}
	if got["sandbox_exec"] {
		t.Errorf("seam-free registry must NOT register sandbox_exec — the skills loop rides the HOST terminal (shell_exec), #52/D-41")
	}
	if got["skill"] {
		t.Errorf("seam-free registry must NOT register a `skill` tool — discovery+install is the host terminal (#51/D-40, #52/D-41)")
	}
	if got["catalog"] || got["install"] {
		t.Errorf("seam-free registry must NOT register catalog/install tools (deleted in 11-09)")
	}
}

// TestRegistrySnippetReuse_HasSkillTool is the no-key structural PARITY proof for the
// snippet-reuse path (18-04 Task 1, Pitfall 3 / #52-#53 rule 4). buildSeamFreeSkillsRegistry
// deliberately OMITS the `skill` tool (the find-skills loop self-extends via the host
// terminal, not a tool) — but a snippet-REUSE scenario MUST resolve `skill action=use` /
// `action=save_snippet`, so the sibling buildSnippetReuseRegistry registers the PRODUCTION
// skill tool over a temp skills root. This test asserts that parity WITHOUT a paid run, so
// a regression (the skill tool dropped from the reuse registry) fails CI's structural slot
// even when the live tier is gated off (no-skip-as-green). A nil pool builds a loader-only
// skill tool — enough to assert the tool's PRESENCE (the gate's live path supplies a pool
// for the write actions). It also re-asserts the seam-free guarantees still hold on the
// sibling (no sandbox_exec; the shell_exec host terminal present).
func TestRegistrySnippetReuse_HasSkillTool(t *testing.T) {
	cfg := config.LoadDB()
	root := t.TempDir()
	exportDir := t.TempDir()
	reg := buildSnippetReuseRegistry(cfg, "", root, exportDir, nil)

	got := map[string]bool{}
	for _, tool := range reg.All() {
		got[tool.Spec().Name] = true
	}
	if !got["skill"] {
		t.Errorf("snippet-reuse registry MUST register the production `skill` tool (parity, Pitfall 3) — have %v", keysOf(got))
	}
	// Parity also keeps the host-terminal surface (shell_exec) so a snippet `use` frame's
	// `<interpreter> <host-path>` command line has a tool to run it; sandbox_exec stays OUT
	// (the snippet posture is host-primary, D-01) — it's only the named per-run escalation.
	if !got["shell_exec"] {
		t.Errorf("snippet-reuse registry must keep shell_exec (the host terminal the by-path snippet frame runs on) — have %v", keysOf(got))
	}
	if got["sandbox_exec"] {
		t.Errorf("snippet-reuse registry must NOT register sandbox_exec — approved snippets run on the HOST terminal (D-01); sandbox is the named per-run escalation only")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
