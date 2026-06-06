//go:build cot_eval

// skills_snippet_reuse_cot_eval_test.go is the 18-04 STEADY-STATE snippet-reuse gate
// (CAP-08.1). It is the measurement payoff of Phase 18: it proves that, once a reusable
// xlsx-builder snippet exists, the chat loop collapses from the 21-dispatch / ~19-roundtrip
// / 142.8s authoring run (D-03, docs/phase-18-xlsx-call-breakdown.md) to a STEADY-STATE
// reuse run of ≤6 tool dispatches under 40s — and it asserts that from the DURABLE
// aura.tool_invocations ledger (migration 0011) + the .xlsx artifact read-back, NEVER from
// r.Reply (artifact-not-reply discipline, feedback_probe_must_verify_artifact_not_reply).
//
// PITFALL 5 (never measure the authoring run): the gate PRE-SEEDS an active xlsx-builder
// snippet (Writer.SaveSnippet → Activate → materialized into the export dir) BEFORE the
// run, then measures the SINGLE reuse run as the steady state. Pre-seeding is the
// sanctioned steady-state shaper per the 18-04 plan ("pre-seed the snippet active, then
// measure ONE reuse run as the steady state").
//
// OPTION A — the PRODUCTION runner path (key_links, the wiring this gate validates): the
// scenario is driven through runner.New(deps) with deps.ToolInvocations =
// toolinvocations.New(pool), so runner_persist.go's persistToolInvocation fires per
// dispatched tool EXACTLY as in production — the ledger rows the assertion reads are the
// shipped wiring, not a bypass. The reuse registry from Task 1 (buildSnippetReuseRegistry,
// the production skill tool over the same pre-seeded skills root + a live pool) is the
// runner's registry, so action=use resolves the snippet's host by-path frame (18-02 D-01)
// and the model runs it through the host shell_exec terminal.
//
// THE GROUNDED GATE METRIC (D-03 substitution, documented as a plan deviation): the plan's
// original must_have asserted "distinct request_id count ≤ ~5". D-03 EMPIRICALLY INVALIDATED
// that proxy — the runner assigns ONE request_id per USER TURN, so the distinct count is 1
// for any single-prompt run and would gate nothing. The grounded replacement (the D-03
// deliverable):
//  1. PRIMARY:  count(event_kind='end') over the run window ≤ maxSteadyStateCalls (6)
//  2. WALL:     max(ended_at) − min(started_at) < maxSteadyStateWallClock (40s)
//  3. DIAGNOSTIC (logged, NOT gated): gap-derived LLM roundtrips = end-events whose
//     started_at − lag(ended_at) > 0.5s, + 1 final reply.
//
// THIS IS A MANUAL, OPERATOR-AUTHORIZED, PAID + DB-BACKED GATE — NOT A CI JOB. It is gated
// on BOTH OPENROUTER_API_KEY (the paid live model) AND the Postgres DSN env (the ledger is
// db-backed); behind the cot_eval build tag so it never runs in default CI / Makefile
// quality. With the key unset it t.Skips with the operator invocation. The key-free
// structural slot is TestRegistrySnippetReuse_HasSkillTool (classify_cot_eval_test.go) —
// the registry-parity guard that fails CI even when this paid tier is skipped.
//
// OPERATOR INVOCATION (fills the docs/aura-quality-snapshot.md Phase-18 row):
//
//	docker compose up -d searxng                               # web tools backend (today's data)
//	set -a; . ./.env; set +a
//	export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"           # host python3 + openpyxl + yfinance (Pitfall 7)
//	export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
//	export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
//	export AURA_RUN_DIR=<inspectable scratch>; export SEARXNG_URL=http://127.0.0.1:18080/search
//	go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E -timeout 900s -v ./internal/eval/
package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reuseSnippetName is the pre-seeded xlsx-builder snippet's name (lowercase [a-z0-9-],
// SanitizeName-safe). It deliberately does NOT contain a forbidden hint word.
const reuseSnippetName = "market-xlsx-builder"

// reuseMaxHops bounds the reuse drive loop (a steady-state run should close in 1-2 turns;
// the cap is a paid-spend safety net — any pause is answered generically and the loop
// continues, mirroring driveSkillsLoop).
const reuseMaxHops = 6

