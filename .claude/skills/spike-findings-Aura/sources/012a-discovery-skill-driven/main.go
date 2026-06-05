// Spike 012a/012b: discovery-skill-driven vs discovery-tool-driven — does find-skills
// style SKILL CONTENT (messages[1] always-block) + a terminal drive DeepSeek-V4 from a
// natural capability-gap prompt through the FULL self-extension loop (find → add →
// use → artifact) as reliably as the hand-built skill action=catalog flow
// (amendment #49, 6 iterations)?
//
// skill-driven runs go end-to-end under the 2026-06-05 no-ceremony directive
// (self-install in the sandbox, artifact ground-truthed via docker exec find);
// tool-driven baseline runs stop at the shipped flow's install-approval endpoint.
// The capture is ACTION-AWARE (skill action + sandbox command parsed from tool args),
// fixing the observability gap in the shipped eval harness (toolNames records only
// "skill", making its catalog→install subsequence assertion structurally false).
//
// Run (paid, OPENROUTER_API_KEY-gated):
//
//	set -a; . ./.env; set +a
//	export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=...
//	go run ./.planning/spikes/012a-discovery-skill-driven -variant both -n 3
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/sandboxagent"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/chetto1983/aura/internal/web"
	"github.com/google/uuid"
)

// naturalPrompt is the EXACT D-35 North-Star prompt from scenarios_skills.go — no
// "skill"/"install"/"catalog" word; the discovery routing must be the model's own.
const naturalPrompt = "Fammi un file Excel con il mercato di Yahoo Finance di oggi. " +
	"Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi."

const maxHops = 6

// alwaysBlockHeader mirrors skills/messages.go (unexported there) — the messages[1]
// wire shape production uses for always:true skill bodies.
const alwaysBlockHeader = "Active skill instructions (always-on):\n\n"

const skillMDPath = ".planning/spikes/012a-discovery-skill-driven/find-skills-aura/SKILL.md"

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

// runMetrics is one run's scorecard. Endpoint + evidence come from STRUCTURED tool
// args, the pause payload, and an in-sandbox artifact probe — never from prose
// (probe-must-verify-artifact-not-reply).
//
// Success criteria per variant (operator directive 2026-06-05: NO install-approval
// ceremony — Aura self-installs in the sandbox like Claude Code; supersedes D-03 for
// in-sandbox installs, PRD amendment pending):
//   - skill-driven: ran `npx skills add` targeting anthropics/skills AND a fresh .xlsx
//     exists in the sandbox workspace at the end (full loop: find → add → use → artifact).
//   - tool-driven: reached the legacy install-approval endpoint (the shipped flow's
//     contract; kept for the baseline comparison).
type runMetrics struct {
	variant                        string
	success                        bool
	selfInstall                    bool // ran `npx skills add` (the directive's WANTED path for B)
	installTarget                  bool // the add targeted anthropics/skills
	artifactFresh                  bool // a .xlsx newer than run start exists in /workspace (ground truth)
	handCoded                      bool // terminal turn with no discovery engagement at all
	hops                           int
	toolCalls                      []string // action-aware: "skill:catalog(q=...)", "sandbox_exec(npx skills find ...)"
	discoveryEvidence              []string
	endpointQuestion               string
	wallMS                         float64
	promptTok, complTok, cachedTok int
	notes                          []string
}

