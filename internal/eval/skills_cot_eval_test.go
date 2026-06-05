//go:build cot_eval

// skills_cot_eval.go is the Phase 11 (CAP-07 / CAP-08 / D-35) LIVE dual-gate xlsx
// North-Star E2E. It drives the REAL parent agent.LlmAgent over the REAL openai_compat
// client against DeepSeek-V4 with a registry that includes the live `skill` tool (the
// real internal/skills Loader/Writer/Installer/CatalogClient), the shipped web tools
// (web_search → SearXNG + web_fetch), ask_user, and sandbox_exec (the live
// sandbox-agent). A NATURAL prompt (NO "skill"/"install" word) must drive the model to
// recognise the capability gap, discover anthropics/skills/xlsx on skills.sh, ask to
// install it, install + use it, pull today's data through the web tools, and produce a
// real .xlsx — all on its own. The dual gate (D-35) is:
//
//   - HARD FLOOR (artifact-not-reply ground truth): the tool_use sequence
//     catalog → ask_user → install → sandbox_exec appears IN ORDER (subsequence); the
//     produced .xlsx EXISTS in the sandbox workspace, OPENS (re-read via a sandbox_exec
//     openpyxl read-back), and CONTAINS today's date — NEVER an assertion on r.Reply.
//   - JUDGE ≥90% AVERAGE over: capability-gap recognition, install prudence, output
//     quality (D-35).
//
// THIS IS A MANUAL, OPERATOR-AUTHORIZED, PAID GATE — NOT A CI JOB. It is gated on
// OPENROUTER_API_KEY and behind the cot_eval build tag so it NEVER runs in normal CI /
// Makefile quality targets (no-skip-as-green: CI gates the unit/db_integration/
// sandbox_integration tiers — this paid LLM tier is the ONE legitimate skip, exactly
// like TestCoTEval/TestSwarmE2E). With the key unset it t.Skips with a clear message.
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
//  2. Migrate Postgres through 0010 and bring up SearXNG (web tools backend):
//
//     make db-migrate
//     docker compose up -d searxng                  # or the socat bridge per memory
//
//  3. Export the live env + run the tier:
//
//     set -a; . ./.env; set +a
//     export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//     export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468
//     export AURA_SANDBOX_AGENT_TOKEN=<the .env token>
//     export AURA_SKILL_EXPORT_DIR=<host dir mounted ro at /skills>
//     export AURA_SKILLS_DIR=<the active skills root>
//     export AURA_DB_URL=... AURA_DB_MIGRATE_URL=... POSTGRES_PASSWORD=...   # through 0010
//     export SEARXNG_URL=http://127.0.0.1:18080/search                      # or the bridge
//     go test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
//
//  4. OPEN the produced .xlsx visually (feedback_inspect_artifact_visually_not_just_pass_status)
//     and replace the Phase-11 TBD placeholder rows in docs/aura-quality-snapshot.md
//     with the observed judge %, coverage, and mutation numbers.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/sandboxagent"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/chetto1983/aura/internal/web"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// skillsReportPath is the scored xlsx-E2E report destination (docs/, never /tmp —
// CLAUDE.md). The operator copies its numbers into the quality snapshot.
const skillsReportPath = "../../docs/aura-skills-eval-2026-06-05.md"

// maxInstallResumeHops bounds the pause/resume drive loop: the model may pause once
// for install approval (and conceivably once more for a follow-up confirmation). A
// hard cap stops a model that pauses forever from running up unbounded spend.
const maxInstallResumeHops = 4

// skillsResult accumulates the observed hard-floor + judge numbers for the report.
type skillsResult struct {
	toolSeq        []string // the observed tool_use sequence across all hops (in order)
	seqOK          bool     // catalog→ask_user→install→sandbox_exec present as a subsequence
	promptNatural  bool     // the prompt contains none of the forbidden words
	installApprove bool     // an install-approval pause fired AND was approved before install
	xlsxExists     bool     // a .xlsx file is present in the workspace (ground truth)
	xlsxOpens      bool     // the .xlsx re-opened via openpyxl read-back
	xlsxToday      bool     // the re-opened .xlsx contains today's date
	xlsxPath       string   // the workspace path of the produced artifact
	judgeScores    map[dimension]int
	judgeMean      float64
	judgePass      bool
	notes          []string
}