// reuseGapThreshold is the inter-call gap above which an end-event is counted as preceded
// by an LLM roundtrip (D-03 diagnostic; logged, never gated).
const reuseGapThreshold = 500 * time.Millisecond

// TestSnippetReuseE2E is the steady-state ledger-asserted snippet-reuse gate (operator-run,
// paid + DB-backed). It pre-seeds an active xlsx-builder snippet, drives ONE reuse run
// through the production runner.Runner, then asserts the 2nd-run-equivalent ledger window
// (≤6 dispatches / <40s) + the fresh .xlsx — never r.Reply.
func TestSnippetReuseE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("snippet-reuse E2E: OPENROUTER_API_KEY unset — this is a MANUAL paid + DB-backed gate, NOT a CI job. " +
			"See the file header for the operator invocation (searxng + composed DSNs + .env + go test -tags 'cot_eval db_integration'). " +
			"The registry-parity surface is covered key-free by TestRegistrySnippetReuse_HasSkillTool.")
	}

	h := newReuseHarness(t)
	sc := skillsSnippetReuseScenarios()[0]
	exp := sc.skills

	// The reuse prompt MUST be natural — no snippet name, no "skill"/"use" hint: discovery
	// is the model's own (the manifest rides the skill tool's Description).
	prompt := sc.prompts[0]
	low := strings.ToLower(prompt)
	for _, w := range exp.forbiddenWords {
		if strings.Contains(low, strings.ToLower(w)) {
			t.Errorf("snippet-reuse: the prompt must be natural (no %q) — got %q", w, prompt)
		}
	}

	ctx := context.Background()
	res := &skillsResult{judgeScores: map[dimension]int{}, workspaceRoot: h.workspace}

	// Pre-seed the reusable snippet ACTIVE (Pitfall 5 steady-state shaper) so the loader's
	// manifest carries it and action=use resolves its host by-path frame.
	h.preseedSnippet(t, ctx)

	// Drive the SINGLE reuse run through the PRODUCTION runner (Option A — persistToolInvocation
	// is the source of the ledger rows the assertion reads).
	runStart := time.Now()
	convID := h.driveReuseRun(t, ctx, prompt, res)

	// STEADY-STATE ASSERTION (artifact-not-reply): read the durable ledger and gate on the
	// grounded D-03 window — end-event dispatch count + wall-clock, NEVER the prose.
	window := h.readLedgerWindow(t, ctx, convID)
	res.notes = append(res.notes, window.summary())
	t.Logf("snippet-reuse steady-state ledger window: %s", window.summary())

	if window.endEvents > exp.maxSteadyStateCalls {
		t.Errorf("STEADY-STATE FLOOR: reuse run dispatched %d tools (event_kind='end') > the grounded budget %d (D-03 ≤6) — "+
			"the snippet reuse did not collapse the loop; per-tool: %v", window.endEvents, exp.maxSteadyStateCalls, window.tools)
	}
	if window.wallClock >= exp.maxSteadyStateWallClock {
		t.Errorf("STEADY-STATE FLOOR: reuse run wall-clock %s ≥ the grounded budget %s (D-03 <40s)",
			window.wallClock.Round(time.Millisecond), exp.maxSteadyStateWallClock)
	}

	// ARTIFACT ASSERTION (artifact-not-reply): the produced .xlsx exists FRESH, opens via
	// openpyxl, and contains today's date.
	verifyXlsxArtifact(t, exp, h.workspace, runStart, res)
	if !res.xlsxExists {
		t.Errorf("HARD FLOOR: no FRESH .xlsx in the host run workspace after the reuse run (artifact-not-reply / stale)")
	}
	if !res.xlsxOpens {
		t.Errorf("HARD FLOOR: the reuse-produced .xlsx did not re-open via openpyxl")
	}
	if !res.xlsxToday {
		t.Errorf("HARD FLOOR: the reuse-produced .xlsx did not contain today's date (stub, not live data)")
	}

	writeReuseSnapshotRow(t, h.cfg.Model, window, res)
}

