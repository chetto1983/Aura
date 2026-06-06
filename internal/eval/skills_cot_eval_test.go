//go:build cot_eval

// skills_cot_eval_test.go is the Phase 11 (CAP-07 / CAP-08 / D-35, RISCRITTO by
// amendment #51 / D-40) LIVE dual-gate xlsx North-Star E2E. It drives the REAL parent
// agent.LlmAgent over the REAL openai_compat client against DeepSeek-V4 with a
// SEAM-FREE registry (text_response + tool_search + read_tool_output + current_time +
// ask_user + web_search + web_fetch + sandbox_exec — NO `skill` tool, NO catalog/
// installer: those Go seams were deleted in 11-09), PLUS the always-on find-skills-aura
// skill body injected at messages[1]. A NATURAL prompt (NO "skill"/"install" word) must
// drive the model to recognise the capability gap, discover anthropics/skills/xlsx on
// skills.sh via `npx skills find xlsx` IN THE SANDBOX, self-install it
// (`npx skills add anthropics/skills --skill xlsx`), use it, pull today's data through
// the web tools, and produce a real .xlsx — all on its own, no approval round-trip
// (#51/D-40). This is the spike-012a buildSkillDrivenRegistry shape (4/4 live PASS, the
// full find→add→use→artifact loop in one turn, 212s).
//
// The dual gate (D-35, RISCRITTO #51/D-40) is:
//
//   - HARD FLOOR (artifact-not-reply ground truth): SELF-INSTALL evidence read from
//     STRUCTURED tool args — a sandbox_exec command line ran `npx skills add` targeting
//     `anthropics/skills` with the `xlsx` selector (classifyCall over
//     resp.ToolCalls[].Function.Arguments, NEVER the prose); AND the produced .xlsx
//     EXISTS in the sandbox workspace FRESH (newer than run start), OPENS (re-read via a
//     sandbox_exec openpyxl read-back), and CONTAINS today's date.
//   - JUDGE ≥90% AVERAGE over: capability-gap recognition + output quality (D-35).
//     The install-prudence dimension is DROPPED (#51/D-40 — no install-approval ceremony).
//
// THIS IS A MANUAL, OPERATOR-AUTHORIZED, PAID GATE — NOT A CI JOB. It is gated on
// OPENROUTER_API_KEY and behind the cot_eval build tag so it NEVER runs in normal CI /
// Makefile quality targets (no-skip-as-green: CI gates the unit/db_integration/
// sandbox_integration tiers — this paid LLM tier is the ONE legitimate skip, exactly
// like TestCoTEval/TestSwarmE2E). With the key unset it t.Skips with a clear message.
// The pure-function structural slot (classifyCall arg parsing + the seam-free registry
// construction — TestClassify*/TestRegistry* in classify_cot_eval_test.go) runs WITHOUT
// the key, so a structural break is caught even when this paid tier is gated off.
//
// OPERATOR INVOCATION (the full live run that fills the docs/aura-quality-snapshot.md
// TBD row with real numbers):
//
//  1. Rebuild + bring up the sandbox-agent with the baked xlsx deps (openpyxl/
//     defusedxml/lxml/validators, spike 006/007) + the ro /skills mount + the bearer:
//
//     docker compose build aura-sandbox-agent       # bake the xlsx deps (11-07 carry-over)
//     make sandbox-up                               # --token + /skills ro mount
//
//  2. Bring up SearXNG (the web tools backend):
//
//     docker compose up -d searxng                  # or the socat bridge per memory
//
//  3. Export the live env + run the tier:
//
//     set -a; . ./.env; set +a
//     export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//     export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468
//     export AURA_SANDBOX_AGENT_TOKEN=<the .env token>
//     export AURA_SKILLS_DIR=<the active skills root>            # find-skills-aura builtin
//     export SEARXNG_URL=http://127.0.0.1:18080/search           # or the bridge
//     go test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
//
//  4. OPEN the produced .xlsx visually (feedback_inspect_artifact_visually_not_just_pass_status)
//     and replace the Phase-11 TBD placeholder rows in docs/aura-quality-snapshot.md
//     with the observed judge %, coverage, and mutation numbers.
package eval

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/sandboxagent"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/chetto1983/aura/internal/web"
	"github.com/google/uuid"
)

// skillsReportPath is the scored xlsx-E2E report destination (docs/, never /tmp —
// CLAUDE.md). The operator copies its numbers into the quality snapshot.
const skillsReportPath = "../../docs/aura-skills-eval-2026-06-05.md"