func main() {
	variant := flag.String("variant", "both", "skill | tool | both")
	n := flag.Int("n", 3, "runs per variant")
	flag.Parse()

	if os.Getenv("OPENROUTER_API_KEY") == "" {
		logf("SETUP", "FATAL: OPENROUTER_API_KEY unset — this is a paid live spike")
		os.Exit(2)
	}

	baseCfg, err := llm.Load()
	if err != nil {
		logf("SETUP", "FATAL: llm.Load: %v", err)
		os.Exit(2)
	}
	client := openai_compat.New(*baseCfg)
	appCfg := config.LoadDB()
	logf("SETUP", "model=%s sandbox=%s", baseCfg.Model, appCfg.SandboxAgent.BaseURL)

	var all []runMetrics
	if *variant == "skill" || *variant == "both" {
		reg := buildSkillDrivenRegistry(appCfg)
		block := alwaysBlockHeader + mustSkillBody()
		for i := range *n {
			m := driveRun(client, *baseCfg, appCfg, reg, "skill-driven", block, i)
			all = append(all, m)
			reportRun(m, i)
		}
	}
	if *variant == "tool" || *variant == "both" {
		reg, stub := buildToolDrivenRegistry(appCfg)
		for i := range *n {
			stub.calls = nil
			m := driveRun(client, *baseCfg, appCfg, reg, "tool-driven", "", i)
			// A's endpoint can also be proven by the captured install args.
			for _, c := range stub.calls {
				if strings.Contains(c, "anthropics/skills") {
					m.discoveryEvidence = append(m.discoveryEvidence, "installer-args:"+c)
				}
			}
			all = append(all, m)
			reportRun(m, i)
		}
	}

	summarize(all, *n)
}

// mustSkillBody loads the adapted find-skills SKILL.md and strips its frontmatter —
// the body becomes the messages[1] always-block (the proposed end-state shape).
func mustSkillBody() string {
	raw, err := os.ReadFile(skillMDPath)
	if err != nil {
		logf("SETUP", "FATAL: read %s: %v (run from the repo root)", skillMDPath, err)
		os.Exit(2)
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	parts := strings.SplitN(s, "---\n", 3)
	if len(parts) == 3 {
		return strings.TrimSpace(parts[2])
	}
	return strings.TrimSpace(s)
}

// buildSkillDrivenRegistry is variant B: the proposed architecture — NO skill tool at
// all; discovery teaching rides in messages[1]; the terminal (sandbox_exec) is the
// transport; ask_user is the approval endpoint.
func buildSkillDrivenRegistry(cfg *config.Config) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{})
	webEngine := web.NewClient(cfg)
	reg.Register(&tools.WebSearch{Engine: webEngine})
	reg.Register(&tools.WebFetch{Engine: webEngine})
	reg.Register(&tools.SandboxExec{Runner: sandboxagent.New(cfg.SandboxAgent)})
	return reg
}

// buildToolDrivenRegistry is variant A: the shipped architecture — the live skill tool
// (real loader + real catalog client) with a STUB installer (clone mechanics are
// 004b-proven; the measurement endpoint is the approval pause, which the stub still
// triggers through the real actionInstall gate path).
func buildToolDrivenRegistry(cfg *config.Config) (*tools.Registry, *stubInstaller) {
	reg := buildSkillDrivenRegistry(cfg) // same base surface

	skillsDir, err := os.MkdirTemp("", "spike012-skills-")
	if err != nil {
		logf("SETUP", "FATAL: scratch skills dir: %v", err)
		os.Exit(2)
	}
	if err := skills.MaterializeBuiltins(skillsDir); err != nil {
		logf("SETUP", "WARN: materialize builtins: %v", err)
	}
	loader := skills.NewLoader(skills.Config{Roots: []string{skillsDir}, BodyCapBytes: cfg.SkillBodyCapBytes})
	catalog := skills.NewCatalogClient(skills.CatalogConfig{
		BaseURL:    cfg.SkillCatalogURL,
		TimeoutSec: cfg.SkillInstallTimeoutSec,
		Disabled:   cfg.SkillCatalogDisabled,
	}, nil)
	stub := &stubInstaller{}
	reg.Register(&tools.SkillTool{
		Loader:    &spikeLoader{loader: loader, manCap: cfg.SkillManifestCapBytes},
		Writer:    spikeWriter{},
		Installer: stub,
		Catalog:   &spikeCatalog{client: catalog},
	})
	return reg, stub
}