// reuseHarness bundles the live-wired Runner + the concrete Stores + the pre-seeded skills
// root so the gate can pre-seed the snippet, drive the production loop, and read the ledger
// directly (DB ground truth).
type reuseHarness struct {
	r          *runner.Runner
	pool       *pgxpool.Pool
	cfg        llm.Config
	appCfg     *config.Config
	workspace  string // inspectable host run dir (the produced .xlsx lands here)
	skillsRoot string // active/pending/archived tree the loader + writer share
	exportDir  string // AURA_SKILL_EXPORT_DIR equivalent — the host by-path materialization target
	writer     *skills.Writer
}

// newReuseHarness migrates the DB through head, opens an aura_app pool, and wires a Runner
// over the REAL Stores + a REAL openai_compat client + the snippet-reuse registry (Task 1).
// It mirrors internal/runner/live_e2e_test.go's newLiveHarness, adapted for the eval pkg.
func newReuseHarness(t *testing.T) *reuseHarness {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := reuseEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := reuseEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := reuseEnvOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := "postgres://aura:" + pwd + "@" + host + ":" + port + "/aura?sslmode=disable"

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

	baseCfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	client := openai_compat.New(*baseCfg)

	appCfg := config.LoadDB()
	workspace := newInspectableWorkspace(t)
	// The skills root + export dir are t.TempDir-rooted (the snippet is pre-seeded fresh
	// each run, no cross-run state). The loader scans skillsRoot; Activate materializes into
	// exportDir; the host by-path frame points at exportDir/<name>/<name>.py.
	skillsRoot := t.TempDir()
	exportDir := t.TempDir()

	reg := buildSnippetReuseRegistry(appCfg, workspace, skillsRoot, exportDir, pool)

	// The always-block renders the snippet manifest exactly as production does (D-07), so
	// the model sees the pre-seeded snippet in messages[1] each turn.
	loader := skills.NewLoader(skills.Config{
		Roots:        []string{skillsRoot},
		BodyCapBytes: appCfg.SkillBodyCapBytes,
		Blocklist:    appCfg.SkillInjectionBlocklist,
	})
	alwaysBlock := func() string {
		block, present := skills.RenderAlwaysBlock(loader.List())
		if !present {
			return ""
		}
		return block
	}

	runDir := t.TempDir()
	convStore := conversations.New(pool, conversations.Config{RunDir: runDir, TurnCapBytes: 65536})
	r := runner.New(runner.Deps{
		Conv:            convStore,
		Pause:           askuser.New(pool),
		Identity:        identity.New(pool),
		CacheMetrics:    cachemetrics.New(pool),
		ToolInvocations: toolinvocations.New(pool),
		Client:          client,
		Registry:        reg,
		LLM:             *baseCfg,
		RunDir:          runDir,
		PreviewCap:      appCfg.ToolPreviewCap,
		Workspace:       filepath.ToSlash(workspace),
		AlwaysBlock:     alwaysBlock,
		TitleTimeout:    30 * time.Second,
		StopTimeout:     45 * time.Second,
	})

	writer := skills.NewWriter(skills.WriterConfig{
		Pool:         pool,
		PendingDir:   filepath.Join(skillsRoot, "pending"),
		ActiveDir:    skillsRoot,
		ExportDir:    exportDir,
		ArchiveDir:   filepath.Join(skillsRoot, "archived"),
		Blocklist:    appCfg.SkillInjectionBlocklist,
		BodyCapBytes: appCfg.SkillBodyCapBytes,
	})

	return &reuseHarness{
		r: r, pool: pool, cfg: *baseCfg, appCfg: appCfg,
		workspace: workspace, skillsRoot: skillsRoot, exportDir: exportDir, writer: writer,
	}
}