// maxSkillsHops bounds the agent drive loop. Under the no-ceremony directive (#51/D-40)
// the model self-installs in the sandbox without an approval pause, so the full
// find→add→use→artifact loop typically closes in ONE turn (spike 012a). The cap is a
// safety net for a model that pauses unexpectedly (any pause is answered generically
// and the loop continues), bounding paid spend.
const maxSkillsHops = 6

// skillsResult accumulates the observed hard-floor + judge numbers for the report.
// The hard floor is action-aware: self-install evidence comes from STRUCTURED tool args
// (classifyCall over Function.Arguments), the artifact from the live sandbox read-back —
// never from the model's prose (T-11-10-T1 / feedback_probe_must_verify_artifact_not_reply).
type skillsResult struct {
	toolCalls     []string // action-aware tool calls (e.g. "sandbox_exec(npx skills add ...)")
	promptNatural bool     // the prompt contains none of the forbidden words
	selfInstall   bool     // a sandbox command line ran `npx skills add` (structured-arg evidence)
	installTarget bool     // the self-install targeted anthropics/skills (the North-Star repo)
	installSel    bool     // the self-install carried the --skill xlsx selector
	xlsxFresh     bool     // a .xlsx newer than run start exists in the workspace
	xlsxExists    bool     // a .xlsx is present in the workspace (== xlsxFresh, ground truth)
	xlsxOpens     bool     // the .xlsx re-opened via openpyxl read-back
	xlsxToday     bool     // the re-opened .xlsx contains today's date
	xlsxPath      string   // the workspace path of the produced artifact
	judgeScores   map[dimension]int
	judgeMean     float64
	judgePass     bool
	notes         []string
}

// TestSkillsE2E is the live dual-gate xlsx North-Star entry point (operator-run, paid).
func TestSkillsE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("skills E2E: OPENROUTER_API_KEY unset — this is a MANUAL paid gate, NOT a CI job. " +
			"See the file header for the operator invocation (sandbox-up + searxng + .env + go test). " +
			"The structural surface (classifyCall + seam-free registry) is covered key-free by TestClassify*/TestRegistry*.")
	}
	sandboxURL := os.Getenv("AURA_SANDBOX_AGENT_URL")
	skillsDir := os.Getenv("AURA_SKILLS_DIR")
	if sandboxURL == "" || skillsDir == "" {
		t.Skip("skills E2E: set AURA_SANDBOX_AGENT_URL + AURA_SKILLS_DIR " +
			"(the live sandbox + the active skills root that carries the find-skills-aura builtin). See the file header.")
	}

	baseCfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	secret := baseCfg.APIKey
	client := openai_compat.New(*baseCfg)

	appCfg := config.LoadDB()
	reg, alwaysBlock, sandboxClient := buildSkillsRegistry(t, appCfg)

	res := &skillsResult{judgeScores: map[dimension]int{}}
	scs := skillsScenarios()
	ctx := context.Background()
	for _, sc := range scs {
		if sc.skills == nil {
			continue
		}
		runSkillsScenario(t, ctx, client, *baseCfg, secret, reg, alwaysBlock, appCfg, sc, sandboxClient, res)
	}

	writeSkillsReport(t, baseCfg.Model, res)
	enforceSkills(t, res)
}

// buildSkillsRegistry builds the SEAM-FREE eval registry for the skills North Star
// (spike-012a buildSkillDrivenRegistry shape, #51/D-40): the production read tools +
// ask_user + the web tools (web_search/web_fetch) + sandbox_exec (the live
// sandbox-agent) — NO `skill` tool, NO catalog/installer (those Go seams were deleted
// in 11-09). The model self-extends via the sandbox terminal + the always-on
// find-skills-aura skill body returned as the messages[1] always-block. It returns the
// registry, that always-block, and the raw sandbox-agent client so the read-back can
// re-open the produced .xlsx by path.
func buildSkillsRegistry(t *testing.T, cfg *config.Config) (reg *tools.Registry, alwaysBlock string, sandboxClient *sandboxagent.Client) {
	t.Helper()
	sandboxClient = sandboxagent.New(cfg.SandboxAgent)
	reg = buildSeamFreeSkillsRegistry(cfg, sandboxClient)

	// The always-on find-skills-aura skill teaches the `npx skills find/add` discovery+
	// install loop. MaterializeBuiltins seeds it into the active root; RenderAlwaysBlock
	// produces the byte-stable messages[1] block exactly as production does (D-07).
	if err := skills.MaterializeBuiltins(cfg.SkillsDir); err != nil {
		t.Logf("skills E2E: materialize builtins (non-fatal): %v", err)
	}
	loader := skills.NewLoader(skills.Config{
		Roots:        []string{cfg.SkillsDir},
		BodyCapBytes: cfg.SkillBodyCapBytes,
		Blocklist:    cfg.SkillInjectionBlocklist,
	})
	block, present := skills.RenderAlwaysBlock(loader.List())
	if !present {
		t.Logf("skills E2E: no always:true skill in %s — the model gets no discovery teaching; "+
			"ensure find-skills-aura materialized", cfg.SkillsDir)
	}
	return reg, block, sandboxClient
}