// TestSkillsE2E is the live dual-gate xlsx North-Star entry point (operator-run, paid).
func TestSkillsE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("skills E2E: OPENROUTER_API_KEY unset — this is a MANUAL paid gate, NOT a CI job. " +
			"See the file header for the operator invocation (sandbox-up + db-migrate + searxng + .env + go test).")
	}
	sandboxURL := os.Getenv("AURA_SANDBOX_AGENT_URL")
	exportDir := os.Getenv("AURA_SKILL_EXPORT_DIR")
	skillsDir := os.Getenv("AURA_SKILLS_DIR")
	if sandboxURL == "" || exportDir == "" || skillsDir == "" {
		t.Skip("skills E2E: set AURA_SANDBOX_AGENT_URL + AURA_SKILL_EXPORT_DIR + AURA_SKILLS_DIR " +
			"(the live sandbox + the /skills mount source + the active skills root). See the file header.")
	}

	baseCfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	secret := baseCfg.APIKey
	client := openai_compat.New(*baseCfg)

	appCfg := config.LoadDB()
	reg, sandboxClient, closers := buildSkillsRegistry(t, appCfg)
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i]()
		}
	}()

	res := &skillsResult{judgeScores: map[dimension]int{}}
	scs := skillsScenarios()
	ctx := context.Background()
	for _, sc := range scs {
		if sc.skills == nil {
			continue
		}
		runSkillsScenario(t, ctx, client, *baseCfg, secret, reg, appCfg, sc, sandboxClient, res)
	}

	writeSkillsReport(t, baseCfg.Model, res)
	enforceSkills(t, res)
}