// driveRun executes one natural-prompt run to the install-approval endpoint (or a
// terminal/hop-capped failure), mirroring the eval driveInstallLoop pause/resume shape.
func driveRun(client llm.Client, cfg llm.Config, appCfg *config.Config,
	reg *tools.Registry, variant, alwaysBlock string, runIdx int,
) runMetrics {
	m := runMetrics{variant: variant}
	history := []llm.Message{}
	if alwaysBlock != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: alwaysBlock})
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: naturalPrompt})
	sessionID := uuid.NewString()
	start := time.Now()

	for hop := 0; hop < maxHops; hop++ {
		m.hops = hop + 1
		paused, ai := driveTurn(client, cfg, appCfg, reg, history, sessionID, &m)
		if !paused {
			break // terminal turn — score below
		}
		if variant == "tool-driven" && isInstallEndpoint(ai.Question) {
			// Legacy baseline endpoint: the shipped flow's approval contract.
			m.success = true
			m.endpointQuestion = oneLine(ai.Question)
			break
		}
		// Any other pause — answer generically and continue (the operator stands in;
		// under the no-ceremony directive a skill-driven ask is itself a finding).
		m.notes = append(m.notes, "pause: "+oneLine(ai.Question))
		askCall := agenttest.MakeToolCall(ai.ToolCallID, "ask_user",
			mustJSON(map[string]any{"question": ai.Question, "kind": ai.Kind}))
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall}},
			llm.Message{Role: llm.RoleTool, ToolCallID: ai.ToolCallID,
				Content: "Procedi pure in autonomia: dati di oggi da Yahoo Finance, nessun'altra preferenza. Non chiedere conferme."},
		)
	}
	m.wallMS = float64(time.Since(start).Microseconds()) / 1000.0

	if variant == "skill-driven" {
		m.artifactFresh = freshXlsxInSandbox(start)
		m.success = m.selfInstall && m.installTarget && m.artifactFresh
		if !m.success {
			m.handCoded = len(m.discoveryEvidence) == 0
		}
	} else if !m.success {
		m.handCoded = len(m.discoveryEvidence) == 0
		m.notes = append(m.notes, "terminal turn without an install-approval endpoint")
	}
	return m
}

// freshXlsxInSandbox is the artifact ground truth: a .xlsx newer than the run start
// anywhere under /workspace in the live sandbox (docker exec — the harness-side
// read-back, independent of the model's prose).
func freshXlsxInSandbox(start time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stamp := start.UTC().Format("2006-01-02 15:04:05")
	out, err := exec.CommandContext(ctx, "docker", "exec", "aura-sandbox-agent", "sh", "-c",
		fmt.Sprintf("find /workspace -name '*.xlsx' -newermt '%s' 2>/dev/null | head -5", stamp)).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), ".xlsx")
}

// driveTurn runs one LlmAgent turn over the live registry, capturing tool calls
// ACTION-AWARE and stopping on pause/terminal. Returns the pause payload if suspended.
func driveTurn(client llm.Client, cfg llm.Config, appCfg *config.Config,
	reg *tools.Registry, history []llm.Message, sessionID string, m *runMetrics,
) (bool, *agent.AwaitingInput) {
	runDir, _ := os.MkdirTemp("", "spike012-run-")
	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        cfg,
		Registry:   reg,
		PreviewCap: appCfg.ToolPreviewCap,
		RunDir:     runDir,
		SessionID:  sessionID,
		UserTurns:  history,
	})
	bud, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		m.notes = append(m.notes, "NewBudget: "+err.Error())
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ic := agent.InvocationContext{Ctx: ctx, Agent: la, RequestID: uuid.New(), Branch: "root", Budget: bud}

	for ev, runErr := range la.Run(ic) {
		if runErr != nil {
			m.notes = append(m.notes, "run error: "+runErr.Error())
			return false, nil
		}
		if ev == nil {
			continue
		}
		if ev.Actions.Escalate {
			m.notes = append(m.notes, "budget escalation")
			return false, nil
		}
		if ev.Actions.AwaitingInput != nil {
			m.toolCalls = append(m.toolCalls, "ask_user")
			return true, ev.Actions.AwaitingInput
		}
		if ev.LLMResponse == nil {
			continue
		}
		resp := ev.LLMResponse
		switch {
		case len(resp.ToolCalls) > 0 && resp.ToolCalls[0].Function.Name != "text_response":
			for i := range resp.ToolCalls {
				classifyCall(m, resp.ToolCalls[i].Function.Name, resp.ToolCalls[i].Function.Arguments)
			}
		case resp.FinishReason != "":
			if d := ev.Actions.StateDelta; d != nil {
				m.promptTok += anyInt(d["prompt_tokens"])
				m.complTok += anyInt(d["completion_tokens"])
				m.cachedTok += anyInt(d["cache_hit_tokens"])
			}
			return false, nil
		}
	}
	return false, nil
}