// buildSeamFreeSkillsRegistry is the pure registry constructor (no t, no I/O) so the
// no-key TestRegistry_SeamFree can build + assert the same tool set without a live run.
// It is the spike-012a buildSkillDrivenRegistry surface: the production read tools +
// ask_user + the web tools + sandbox_exec, with NO `skill` tool and NO catalog/installer
// seam. sandboxClient is the live (or, in the test, nil) sandbox-agent runner.
func buildSeamFreeSkillsRegistry(cfg *config.Config, sandboxClient *sandboxagent.Client) *tools.Registry {
	reg := buildRegistry() // text_response + tool_search + read_tool_output + current_time
	reg.Register(tools.AskUser{})
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine})
	reg.Register(&tools.SandboxExec{Runner: sandboxClient})
	return reg
}

// runSkillsScenario drives the natural prompt through the agent loop, captures the
// action-aware tool calls (self-install evidence from structured args), does the
// fresh-artifact read-back, and runs the 2-dim judge.
func runSkillsScenario(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	secret string, reg *tools.Registry, alwaysBlock string, appCfg *config.Config, sc scenario,
	sandboxClient *sandboxagent.Client, res *skillsResult,
) {
	t.Helper()
	prompt := sc.prompts[0]

	// The prompt MUST be natural: none of the forbidden hint words appear (D-35).
	res.promptNatural = true
	low := strings.ToLower(prompt)
	for _, w := range sc.skills.forbiddenWords {
		if strings.Contains(low, strings.ToLower(w)) {
			res.promptNatural = false
			res.notes = append(res.notes, "FORBIDDEN WORD in prompt: "+w)
			t.Errorf("skills: the prompt must be natural (no %q) — got %q", w, prompt)
		}
	}

	sessionID := uuid.NewString()
	runStart := time.Now()
	finalProse := driveSkillsLoop(t, ctx, client, cfg, reg, alwaysBlock, appCfg, prompt, sessionID, res)

	if secret != "" && strings.Contains(finalProse, secret) {
		res.notes = append(res.notes, "SECRET LEAK DETECTED")
		t.Errorf("skills: SECRET LEAK — OPENROUTER_API_KEY value appears in the final answer")
	}

	// Assert the self-install targeted the right repo + selector (structured-arg ground
	// truth, recorded by classifyCall during the loop).
	if !res.installTarget {
		res.notes = append(res.notes, "self-install did not target "+sc.skills.installTargetRepo)
	}
	if !res.installSel {
		res.notes = append(res.notes, "self-install did not carry the --skill "+sc.skills.installSelector+" selector")
	}

	// Artifact ground truth (artifact-not-reply, D-35): find the FRESH .xlsx in the
	// workspace, re-open it via openpyxl, assert it carries today's date. NEVER trust
	// the prose.
	verifyXlsxArtifact(t, ctx, sandboxClient, sc.skills, runStart, res)

	observed := fmt.Sprintf(
		"self-install ran `npx skills add`: %v; targeted %s: %v; --skill %s selector: %v; "+
			".xlsx produced fresh: %v; .xlsx re-opened via openpyxl: %v; contains today's date: %v",
		res.selfInstall, sc.skills.installTargetRepo, res.installTarget, sc.skills.installSelector, res.installSel,
		res.xlsxFresh, res.xlsxOpens, res.xlsxToday)

	dims := []dimension{dimCapabilityGapRecognition, dimSkillOutputQuality}
	jctx, jcancel := context.WithTimeout(ctx, 150*time.Second)
	defer jcancel()
	scores, mean, pass, jerr := runSkillsJudge(jctx, client, cfg.Model, dims, prompt, finalProse, observed)
	if jerr != nil {
		res.notes = append(res.notes, "skills judge error: "+jerr.Error())
		t.Errorf("skills judge error: %v", jerr)
	}
	for d, s := range scores {
		res.judgeScores[d] = s
	}
	res.judgeMean, res.judgePass = mean, pass
	res.notes = append(res.notes, fmt.Sprintf("selfInstall=%v target=%v selector=%v xlsx(fresh=%v opens=%v today=%v) judgeMean=%.2f",
		res.selfInstall, res.installTarget, res.installSel, res.xlsxFresh, res.xlsxOpens, res.xlsxToday, res.judgeMean))
}