// buildSkillsRegistry mirrors cmd/aura/main.go buildBaseRegistry for the skills North
// Star: the production read tools + the live `skill` tool (loader/writer/installer/
// catalog over internal/skills) + ask_user + the web tools (web_search/web_fetch) +
// sandbox_exec (the live sandbox-agent). The adapters live HERE (the cmd/aura ones are
// in the main package the eval tier cannot import) and are thin bridges over the same
// internal/skills types the composition root uses. It also returns the raw
// sandbox-agent client so the read-back can re-open the produced .xlsx by path.
func buildSkillsRegistry(t *testing.T, cfg *config.Config) (reg *tools.Registry, sandboxClient *sandboxagent.Client, closers []func() error) {
	t.Helper()
	reg = buildRegistry() // text_response + tool_search + read_tool_output + current_time
	reg.Register(tools.AskUser{})

	// Web tools (web_search → SearXNG + web_fetch) — the scenario pulls today's data
	// through them, exactly as `aura chat` does.
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine})

	// The live sandbox-agent client (bearer-carrying, D-38) — the agent's sandbox_exec
	// AND the harness's openpyxl read-back share it.
	sandboxClient = sandboxagent.New(cfg.SandboxAgent)
	reg.Register(&tools.SandboxExec{Runner: sandboxClient})

	// The live `skill` tool wired through the eval-package adapters (the same
	// internal/skills primitives the composition root uses). find-skills is reachable
	// via the catalog; the agent must autonomously discover anthropics/skills/xlsx and
	// install it (D-35). MaterializeBuiltins seeds skill-creator so the active root scan
	// is non-empty from the first turn.
	if err := skills.MaterializeBuiltins(cfg.SkillsDir); err != nil {
		t.Logf("skills E2E: materialize builtins (non-fatal): %v", err)
	}
	loader := skills.NewLoader(skills.Config{Roots: []string{cfg.SkillsDir}, BodyCapBytes: cfg.SkillBodyCapBytes})
	pool := skillsMigratedPool(t) // through 0010 (the audit store the Writer records into)
	writer := skills.NewWriter(skills.WriterConfig{
		Pool:         pool,
		PendingDir:   cfg.SkillsDir + "/pending",
		ActiveDir:    cfg.SkillsDir,
		ExportDir:    cfg.SkillExportDir,
		ArchiveDir:   cfg.SkillsDir + "/archived",
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       writer,
		TimeoutSec:   cfg.SkillInstallTimeoutSec,
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
	catalog := skills.NewCatalogClient(skills.CatalogConfig{
		BaseURL:    cfg.SkillCatalogURL,
		TimeoutSec: cfg.SkillInstallTimeoutSec,
		Disabled:   cfg.SkillCatalogDisabled,
	}, nil)
	reg.Register(&tools.SkillTool{
		Loader:    &evalSkillLoader{loader: loader, manCap: cfg.SkillManifestCapBytes},
		Writer:    &evalSkillWriter{w: writer},
		Installer: &evalSkillInstaller{inst: installer},
		Catalog:   &evalSkillCatalog{client: catalog},
	})
	return reg, sandboxClient, closers
}

// runSkillsScenario drives the natural prompt through the pause/resume install loop,
// captures the full tool_use sequence, does the artifact read-back, and runs the judge.
func runSkillsScenario(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	secret string, reg *tools.Registry, appCfg *config.Config, sc scenario,
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
	finalProse := driveInstallLoop(t, ctx, client, cfg, reg, appCfg, prompt, sessionID, res)

	if secret != "" && strings.Contains(finalProse, secret) {
		res.notes = append(res.notes, "SECRET LEAK DETECTED")
		t.Errorf("skills: SECRET LEAK — OPENROUTER_API_KEY value appears in the final answer")
	}

	res.seqOK = isSubsequence(sc.skills.requiredSeq, res.toolSeq)

	// Artifact ground truth (artifact-not-reply, D-35): find the .xlsx in the workspace,
	// re-open it via openpyxl, assert it carries today's date. NEVER trust the prose.
	verifyXlsxArtifact(t, ctx, sandboxClient, sc.skills, res)

	observed := fmt.Sprintf(
		"tool_use sequence: %v (catalog→ask_user→install→sandbox_exec in order: %v); "+
			"install-approval pause fired+approved before install: %v; "+
			".xlsx produced: %v; .xlsx re-opened via openpyxl: %v; contains today's date: %v",
		res.toolSeq, res.seqOK, res.installApprove, res.xlsxExists, res.xlsxOpens, res.xlsxToday)

	dims := []dimension{dimCapabilityGapRecognition, dimInstallPrudence, dimSkillOutputQuality}
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
	res.notes = append(res.notes, fmt.Sprintf("seqOK=%v install_approved=%v xlsx(exists=%v opens=%v today=%v) judgeMean=%.2f",
		res.seqOK, res.installApprove, res.xlsxExists, res.xlsxOpens, res.xlsxToday, res.judgeMean))
}

// driveInstallLoop runs the agent over successive turns, auto-approving the install
// pause (the operator stands in for the human, answering "yes, install"). Each turn is
// a fresh LlmAgent over the accumulated history; when ask_user fires the loop appends
// the synthesized assistant ask_user tool_call + the approval tool-result and starts the
// next turn. It returns the final assistant prose (a turn that ended without a pause).
func driveInstallLoop(t *testing.T, ctx context.Context, client llm.Client, cfg llm.Config,
	reg *tools.Registry, appCfg *config.Config, prompt, sessionID string, res *skillsResult,
) string {
	t.Helper()
	history := []llm.Message{{Role: llm.RoleUser, Content: prompt}}
	installSeen := false
	for hop := 0; hop < maxInstallResumeHops; hop++ {
		c := runSkillsTurn(t, ctx, client, cfg, reg, appCfg, history, sessionID)
		logTurnTrace(t, fmt.Sprintf("skills-hop-%d", hop), c)
		res.toolSeq = append(res.toolSeq, c.toolNames...)

		if !c.paused || c.awaitingInput == nil {
			return c.prose // a terminal turn — done
		}

		// An install-approval pause that fires BEFORE any install tool ran is the prudent
		// path (D-35: ask before installing). Record it the first time.
		if !installSeen && !installToolSeen(res.toolSeq) {
			res.installApprove = true
		}

		// Auto-approve: append the assistant ask_user tool_call + the human's approval as
		// the tool-result, then continue (the operator stands in for the human). The
		// assistant message is the ask_user-only rewrite the agent itself produced on the
		// pause (D-A1-07 wire-correctness), reconstructed from the AwaitingInput payload.
		ai := c.awaitingInput
		askCall := agenttest.MakeToolCall(ai.ToolCallID, "ask_user",
			mustJSONString(map[string]any{"question": ai.Question, "kind": ai.Kind}))
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall}},
			llm.Message{Role: llm.RoleTool, ToolCallID: ai.ToolCallID, Content: approvalAnswer(ai)},
		)
		if installToolSeen(res.toolSeq) {
			installSeen = true
		}
	}
	res.notes = append(res.notes, fmt.Sprintf("install loop hit the %d-hop cap without a terminal turn", maxInstallResumeHops))
	return ""
}

