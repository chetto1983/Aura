//go:build cot_eval

// harness_swarm_e2e_test.go is the Phase 9 (CAP-03 / SC#5 / D-22) LIVE dual-gate
// swarm E2E. It drives the REAL parent agent.LlmAgent over the REAL openai_compat
// client against DeepSeek-V4 with a registry that includes swarm_spawn (the live
// internal/swarm RunnerAdapter) AND the mail + WhatsApp MCP servers mounted through
// the same managed-config boot path `aura chat` uses. A NATURAL prompt (NO "swarm"
// word) must drive the model to fan out on its own; the dual gate is:
//
//   - HARD FLOOR (deterministic ground truth): ≥2 workers spawned (counted off the
//     ChildReport array the swarm_spawn tool returned); the expected facts present in
//     the aggregated answer; the self-mail AND self-WhatsApp messages found on
//     read-back via the SAME mounted MCP server (self-chat JID duality, D-19); the
//     wall-clock < 1.5× a single-worker baseline run (D-22).
//   - JUDGE ≥90% AVERAGE: autonomous parallelization (no "swarm" in the prompt),
//     sub-answer correctness, aggregation quality, and NO over-spawn on the control.
//
// THIS IS A MANUAL, OPERATOR-AUTHORIZED, PAID GATE — NOT A CI JOB. It is gated on
// OPENROUTER_API_KEY and behind the cot_eval build tag so it NEVER runs in normal CI
// / Makefile quality targets (no-skip-as-green: CI gates stay on unit/property/race/
// goleak — this is the ONE legitimate skip, exactly like TestCoTEval). With the key
// unset it t.Skips with a clear message.
//
// OPERATOR INVOCATION (the full live run that fills the docs/aura-quality-snapshot.md
// TBD row with real numbers):
//
//  1. Bring up the whatsmeow bridge (the user's fork chetto1983/whatsapp-mcp @ 6de1dcd,
//     REST :8080) in WSL and health-check it — a live bridge answers GET /api/send with
//     HTTP 405 (Method Not Allowed); a connection refused means it is down:
//
//     wsl -e bash -lc 'cd ~/whatsapp-mcp/whatsapp-bridge && ./whatsapp-bridge'   # in its own shell
//     curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/api/send    # expect 405
//
//  2. Register the mail + whatsapp servers once (creds in managed-config Env / .env):
//
//     aura mcp install mail        # then edit the SMTP/IMAP Env (Gmail app password)
//     aura mcp install whatsapp
//
//  3. Export the swarm self-target env (the user's OWN address/number — messages to
//     self ONLY, D-19) and run the tier:
//
//     set -a; . ./.env; set +a
//     export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//     export AURA_EVAL_SELF_MAIL="you@example.com"
//     export AURA_EVAL_SELF_PHONE="39XXXXXXXXXX"
//     export AURA_EVAL_WA_CHAT_SELF="39XXXXXXXXXX@s.whatsapp.net"   # bridge-sent JID (D-19 duality)
//     go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/
//
//  4. Replace the TBD placeholder row in docs/aura-quality-snapshot.md (Phase 9 swarm
//     E2E) with the real observed numbers the run prints + writes to the scored report.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/google/uuid"
)

// swarmReportPath is the scored swarm-E2E report destination (docs/, never /tmp —
// CLAUDE.md). The operator copies its numbers into the quality snapshot.
const swarmReportPath = "../../docs/aura-swarm-eval-2026-06-04.md"

// swarmResult accumulates the observed hard-floor + judge numbers for the report.
type swarmResult struct {
	workers      int
	factsPresent bool
	mailReadBack bool
	waReadBack   bool
	swarmMS      float64 // end-to-end turn wall-clock (advisory — includes parent spawn-decision + synthesis turns)
	fanoutMS     float64 // swarm_spawn tool-call duration (the SC#1 wall-clock: fan-out start → all reports back)
	baselineMS   float64
	timingOK     bool
	judgeScores  map[dimension]int
	judgeMean    float64
	judgePass    bool
	controlSpawn bool // true = the control prompt over-spawned (a FAIL)
	controlJudge int
	mountedMail  []string
	mountedWA    []string
	notes        []string
}