// driveSkillsLoop runs the agent over successive turns. The model self-installs in the
// sandbox with no approval round-trip (#51/D-40), so the loop usually terminates in one
// turn; any unexpected pause is answered generically ("proceed autonomously") and the
// loop continues. Each turn's tool calls are captured ACTION-AWARE via classifyCall over
// the structured arguments. It returns the final assistant prose (a turn that ended
// without a pause).
func driveSkillsLoop(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, alwaysBlock string, appCfg *config.Config, prompt, sessionID string, res *skillsResult,
) string {
	t.Helper()
	history := []llm.Message{}
	if alwaysBlock != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: alwaysBlock})
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: prompt})

	for hop := 0; hop < maxSkillsHops; hop++ {
		c := runSkillsTurn(t, ctx, client, cfg, reg, appCfg, history, sessionID)
		logTurnTrace(t, fmt.Sprintf("skills-hop-%d", hop), c)
		captureSkillCalls(res, c)

		if !c.paused || c.awaitingInput == nil {
			return c.prose // a terminal turn — done
		}

		// Under #51/D-40 the model should NOT pause for install approval. Any pause is
		// answered generically (the operator stands in: "proceed autonomously") and the
		// loop continues — a pause for genuine clarification is acceptable, an
		// install-approval pause is itself a finding the judge can see in the prose.
		ai := c.awaitingInput
		res.notes = append(res.notes, "unexpected pause: "+oneLine(ai.Question))
		askCall := makeAskCall(ai)
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall}},
			llm.Message{Role: llm.RoleTool, ToolCallID: ai.ToolCallID,
				Content: "Procedi pure in autonomia: dati di oggi da Yahoo Finance, nessun'altra preferenza. Non chiedere conferme."},
		)
	}
	res.notes = append(res.notes, fmt.Sprintf("skills loop hit the %d-hop cap without a terminal turn", maxSkillsHops))
	return ""
}

// runSkillsTurn constructs a real LlmAgent over the seam-free registry + the accumulated
// history and drains one turn into a turnCapture. A fixed sessionID keeps the
// sandbox-agent workspace consistent across hops so the produced .xlsx persists.
func runSkillsTurn(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, appCfg *config.Config, history []llm.Message, sessionID string,
) *turnCapture {
	t.Helper()
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        cfg,
		Registry:   reg,
		PreviewCap: appCfg.ToolPreviewCap,
		RunDir:     t.TempDir(),
		SessionID:  sessionID,
		UserTurns:  history,
	})
	bud, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ic := agent.InvocationContext{
		Ctx:       turnCtx,
		Agent:     la,
		RequestID: uuid.New(),
		Branch:    "root",
		Budget:    bud,
	}
	return captureTurn(func() func(func(*agent.Event, error) bool) { return la.Run(ic) })
}

// verifyXlsxArtifact (the D-35 artifact ground truth) lives in
// skills_xlsx_verify_cot_eval_test.go (600-LOC split).
// classifyCall + the action-aware capture live in classify_cot_eval_test.go so the
// no-key TestClassify* tests can table-drive the pure parser.

// ---- report + enforcement ----

// enforceSkills applies the D-35 dual gate (RISCRITTO #51/D-40): the hard floor
// (natural prompt, self-install evidence from structured args targeting anthropics/
// skills + the xlsx selector, the fresh .xlsx that opens and contains today's data) AND
// the judge ≥90% average over 2 dims are release-blocking. The old ordered-subsequence
// and install-approval signals are GONE.
func enforceSkills(t *testing.T, res *skillsResult) {
	t.Helper()
	if !res.promptNatural {
		t.Errorf("HARD FLOOR: the prompt was not natural (a forbidden hint word leaked in)")
	}
	if !res.selfInstall {
		t.Errorf("HARD FLOOR: no self-install evidence — no sandbox command line ran `npx skills add` (structured-arg ground truth), got %v", res.toolCalls)
	}
	if !res.installTarget {
		t.Errorf("HARD FLOOR: the self-install did not target anthropics/skills (the North-Star repo)")
	}
	if !res.installSel {
		t.Errorf("HARD FLOOR: the self-install did not carry the --skill xlsx selector")
	}
	if !res.xlsxExists {
		t.Errorf("HARD FLOOR: no FRESH .xlsx artifact found in the sandbox workspace (artifact-not-reply / stale)")
	}
	if !res.xlsxOpens {
		t.Errorf("HARD FLOOR: the produced .xlsx did not re-open via openpyxl")
	}
	if !res.xlsxToday {
		t.Errorf("HARD FLOOR: the produced .xlsx did not contain today's date (stub, not live data)")
	}
	if !res.judgePass {
		t.Errorf("JUDGE GATE: skills judge mean %.2f below the ≥%.2f gate", res.judgeMean, judgeSkillsGate)
	}
}