// preseedSnippet saves + activates the reusable xlsx-builder snippet so it is ACTIVE
// (manifest-visible + materialized for host by-path use) before the reuse run (Pitfall 5).
func (h *reuseHarness) preseedSnippet(t *testing.T, ctx context.Context) {
	t.Helper()
	actor := skills.AuditActor{ActorID: "operator"}
	if _, err := h.writer.SaveSnippet(ctx, reuseSnippetName, "python", reuseSnippetCode(),
		skills.Frontmatter{
			Name:         reuseSnippetName,
			Description:  "Build an .xlsx of today's market: fetches live Yahoo Finance quotes (yfinance) for the US indices + mega-caps and writes Mercato_Yahoo_<date>.xlsx into the current working directory.",
			NeedsNetwork: true,
		}, actor); err != nil {
		t.Fatalf("preseed: SaveSnippet: %v", err)
	}
	if err := h.writer.Activate(ctx, reuseSnippetName, skills.ApprovalCLI, "", actor); err != nil {
		t.Fatalf("preseed: Activate: %v", err)
	}
	hostPath, _, err := skills.SnippetHostInvocation(reuseSnippetName, "python", h.exportDir)
	if err != nil {
		t.Fatalf("preseed: resolve host path: %v", err)
	}
	if _, statErr := os.Stat(hostPath); statErr != nil {
		t.Fatalf("preseed: materialized snippet not on disk at %s: %v", hostPath, statErr)
	}
	t.Logf("snippet-reuse: pre-seeded ACTIVE snippet %q materialized at %s", reuseSnippetName, hostPath)
}

// driveReuseRun mints a conversation, drives the natural reuse prompt through the
// production runner.Turn loop (so persistToolInvocation writes the ledger), and returns the
// conversation id (the ledger key). Any ask_user pause is answered generically and the loop
// continues; the run's prose is captured for the report (NEVER asserted on).
func (h *reuseHarness) driveReuseRun(t *testing.T, ctx context.Context, prompt string, res *skillsResult) string {
	t.Helper()
	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := h.r.NewConversationWithID(ctx, convID); err != nil {
		t.Fatalf("NewConversationWithID: %v", err)
	}
	t.Cleanup(func() {
		_ = h.r.Stop(context.Background(), convID)
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", convID)
	})

	msg := prompt
	for hop := 0; hop < reuseMaxHops; hop++ {
		var lastErr error
		paused := false
		var prose string
		for ev, err := range h.r.Turn(ctx, convID, ptrOrNil(msg, hop)) {
			if err != nil {
				lastErr = err
				break
			}
			if ev == nil {
				continue
			}
			if ev.Actions.AwaitingInput != nil {
				paused = true
			}
			if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
				prose = ev.LLMResponse.Content
			}
		}
		if lastErr != nil {
			res.notes = append(res.notes, "reuse run error hop "+itoa(hop)+": "+oneLine(lastErr.Error()))
			t.Fatalf("reuse run hop %d: %v", hop, lastErr)
		}
		if !paused {
			res.notes = append(res.notes, "reuse final prose: "+oneLine(prose))
			return convID
		}
		// A pause: answer generically via the pause store, then continue with userMsg=nil
		// (the resume path — the answer is already a RoleTool turn).
		if err := h.answerPendingGenerically(ctx, convID); err != nil {
			t.Fatalf("reuse run: answer pending: %v", err)
		}
		msg = ""
	}
	res.notes = append(res.notes, fmt.Sprintf("reuse loop hit the %d-hop cap without terminating", reuseMaxHops))
	return convID
}

// answerPendingGenerically auto-accepts every pending ask_user for the conversation with a
// generic "proceed autonomously" answer so the reuse loop is never wedged on a clarification
// pause (the operator stands in, mirroring driveSkillsLoop). It writes the resume answer
// straight through the pause store + appends the RoleTool answer turn the resume re-run
// reads (the runner's SubmitAnswer-equivalent path).
func (h *reuseHarness) answerPendingGenerically(ctx context.Context, convID string) error {
	pauseStore := askuser.New(h.pool)
	pending, err := pauseStore.ListPending(ctx, convID)
	if err != nil {
		return fmt.Errorf("list pending: %w", err)
	}
	const answer = "Procedi pure in autonomia: dati di oggi da Yahoo Finance, nessun'altra preferenza. Non chiedere conferme."
	answers := map[string]askuser.ResumeAnswer{}
	for _, p := range pending {
		answers[p.Token] = askuser.ResumeAnswer{Action: "accept", Content: answer}
	}
	if len(answers) == 0 {
		return nil
	}
	if err := pauseStore.MarkResumedBatch(ctx, answers); err != nil {
		return fmt.Errorf("mark resumed: %w", err)
	}
	// Append the RoleTool answer turns so the resume re-run (userMsg=nil) sees them.
	convStore := conversations.New(h.pool, conversations.Config{RunDir: h.appCfg.RunDir, TurnCapBytes: 65536})
	n, err := convStore.CountTurns(ctx, convID)
	if err != nil {
		return fmt.Errorf("count turns: %w", err)
	}
	for i, p := range pending {
		if err := convStore.AppendTurn(ctx, conversations.AppendTurnParams{
			ConversationID: convID,
			Seq:            n + 1 + i,
			Role:           llm.RoleTool,
			ToolCallID:     p.ToolCallID,
			Content:        answer,
		}); err != nil {
			return fmt.Errorf("append tool answer: %w", err)
		}
	}
	return nil
}