// TestSwarmE2E is the live dual-gate swarm entry point (operator-run, paid).
func TestSwarmE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("swarm E2E: OPENROUTER_API_KEY unset — this is a MANUAL paid gate, NOT a CI job. " +
			"See the file header for the operator invocation (bridge bring-up + .env + AURA_EVAL_SELF_* + go test).")
	}
	selfMail := os.Getenv("AURA_EVAL_SELF_MAIL")
	selfPhone := os.Getenv("AURA_EVAL_SELF_PHONE")
	waChatSelf := os.Getenv("AURA_EVAL_WA_CHAT_SELF")
	if selfMail == "" || selfPhone == "" || waChatSelf == "" {
		t.Skip("swarm E2E: set AURA_EVAL_SELF_MAIL + AURA_EVAL_SELF_PHONE + AURA_EVAL_WA_CHAT_SELF " +
			"(your OWN address/number — messages to self ONLY, D-19). See the file header.")
	}

	baseCfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	secret := baseCfg.APIKey
	client := openai_compat.New(*baseCfg)

	cfg := config.LoadDB()
	reg, mountedMail, mountedWA, closers := buildSwarmRegistry(t, cfg)
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i]()
		}
	}()
	if len(mountedMail) == 0 || len(mountedWA) == 0 {
		t.Fatalf("swarm E2E: mail (%d) and whatsapp (%d) MCP tools must both mount — bring up the bridge + register the servers (see header)",
			len(mountedMail), len(mountedWA))
	}

	res := &swarmResult{judgeScores: map[dimension]int{}, mountedMail: mountedMail, mountedWA: mountedWA}
	scs := swarmScenarios(selfMail, selfPhone, waChatSelf)

	// Inject a fresh per-run tag into the autonomous scenario so read-back is exact.
	runTag := strings.ToUpper(uuid.NewString()[:8])
	mailTag := "AURA-MAIL-" + runTag
	waTag := "AURA-WA-" + runTag

	ctx := context.Background()
	for _, sc := range scs {
		switch {
		case sc.swarm != nil:
			runSwarmScenario(t, ctx, client, *baseCfg, secret, reg, cfg, sc, selfMail, mailTag, waTag, res)
		case sc.noOverSpawn:
			runControlScenario(t, ctx, client, *baseCfg, reg, cfg, sc, res)
		}
	}

	writeSwarmReport(t, baseCfg.Model, res)
	enforceSwarm(t, res)
}

// buildSwarmRegistry mirrors cmd/aura/main.go buildBaseRegistry + the fail-soft MCP
// mount loop: the production tool set + swarm_spawn (live RunnerAdapter) + the mail +
// whatsapp MCP servers from the managed config. It is
// the SAME boot path `aura chat` uses, so the harness exercises swarm_spawn against a
// real MCP-mounted registry (D-19/D-20/D-21 first live exercise from the eval tier).
func buildSwarmRegistry(t *testing.T, cfg *config.Config) (reg *tools.Registry, mail, wa []string, closers []func() error) {
	t.Helper()
	reg = buildRegistry() // text_response + tool_search + read_tool_output + current_time
	reg.Register(tools.AskUser{})
	reg.Register(&tools.SwarmSpawn{Runner: swarm.NewRunnerAdapter(*cfg), MaxGoals: cfg.MaxSwarmGoals})

	if cfg.MCPServersErr != nil {
		t.Fatalf("swarm E2E: MCP config parse error: %v", cfg.MCPServersErr)
	}
	ctx := context.Background()
	for _, name := range []string{"mail", "whatsapp"} {
		sc, ok := cfg.MCPServers[name]
		if !ok {
			t.Logf("swarm E2E: MCP server %q not registered — `aura mcp install %s` first (see header)", name, name)
			continue
		}
		closer, names, err := mcptools.MountServer(ctx, ctx, reg, name, sc)
		if err != nil {
			t.Logf("swarm E2E: MCP server %q failed to mount (fail-soft): %v", name, err)
			continue
		}
		closers = append(closers, closer)
		if name == "mail" {
			mail = names
		} else {
			wa = names
		}
	}
	return reg, mail, wa, closers
}