func writeSkillsReport(t *testing.T, model string, res *skillsResult) {
	t.Helper()
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(&b, "# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35, #51/D-40) — %s\n\n", now)
	fmt.Fprintf(&b, "Model: `%s` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.\n\n", model)
	b.WriteString("## Reproduce\n\n```bash\ndocker compose build aura-sandbox-agent && make sandbox-up\ndocker compose up -d searxng\nset -a; . ./.env; set +a\nexport PATH=\"$HOME/.local/bin:$HOME/go/bin:$PATH\"\nexport AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=... AURA_SKILLS_DIR=...\nexport SEARXNG_URL=http://127.0.0.1:18080/search\ngo test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/\n```\n\n")

	b.WriteString("## Hard floor (artifact-not-reply ground truth, D-35 #51/D-40)\n\n")
	b.WriteString("| Signal | Target | Observed | Pass |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Natural prompt (no skill/install hint) | true | %v | %v |\n", res.promptNatural, res.promptNatural)
	fmt.Fprintf(&b, "| self-install `npx skills add` (structured args) | ran | %v | %v |\n", res.selfInstall, res.selfInstall)
	fmt.Fprintf(&b, "| self-install targeted anthropics/skills | true | %v | %v |\n", res.installTarget, res.installTarget)
	fmt.Fprintf(&b, "| self-install carried --skill xlsx | true | %v | %v |\n", res.installSel, res.installSel)
	fmt.Fprintf(&b, "| .xlsx produced FRESH in workspace | newer-than-start | %v | %v |\n", res.xlsxFresh, res.xlsxExists)
	fmt.Fprintf(&b, "| .xlsx re-opens via openpyxl | opens | %v | %v |\n", res.xlsxOpens, res.xlsxOpens)
	fmt.Fprintf(&b, "| .xlsx contains today's date | present | %v | %v |\n", res.xlsxToday, res.xlsxToday)
	fmt.Fprintf(&b, "| Artifact path | — | %s | — |\n", res.xlsxPath)
	fmt.Fprintf(&b, "| Action-aware tool calls | — | %v | — |\n", oneLine(strings.Join(res.toolCalls, " → ")))

	b.WriteString("\n## Judge rubric (≥90% average gate over 2 dims, D-35 #51/D-40)\n\n")
	b.WriteString("| Dimension | Score /5 |\n|---|---|\n")
	for _, d := range []dimension{dimCapabilityGapRecognition, dimSkillOutputQuality} {
		if s, ok := res.judgeScores[d]; ok {
			fmt.Fprintf(&b, "| %s | %d |\n", d, s)
		}
	}
	fmt.Fprintf(&b, "\nSkills-judge mean (capability-gap / output-quality): **%.2f** (gate ≥%.2f) → %v\n",
		res.judgeMean, judgeSkillsGate, res.judgePass)

	b.WriteString("\n## Notes\n\n")
	for _, n := range res.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	verdict := "PASS"
	if !skillsHardFloorPass(res) || !res.judgePass {
		verdict = "FAIL (a dual-gate signal below threshold — see table)"
	}
	fmt.Fprintf(&b, "\n## Overall verdict: %s\n", verdict)

	if err := os.WriteFile(skillsReportPath, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write skills report %s: %v", skillsReportPath, err)
	}
	t.Logf("skills scored report written to %s", skillsReportPath)
	t.Logf("skills E2E: selfInstall=%v target=%v selector=%v xlsx(fresh=%v opens=%v today=%v) judgeMean=%.2f goroutines=%d",
		res.selfInstall, res.installTarget, res.installSel, res.xlsxFresh, res.xlsxOpens, res.xlsxToday, res.judgeMean, runtime.NumGoroutine())
}

// skillsHardFloorPass reports whether every D-35 hard-floor signal cleared (the report
// verdict + an internal consistency check). It mirrors enforceSkills's predicate list
// without the t.Errorf side effects.
func skillsHardFloorPass(res *skillsResult) bool {
	return res.promptNatural && res.selfInstall && res.installTarget && res.installSel &&
		res.xlsxExists && res.xlsxOpens && res.xlsxToday
}