// ledgerWindow is the steady-state measurement read from aura.tool_invocations: the
// end-event dispatch count (PRIMARY budget), the wall-clock (FLOOR), the per-tool sequence,
// and the gap-derived roundtrip estimate (DIAGNOSTIC).
type ledgerWindow struct {
	endEvents      int
	wallClock      time.Duration
	tools          []string
	roundtripsDiag int
}

func (w ledgerWindow) summary() string {
	return fmt.Sprintf("endEvents=%d wallClock=%s roundtrips(diag)=%d tools=[%s]",
		w.endEvents, w.wallClock.Round(time.Millisecond), w.roundtripsDiag, strings.Join(w.tools, " → "))
}

// readLedgerWindow reads the conversation's append-only ledger and derives the grounded
// D-03 window: count(event_kind='end') (primary), max(ended_at)−min(started_at) (wall), and
// the gap-derived roundtrip diagnostic. It is the artifact-not-reply ground truth — it reads
// the DURABLE ledger persistToolInvocation wrote, never the prose.
func (h *reuseHarness) readLedgerWindow(t *testing.T, ctx context.Context, convID string) ledgerWindow {
	t.Helper()
	events, err := toolinvocations.New(h.pool).ListByConversation(ctx, convID)
	if err != nil {
		t.Fatalf("readLedgerWindow: ListByConversation: %v", err)
	}
	ends := make([]toolinvocations.Event, 0, len(events))
	for _, e := range events {
		if e.Event == toolinvocations.EventEnd {
			ends = append(ends, e)
		}
	}
	sort.Slice(ends, func(i, j int) bool { return ends[i].Seq < ends[j].Seq })

	w := ledgerWindow{endEvents: len(ends)}
	var minStart, maxEnd time.Time
	var prevEnd time.Time
	roundtrips := 0
	for i, e := range ends {
		w.tools = append(w.tools, e.ToolName)
		if !e.StartedAt.IsZero() && (minStart.IsZero() || e.StartedAt.Before(minStart)) {
			minStart = e.StartedAt
		}
		if !e.EndedAt.IsZero() && e.EndedAt.After(maxEnd) {
			maxEnd = e.EndedAt
		}
		// A gap from the previous end to this start above the threshold marks an LLM
		// roundtrip preceding this dispatch (D-03 diagnostic; the first call always counts).
		if i == 0 || (!prevEnd.IsZero() && !e.StartedAt.IsZero() && e.StartedAt.Sub(prevEnd) > reuseGapThreshold) {
			roundtrips++
		}
		if !e.EndedAt.IsZero() {
			prevEnd = e.EndedAt
		}
	}
	if !minStart.IsZero() && !maxEnd.IsZero() && maxEnd.After(minStart) {
		w.wallClock = maxEnd.Sub(minStart)
	}
	w.roundtripsDiag = roundtrips + 1 // + the final reply turn (no tool dispatch)
	return w
}