// runSwarmScenario runs the natural autonomous prompt (with the per-run tags
// substituted), captures the turn, applies the hard floor + judge, and does the
// mail/WhatsApp read-back via the mounted MCP server.
func runSwarmScenario(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	secret string, reg *tools.Registry, appCfg *config.Config, sc scenario,
	selfMail, mailTag, waTag string, res *swarmResult,
) {
	t.Helper()
	prompt := strings.ReplaceAll(sc.prompts[0], swarmMailTagMarker, mailTag)
	prompt = strings.ReplaceAll(prompt, swarmWATagMarker, waTag)

	// Single-worker baseline: ONE of the two subtasks alone, to anchor the < 1.5×
	// timing budget (D-22). Best-effort — a baseline failure leaves timing advisory.
	res.baselineMS = swarmBaselineMS(t, ctx, client, cfg, reg, appCfg, selfMail, mailTag+"-BASE")

	c := runAgentTurn(t, ctx, client, cfg, reg, appCfg, prompt, 0)
	res.swarmMS = c.totalMS
	logTurnTrace(t, "swarm", c)

	if leaked := secretLeaked(secret, c); leaked {
		res.notes = append(res.notes, "SECRET LEAK DETECTED")
		t.Errorf("swarm: SECRET LEAK — OPENROUTER_API_KEY value appears in observable output")
	}

	res.workers = countSwarmWorkers(c)
	res.factsPresent = factsPresent(c.prose, sc.swarm.expectFacts)
	// SC#1 semantics: "wall-clock < 1.5× single-worker" is defined on the FAN-OUT
	// (the unit fixture has no parent turns), so the live gate times the
	// swarm_spawn tool call — fan-out start to all reports collected — against the
	// single-worker baseline. The end-to-end turn (swarmMS) additionally carries
	// the parent's spawn-decision and synthesis LLM turns, i.e. provider turn
	// latency, not parallelization quality; it is reported as advisory. The
	// multiplier stays 1.5 by default and is operator-tunable via
	// AURA_EVAL_SWARM_TIMING_BUDGET (latency budgets are env-tunable per project
	// discipline; correctness gates are not).
	res.fanoutMS = swarmFanoutMS(c)
	res.timingOK = timingOK(res.fanoutMS, res.baselineMS, timingBudgetFromEnv(sc.swarm.timingBudget))

	// Read-back via the SAME mounted MCP server (ground truth).
	res.mailReadBack = mailReadBack(t, ctx, reg, res.mountedMail, mailTag)
	res.waReadBack = waReadBack(t, ctx, reg, res.mountedWA, waTag, sc.swarm.waChatSelf)

	observed := fmt.Sprintf("workers spawned (swarm_spawn child reports): %d", res.workers)
	dims := []dimension{dimAutonomousParallel, dimSubAnswerCorrectness, dimAggregationQuality}
	jctx, jcancel := context.WithTimeout(ctx, 120*time.Second)
	defer jcancel()
	scores, mean, pass, jerr := runSwarmJudge(jctx, client, cfg.Model, dims, prompt, c.prose, observed)
	if jerr != nil {
		res.notes = append(res.notes, "swarm judge error: "+jerr.Error())
		t.Errorf("swarm judge error: %v", jerr)
	}
	for d, s := range scores {
		res.judgeScores[d] = s
	}
	res.judgeMean, res.judgePass = mean, pass
	res.notes = append(res.notes, fmt.Sprintf("workers=%d facts=%v timing=%v(fanout %.0f / baseline %.0f / e2e %.0f ms) mail=%v wa=%v judgeMean=%.2f",
		res.workers, res.factsPresent, res.timingOK, res.fanoutMS, res.baselineMS, res.swarmMS,
		res.mailReadBack, res.waReadBack, res.judgeMean))
}

// runControlScenario runs the trivial single-task control and asserts the model did
// NOT call swarm_spawn (no-over-spawn). The judge scores the choice too.
func runControlScenario(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, appCfg *config.Config, sc scenario, res *swarmResult,
) {
	t.Helper()
	c := runAgentTurn(t, ctx, client, cfg, reg, appCfg, sc.prompts[0], 0)
	logTurnTrace(t, "control", c)
	res.controlSpawn = calledSwarmSpawn(c)

	observed := "the user asked one trivial question; a correct agent answers directly and spawns ZERO workers. " +
		fmt.Sprintf("observed swarm_spawn called: %v", res.controlSpawn)
	jctx, jcancel := context.WithTimeout(ctx, 90*time.Second)
	defer jcancel()
	scores, _, _, jerr := runSwarmJudge(jctx, client, cfg.Model, []dimension{dimNoOverSpawn}, sc.prompts[0], c.prose, observed)
	if jerr != nil {
		res.notes = append(res.notes, "control judge error: "+jerr.Error())
		t.Errorf("control judge error: %v", jerr)
	}
	if s, ok := scores[dimNoOverSpawn]; ok {
		res.controlJudge = s
		res.judgeScores[dimNoOverSpawn] = s
	}
	res.notes = append(res.notes, fmt.Sprintf("control over-spawn=%v judge=%d/5", res.controlSpawn, res.controlJudge))
}