// runSkillsTurn constructs a real LlmAgent over the live registry + the accumulated
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

// ---- helpers ----

// skillsMigratedPool mirrors the db_integration migratedPool (audit_store_integration_
// test.go) for the cot_eval tier: EnsureRoles + Migrate (through 0010) + Open an
// aura_app pool the Writer records audit rows into. The composed DSNs come from the
// SAME env the operator exports for the live run; a missing one t.Fatals (the harness
// is already past the OPENROUTER gate, so the operator opted into the paid live run and
// the DB must be up). Closed via t.Cleanup.
func skillsMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := mustEnv(t, "POSTGRES_PASSWORD")
	migrateURL := mustEnv(t, "AURA_DB_MIGRATE_URL")
	appURL := mustEnv(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// mustEnv reads a required live-run env var, failing the (already OPENROUTER-gated)
// paid run loudly when it is unset.
func mustEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("skills E2E: %s is required for the live run (the DB the audit store records into) — see the file header", key)
	}
	return v
}

// isSubsequence reports whether want appears in got in relative order (other tools may
// interleave). This is the D-35 sequence assertion: catalog→ask_user→install→
// sandbox_exec must occur in that order, not necessarily contiguously.
func isSubsequence(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// installToolSeen reports whether the install path has run (the model called the skill
// tool with action=install). The harness can only see "skill" in the tool names, so it
// treats a "skill" call after a "catalog"-bearing run as the install signal; the seqOK
// subsequence check is the authoritative install-order assertion.
func installToolSeen(seq []string) bool {
	count := 0
	for _, s := range seq {
		if s == "skill" {
			count++
		}
	}
	return count >= 2 // at least catalog + install both routed through the one skill tool
}

// approvalAnswer renders the human's approval as the resume tool-result the model
// reads. It is an unambiguous "yes, install + proceed" so the model continues to the
// install + use (the operator stands in for the human approver).
func approvalAnswer(ai *agent.AwaitingInput) string {
	return "Sì, approvo: installa la skill richiesta e poi usala per completare il compito. " +
		"(question: " + ai.Question + ")"
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ---- report + enforcement ----

// enforceSkills applies the D-35 dual gate: the hard floor (natural prompt, the
// catalog→ask_user→install→sandbox_exec sequence, the .xlsx exists/opens/contains-today)
// AND the judge ≥90% average are release-blocking.
func enforceSkills(t *testing.T, res *skillsResult) {
	t.Helper()
	if !res.promptNatural {
		t.Errorf("HARD FLOOR: the prompt was not natural (a forbidden hint word leaked in)")
	}
	if !res.seqOK {
		t.Errorf("HARD FLOOR: tool_use sequence catalog→ask_user→install→sandbox_exec not observed in order, got %v", res.toolSeq)
	}
	if !res.installApprove {
		t.Errorf("HARD FLOOR: no install-approval pause fired before the install (install prudence, D-03/D-35)")
	}
	if !res.xlsxExists {
		t.Errorf("HARD FLOOR: no .xlsx artifact found in the sandbox workspace (artifact-not-reply)")
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
	fmt.Fprintf(&b, "# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35) — %s\n\n", now)
	fmt.Fprintf(&b, "Model: `%s` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.\n\n", model)
	b.WriteString("## Reproduce\n\n```bash\ndocker compose build aura-sandbox-agent && make sandbox-up && make db-migrate\ndocker compose up -d searxng\nset -a; . ./.env; set +a\nexport PATH=\"$HOME/.local/bin:$HOME/go/bin:$PATH\"\nexport AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=... AURA_SKILL_EXPORT_DIR=... AURA_SKILLS_DIR=...\nexport AURA_DB_URL=... AURA_DB_MIGRATE_URL=... POSTGRES_PASSWORD=... SEARXNG_URL=http://127.0.0.1:18080/search\ngo test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/\n```\n\n")

	b.WriteString("## Hard floor (artifact-not-reply ground truth, D-35)\n\n")
	b.WriteString("| Signal | Target | Observed | Pass |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Natural prompt (no skill/install hint) | true | %v | %v |\n", res.promptNatural, res.promptNatural)
	fmt.Fprintf(&b, "| tool_use seq catalog→ask_user→install→sandbox_exec | in order | %v | %v |\n", res.toolSeq, res.seqOK)
	fmt.Fprintf(&b, "| Install-approval pause before install | fired+approved | %v | %v |\n", res.installApprove, res.installApprove)
	fmt.Fprintf(&b, "| .xlsx produced in workspace | exists | %v | %v |\n", res.xlsxExists, res.xlsxExists)
	fmt.Fprintf(&b, "| .xlsx re-opens via openpyxl | opens | %v | %v |\n", res.xlsxOpens, res.xlsxOpens)
	fmt.Fprintf(&b, "| .xlsx contains today's date | present | %v | %v |\n", res.xlsxToday, res.xlsxToday)
	fmt.Fprintf(&b, "| Artifact path | — | %s | — |\n", res.xlsxPath)

	b.WriteString("\n## Judge rubric (≥90% average gate, D-35)\n\n")
	b.WriteString("| Dimension | Score /5 |\n|---|---|\n")
	for _, d := range []dimension{dimCapabilityGapRecognition, dimInstallPrudence, dimSkillOutputQuality} {
		if s, ok := res.judgeScores[d]; ok {
			fmt.Fprintf(&b, "| %s | %d |\n", d, s)
		}
	}
	fmt.Fprintf(&b, "\nSkills-judge mean (capability-gap/install-prudence/output-quality): **%.2f** (gate ≥%.2f) → %v\n",
		res.judgeMean, judgeSkillsGate, res.judgePass)

	b.WriteString("\n## Notes\n\n")
	for _, n := range res.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	verdict := "PASS"
	if !res.promptNatural || !res.seqOK || !res.installApprove || !res.xlsxExists || !res.xlsxOpens || !res.xlsxToday || !res.judgePass {
		verdict = "FAIL (a dual-gate signal below threshold — see table)"
	}
	fmt.Fprintf(&b, "\n## Overall verdict: %s\n", verdict)

	if err := os.WriteFile(skillsReportPath, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write skills report %s: %v", skillsReportPath, err)
	}
	t.Logf("skills scored report written to %s", skillsReportPath)
	t.Logf("skills E2E: seqOK=%v install_approved=%v xlsx(exists=%v opens=%v today=%v) judgeMean=%.2f goroutines=%d",
		res.seqOK, res.installApprove, res.xlsxExists, res.xlsxOpens, res.xlsxToday, res.judgeMean, runtime.NumGoroutine())
}