// reuseSnippetCode is the pre-seeded python xlsx-builder. It is a REAL runnable script that
// HONORS the snippet's declared SaveSnippet description ("Build an .xlsx of today's market
// from a fetched table"): it fetches LIVE quotes via yfinance for a fixed representative set
// (the three US indices + the mega-caps), then writes the rows + an "Aggiornato al
// <YYYY-MM-DD>" date cell into <cwd>/Mercato_Yahoo_<YYYY-MM-DD>.xlsx, prints the filename, and
// exits NON-ZERO with a clear message on a fetch failure (so the model SEES a failure rather
// than silently shipping a stub). The gate measures REUSE of this vetted artifact, not
// authoring — a steady-state run is "skill action=use → host by-path exec → verify → reply".
//
// HOST PREREQUISITE (Pitfall 7, documented in the operator invocation): python3 + openpyxl +
// yfinance on the host PATH. The blocklist scan runs on THIS code at save time — it is benign
// by construction. The script runs in ~2-5s (one batched yfinance download), inside the
// steady-state budget. The date cell carries the ISO form the strict today-floor scans for, so
// the snippet's OWN output satisfies the artifact assertion (no recovery churn).
func reuseSnippetCode() string {
	return `import sys, datetime
import yfinance as yf
from openpyxl import Workbook

TICKERS = [
    ("^GSPC", "S&P 500"),
    ("^DJI", "Dow Jones"),
    ("^IXIC", "Nasdaq"),
    ("AAPL", "Apple"),
    ("MSFT", "Microsoft"),
    ("GOOGL", "Alphabet"),
    ("AMZN", "Amazon"),
    ("META", "Meta"),
    ("TSLA", "Tesla"),
    ("NVDA", "Nvidia"),
]

today = datetime.date.today().isoformat()
symbols = [t for t, _ in TICKERS]
data = yf.download(symbols, period="2d", interval="1d", group_by="ticker",
                   auto_adjust=False, progress=False, threads=True)
if data is None or data.empty:
    sys.exit("ERRORE: nessun dato restituito da Yahoo Finance")

rows = []
for sym, name in TICKERS:
    try:
        sub = data[sym].dropna()
    except Exception as e:
        print("WARN", sym, e, file=sys.stderr)
        continue
    if sub.empty:
        continue
    last = sub.iloc[-1]
    close = float(last["Close"])
    vol = int(last["Volume"]) if last["Volume"] == last["Volume"] else 0
    prev = float(sub.iloc[-2]["Close"]) if len(sub) >= 2 else 0.0
    var = (close - prev) / prev * 100.0 if prev else 0.0
    rows.append((sym, name, round(close, 2), round(var, 2), vol))

if not rows:
    sys.exit("ERRORE: nessun ticker recuperato da Yahoo Finance")

wb = Workbook()
ws = wb.active
ws.title = "Mercato Oggi"
ws["A1"] = "Aggiornato al %s" % today
ws.append([])
ws.append(["Ticker", "Nome", "Prezzo", "Var %", "Volume"])
for r in rows:
    ws.append(list(r))
out = "Mercato_Yahoo_%s.xlsx" % today
wb.save(out)
print("WROTE", out, "(%d tickers)" % len(rows))
`
}

// writeReuseSnapshotRow appends the observed steady-state numbers to the Phase-18 detail in
// docs/aura-quality-snapshot.md (docs/, never /tmp). The operator confirms the row at the
// checkpoint; the IMPLEMENTATION state is the structural tier green + these live numbers.
func writeReuseSnapshotRow(t *testing.T, model string, w ledgerWindow, res *skillsResult) {
	t.Helper()
	t.Logf("snippet-reuse E2E (model %s): %s; xlsx(fresh=%v opens=%v today=%v path=%s)",
		model, w.summary(), res.xlsxExists, res.xlsxOpens, res.xlsxToday, res.xlsxPath)
	t.Logf("OPERATOR: copy endEvents=%d / wallClock=%s into the docs/aura-quality-snapshot.md Phase-18 row (replace pending-operator-run).",
		w.endEvents, w.wallClock.Round(time.Millisecond))
}

// --- small helpers (no I/O, no network) ---

// reuseEnvOrSkip mirrors live_e2e_test.go's envOrSkipLive: skip locally / t.Fatal under $CI
// when a required DSN var is unset (no-skip-as-green — a skipped db tier must not pass green).
func reuseEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("snippet-reuse E2E requires %s, but it is unset under CI — "+
				"a skipped db tier must not pass as green; wire it in the paid CI lane", key)
		}
		t.Skipf("snippet-reuse E2E requires %s; set it and re-run (e.g. via .env + the composed DSN)", key)
	}
	return v
}

// ptrOrNil returns &msg for the first hop (the fresh user turn) and nil thereafter (the
// resume path — userMsg=nil re-runs over the rehydrated history). An empty msg on hop 0
// would be a bug, but the caller always seeds a non-empty prompt.
func ptrOrNil(msg string, hop int) *string {
	if hop == 0 {
		return &msg
	}
	return nil
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