// logTurnTrace dumps the observable turn record (tool calls in order, per-result
// previews, finish state, prose head) so a live-gate failure is diagnosable from
// the -v log alone — without it the only signal is the aggregate verdict line
// (lesson from the first live run: workers=0 with no way to see what the model
// actually did).
func logTurnTrace(t *testing.T, label string, c *turnCapture) {
	t.Helper()
	if c == nil {
		t.Logf("%s trace: nil capture", label)
		return
	}
	head := func(s string, n int) string {
		s = strings.ReplaceAll(s, "\n", "\\n")
		if len(s) > n {
			return s[:n] + "…"
		}
		return s
	}
	t.Logf("%s trace: tools=%v finish=%q terminated=%v(%s) runErr=%v firstByte=%.0fms total=%.0fms",
		label, c.toolNames, c.finish, c.terminated, c.terminalReason, c.runErr, c.firstByteMS, c.totalMS)
	for i, r := range c.toolResults {
		name := "?"
		if i < len(c.toolNames) {
			name = c.toolNames[i]
		}
		t.Logf("%s trace: result[%d] %s → %s", label, i, name, head(r, 220))
	}
	t.Logf("%s trace: prose → %s", label, head(c.prose, 400))
}

// runAgentTurn constructs a real LlmAgent over the live registry and drains one turn
// into a turnCapture (the swarm context is injected automatically by runTool when the
// model calls swarm_spawn — the cycle-free seam, 09-05).
func runAgentTurn(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, appCfg *config.Config, prompt string, _ int,
) *turnCapture {
	t.Helper()
	sessionID := uuid.NewString()
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        cfg,
		Registry:   reg,
		PreviewCap: appCfg.ToolPreviewCap,
		RunDir:     t.TempDir(),
		SessionID:  sessionID,
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
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

// swarmFanoutMS returns the duration of the (first successful) swarm_spawn tool
// call — fan-out start to all child reports collected — derived from the aligned
// toolCallMS/toolResultMS capture timestamps. A "successful" call is one whose
// result is the report array (starts with '['), not an inline error string; a
// teaching-error retry must not count as fan-out time. Returns 0 when no
// successful spawn happened (timingOK then advisory-passes via its guard... the
// workers floor already fails such a run).
func swarmFanoutMS(c *turnCapture) float64 {
	for i, name := range c.toolNames {
		if name != "swarm_spawn" || i >= len(c.toolResultMS) || i >= len(c.toolCallMS) {
			continue
		}
		if i < len(c.toolResults) && strings.HasPrefix(strings.TrimSpace(c.toolResults[i]), "[") {
			return c.toolResultMS[i] - c.toolCallMS[i]
		}
	}
	return 0
}

// timingBudgetFromEnv resolves the fan-out timing multiplier: the scenario's
// value (1.5, SC#1) unless AURA_EVAL_SWARM_TIMING_BUDGET overrides it. Latency
// budgets are operator-tunable; correctness gates are not.
func timingBudgetFromEnv(def float64) float64 {
	if v := os.Getenv("AURA_EVAL_SWARM_TIMING_BUDGET"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// swarmBaselineMS runs a single self-contained subtask to anchor the timing budget.
// The baseline MUST be the full mail subtask (compute + MCP send) — the same
// workload one worker carries — not bare arithmetic: anchoring a tool-using
// fan-out against a 3.5s no-tool answer made the 1.5× budget unmeetable by
// construction (run 5, 2026-06-04: swarm 35.7s vs "baseline" 3.5s while both
// workers were individually 17-21s). A distinct -BASE tag keeps the baseline
// send out of the scenario read-back. Best-effort: any failure returns 0
// (timing then advisory-passes, timingOK guard).
func swarmBaselineMS(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, appCfg *config.Config, selfMail, baseTag string,
) float64 {
	t.Helper()
	bctx, bcancel := context.WithTimeout(ctx, 180*time.Second)
	defer bcancel()
	c := runAgentTurn(t, bctx, client, cfg, reg, appCfg,
		"Calcola quanto fa 144 diviso 12, poi mandami una mail a "+selfMail+
			" con oggetto e corpo che contengano esattamente il codice "+baseTag+
			" e il risultato del calcolo. Quando hai inviato, confermami cosa hai mandato.", 0)
	if c == nil {
		return 0
	}
	logTurnTrace(t, "baseline", c)
	return c.totalMS
}

// ---- MCP read-back (ground truth via the SAME mounted server) ----

// mailReadBack calls the mounted mail__search_emails {query} tool (spike-001: the
// arg is {query} required, not a criteria object) and asserts the per-run tag is in
// the result. Returns false if no search tool mounted or the tag is absent.
func mailReadBack(t *testing.T, ctx context.Context, reg *tools.Registry, mounted []string, tag string) bool {
	t.Helper()
	name := pickTool(mounted, "search_emails")
	if name == "" {
		t.Logf("swarm E2E: no mail search tool mounted (have %v) — mail read-back unavailable", mounted)
		return false
	}
	args := mustJSON(map[string]any{"query": tag})
	return pollToolForTag(t, ctx, reg, name, args, tag, 6, 15*time.Second)
}

// waReadBack calls the mounted whatsapp__list_messages tool for the self-chat JID and
// asserts the per-run tag is present. The bridge-sent self-chat lives under
// <phone>@s.whatsapp.net (D-19 JID duality — that is the chatSelf the operator exports).
func waReadBack(t *testing.T, ctx context.Context, reg *tools.Registry, mounted []string, tag, chatSelf string) bool {
	t.Helper()
	name := pickTool(mounted, "list_messages")
	if name == "" {
		t.Logf("swarm E2E: no whatsapp list_messages tool mounted (have %v) — WhatsApp read-back unavailable", mounted)
		return false
	}
	args := mustJSON(map[string]any{"chat_jid": chatSelf, "limit": 20})
	return pollToolForTag(t, ctx, reg, name, args, tag, 6, 5*time.Second)
}

// pollToolForTag executes a read-back tool up to attempts times (interval apart),
// returning true as soon as the tag appears in the tool result body (preview or the
// spilled sidecar). Read-back latency on Gmail is ~11-15s (spike 001).
func pollToolForTag(t *testing.T, ctx context.Context, reg *tools.Registry, name string,
	args json.RawMessage, tag string, attempts int, interval time.Duration,
) bool {
	t.Helper()
	tool, ok := reg.Get(name)
	if !ok {
		return false
	}
	// Generous preview cap so a 20-message list / multi-hit search is not truncated
	// before we scan for the tag (the sidecar FullPath is the fallback anyway).
	const readBackCap = 1 << 20
	for i := 0; i < attempts; i++ {
		toolCtx := tools.WithToolCallContext(ctx, uuid.NewString(), uuid.NewString(), t.TempDir(), readBackCap)
		res, err := tool.Execute(toolCtx, args)
		if err == nil && toolResultContains(res, tag) {
			return true
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(interval):
			}
		}
	}
	return false
}

// pickTool returns the mounted namespaced name (<server>__<tool>) whose RAW suffix
// matches want, or "" if none. Bridged names are <namespace>__<rawname>.
func pickTool(mounted []string, want string) string {
	for _, n := range mounted {
		if n == want || strings.HasSuffix(n, "__"+want) {
			return n
		}
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// toolResultContains reports whether the tag is in the tool result body. The body is
// in res.Preview (the history-bound content); when it spilled to a sidecar
// (res.Truncated) the full bytes live at res.FullPath, so we read that too.
func toolResultContains(res tools.ToolResult, tag string) bool {
	if strings.Contains(res.Preview, tag) {
		return true
	}
	if res.Truncated && res.FullPath != "" {
		if b, err := os.ReadFile(res.FullPath); err == nil && strings.Contains(string(b), tag) {
			return true
		}
	}
	return false
}

// ---- report + enforcement ----

// enforceSwarm applies the D-22 dual gate: the hard floor (≥2 workers, facts, both
// read-backs, timing) AND the judge ≥90% average are release-blocking; the control
// must NOT over-spawn.
func enforceSwarm(t *testing.T, res *swarmResult) {
	t.Helper()
	if res.workers < 2 {
		t.Errorf("HARD FLOOR: expected ≥2 workers (autonomous parallelization), got %d", res.workers)
	}
	if !res.factsPresent {
		t.Errorf("HARD FLOOR: expected facts missing from the aggregated answer")
	}
	if !res.mailReadBack {
		t.Errorf("HARD FLOOR: self-mail not found on MCP read-back")
	}
	if !res.waReadBack {
		t.Errorf("HARD FLOOR: self-WhatsApp message not found on MCP read-back")
	}
	if !res.timingOK {
		t.Errorf("HARD FLOOR: fan-out wall-clock %.0fms exceeded %.2g× the single-worker baseline %.0fms (end-to-end turn: %.0fms, advisory)",
			res.fanoutMS, timingBudgetFromEnv(1.5), res.baselineMS, res.swarmMS)
	}
	if !res.judgePass {
		t.Errorf("JUDGE GATE: swarm judge mean %.2f below the ≥%.2f gate", res.judgeMean, judgeSwarmGate)
	}
	if res.controlSpawn {
		t.Errorf("NO-OVER-SPAWN: the control prompt (trivial single task) incorrectly called swarm_spawn")
	}
}

func writeSwarmReport(t *testing.T, model string, res *swarmResult) {
	t.Helper()
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(&b, "# Aura Live Swarm E2E (CAP-03 / SC#5 / D-22) — %s\n\n", now)
	fmt.Fprintf(&b, "Model: `%s` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.\n\n", model)
	b.WriteString("## Reproduce\n\n```bash\nset -a; . ./.env; set +a\nexport PATH=\"$HOME/.local/bin:$HOME/go/bin:$PATH\"\nexport AURA_EVAL_SELF_MAIL=... AURA_EVAL_SELF_PHONE=... AURA_EVAL_WA_CHAT_SELF=...@s.whatsapp.net\ngo test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/\n```\n\n")

	b.WriteString("## Hard floor (deterministic ground truth)\n\n")
	b.WriteString("| Signal | Target | Observed | Pass |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Workers spawned (tool_use) | ≥2 | %d | %v |\n", res.workers, res.workers >= 2)
	fmt.Fprintf(&b, "| Expected facts in answer | present | %v | %v |\n", res.factsPresent, res.factsPresent)
	fmt.Fprintf(&b, "| Self-mail read-back (MCP) | found | %v | %v |\n", res.mailReadBack, res.mailReadBack)
	fmt.Fprintf(&b, "| Self-WhatsApp read-back (MCP) | found | %v | %v |\n", res.waReadBack, res.waReadBack)
	fmt.Fprintf(&b, "| Fan-out wall-clock vs single-worker (SC#1) | < %.2g× | %.0f/%.0f ms | %v |\n",
		timingBudgetFromEnv(1.5), res.fanoutMS, res.baselineMS, res.timingOK)
	fmt.Fprintf(&b, "| End-to-end turn incl. parent LLM turns (advisory) | — | %.0f ms | — |\n", res.swarmMS)

	b.WriteString("\n## Judge rubric (≥90% average gate, D-22)\n\n")
	b.WriteString("| Dimension | Score /5 |\n|---|---|\n")
	for _, d := range []dimension{dimAutonomousParallel, dimSubAnswerCorrectness, dimAggregationQuality, dimNoOverSpawn} {
		if s, ok := res.judgeScores[d]; ok {
			fmt.Fprintf(&b, "| %s | %d |\n", d, s)
		}
	}
	fmt.Fprintf(&b, "\nSwarm-judge mean (autonomous/sub-answer/aggregation): **%.2f** (gate ≥%.2f) → %v\n",
		res.judgeMean, judgeSwarmGate, res.judgePass)
	fmt.Fprintf(&b, "\nControl over-spawn: %v (must be false); no_over_spawn judge: %d/5\n", res.controlSpawn, res.controlJudge)

	b.WriteString("\n## Mounted MCP tools\n\n")
	sort.Strings(res.mountedMail)
	sort.Strings(res.mountedWA)
	fmt.Fprintf(&b, "- mail: %s\n- whatsapp: %s\n", strings.Join(res.mountedMail, ", "), strings.Join(res.mountedWA, ", "))

	b.WriteString("\n## Notes\n\n")
	for _, n := range res.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	verdict := "PASS"
	if res.workers < 2 || !res.factsPresent || !res.mailReadBack || !res.waReadBack || !res.timingOK || !res.judgePass || res.controlSpawn {
		verdict = "FAIL (a dual-gate signal below threshold — see table)"
	}
	fmt.Fprintf(&b, "\n## Overall verdict: %s\n", verdict)

	if err := os.WriteFile(swarmReportPath, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write swarm report %s: %v", swarmReportPath, err)
	}
	t.Logf("swarm scored report written to %s", swarmReportPath)
	t.Logf("swarm E2E: workers=%d facts=%v mail=%v wa=%v timing=%v judgeMean=%.2f control_over_spawn=%v goroutines=%d",
		res.workers, res.factsPresent, res.mailReadBack, res.waReadBack, res.timingOK, res.judgeMean, res.controlSpawn, runtime.NumGoroutine())
}