// classifyCall records one tool call action-aware and flags discovery/self-install
// evidence off the STRUCTURED arguments.
func classifyCall(m *runMetrics, name, rawArgs string) {
	switch name {
	case "skill":
		var a struct {
			Action, Query, Repo, Name string
		}
		_ = json.Unmarshal([]byte(rawArgs), &a)
		m.toolCalls = append(m.toolCalls, fmt.Sprintf("skill:%s(q=%s repo=%s name=%s)", a.Action, a.Query, a.Repo, a.Name))
		if a.Action == "catalog" {
			m.discoveryEvidence = append(m.discoveryEvidence, "catalog:"+a.Query)
		}
	case "sandbox_exec":
		var a struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		_ = json.Unmarshal([]byte(rawArgs), &a)
		cmd := strings.TrimSpace(a.Command + " " + strings.Join(a.Args, " "))
		m.toolCalls = append(m.toolCalls, "sandbox_exec("+oneLine(cmd)+")")
		flat := strings.Join(strings.Fields(cmd), " ")
		if strings.Contains(flat, "skills find") || (strings.Contains(flat, "skills add") && strings.Contains(flat, "--list")) {
			m.discoveryEvidence = append(m.discoveryEvidence, "terminal:"+oneLine(cmd))
		} else if strings.Contains(flat, "skills add") {
			// Self-install — the WANTED path under the 2026-06-05 no-ceremony directive.
			m.selfInstall = true
			if strings.Contains(flat, "anthropics/skills") {
				m.installTarget = true
			}
			m.discoveryEvidence = append(m.discoveryEvidence, "install:"+oneLine(cmd))
		}
	default:
		m.toolCalls = append(m.toolCalls, name)
	}
}

// isInstallEndpoint detects the approval pause that names the North-Star target. The
// pause question is the structured gate payload (A: renderInstallGate; B: the model's
// ask_user) — the ONE surface both variants share.
func isInstallEndpoint(q string) bool {
	low := strings.ToLower(q)
	return strings.Contains(low, "anthropics/skills") && strings.Contains(low, "xlsx")
}

func reportRun(m runMetrics, i int) {
	status := "FAIL"
	if m.success {
		status = "PASS"
	}
	logf(strings.ToUpper(m.variant), "run %d: %s hops=%d wall=%.0fms tokens(p/c/cached)=%d/%d/%d selfInstall=%v target=%v artifactFresh=%v handCoded=%v",
		i+1, status, m.hops, m.wallMS, m.promptTok, m.complTok, m.cachedTok, m.selfInstall, m.installTarget, m.artifactFresh, m.handCoded)
	logf(strings.ToUpper(m.variant), "run %d calls: %s", i+1, strings.Join(m.toolCalls, " → "))
	for _, e := range m.discoveryEvidence {
		logf(strings.ToUpper(m.variant), "run %d discovery: %s", i+1, e)
	}
	if m.endpointQuestion != "" {
		logf(strings.ToUpper(m.variant), "run %d endpoint: %s", i+1, m.endpointQuestion)
	}
	for _, n := range m.notes {
		logf(strings.ToUpper(m.variant), "run %d note: %s", i+1, n)
	}
}

func summarize(all []runMetrics, n int) {
	counts := map[string][2]int{} // variant -> [success, total]
	for _, m := range all {
		c := counts[m.variant]
		if m.success {
			c[0]++
		}
		c[1]++
		counts[m.variant] = c
	}
	for v, c := range counts {
		logf("RESULT", "%s: %d/%d runs succeeded", v, c[0], c[1])
	}
	skillC := counts["skill-driven"]
	if skillC[1] > 0 && skillC[0]*3 >= skillC[1]*2 {
		logf("SUMMARY", "VALIDATED — skill-driven self-extension (find → add → use → artifact) works autonomously")
		return
	}
	if skillC[1] == 0 {
		logf("SUMMARY", "BASELINE-ONLY run (no skill-driven runs requested)")
		return
	}
	logf("SUMMARY", "INVALIDATED — skill-driven success %d/%d (need >=2/3)", skillC[0], skillC[1])
	os.Exit(1)
}

// ---- variant-A seams (mirror internal/eval/skills_adapters_cot_eval.go) ----

type spikeLoader struct {
	loader *skills.Loader
	manCap int
}

func (a *spikeLoader) List() []tools.SkillMeta {
	loaded := a.loader.List()
	out := make([]tools.SkillMeta, 0, len(loaded))
	for _, s := range loaded {
		out = append(out, tools.SkillMeta{Name: s.Name, Description: s.Description})
	}
	return out
}

func (a *spikeLoader) Body(name string) (string, bool) {
	s, ok := a.loader.Get(name)
	if !ok {
		return "", false
	}
	return s.Body, true
}

func (a *spikeLoader) ManifestDescription() string {
	return skills.RenderManifest(a.loader.List(), a.manCap)
}

func (a *spikeLoader) Snippet(name string) (string, string, string, bool) {
	s, found := a.loader.Get(name)
	if !found || s.Type != skills.TypeSnippet {
		return "", "", "", false
	}
	path, interp, err := skills.SnippetInvocation(name, s.Language)
	if err != nil {
		return "", "", "", false
	}
	return s.Body, path, interp, true
}

type spikeCatalog struct{ client *skills.CatalogClient }

func (a *spikeCatalog) Search(ctx context.Context, query string) ([]tools.CatalogResult, error) {
	items, err := a.client.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]tools.CatalogResult, 0, len(items))
	for _, it := range items {
		name := it.Name
		if name == "" {
			name = it.SkillID
		}
		out = append(out, tools.CatalogResult{Name: name, Source: it.Source, Installs: it.Installs, SkillID: it.SkillID})
	}
	return out, nil
}

func (a *spikeCatalog) Disabled() bool { return a.client.Disabled() }

// stubInstaller records install calls and returns a summary WITHOUT cloning — the
// clone mechanics are 004b-proven; the discovery endpoint (the gate pause the real
// actionInstall raises right after Install returns) is what this spike measures.
type stubInstaller struct{ calls []string }

func (s *stubInstaller) Install(_ context.Context, repoURL, skillName string) (tools.InstallSummary, error) {
	s.calls = append(s.calls, repoURL+"@"+skillName)
	name := skillName
	if name == "" {
		name = "(first-found)"
	}
	return tools.InstallSummary{Name: name, ContentHash: "spike-stub", Tier: "Risky"}, nil
}

// spikeWriter fails loudly — write actions are out of this spike's scope.
type spikeWriter struct{}

func (spikeWriter) WriteMutation(context.Context, string, string, string, string, bool) (string, error) {
	return "", fmt.Errorf("skill writes are not wired in this spike")
}

// ---- small helpers ----

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}
