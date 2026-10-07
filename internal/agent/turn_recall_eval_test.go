//go:build turn_recall_eval

// The frozen turn-recall evaluation (docs/verification/turn-recall-frozen-eval.md). Paid:
// every uncertain turn asks the configured teacher once, as the runner's background worker
// does. Run only with the operator's OK.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

type evalEnv struct {
	cfg        llm.Config
	client     llm.Client
	classifier *prompt.ReasoningClassifier
}

// evalArm is one arm's decision for a reading; ask is whether it asked the background teacher.
type evalArm struct {
	effort llm.ReasoningEffort
	source string
	ask    bool
}

// evalRecord is one reading. teacher is the background teacher's outcome, "" when neither
// arm asked it.
type evalRecord struct {
	trial           int
	split           string
	turn            evalTurn
	seeds           evalArm
	baseline        evalArm
	recall          evalArm
	recallMiss      string
	teacher         string
	teacherDuration time.Duration
	labelDistance   float64
	recallDuration  time.Duration
	reading         time.Duration
}

// evalTeacherTimeout is the runner's defaultTeacherTimeout, which this package cannot import.
const evalTeacherTimeout = 30 * time.Second

func (r evalRecord) gateRow() evalGateRow {
	return evalGateRow{
		trial: r.trial, split: r.split, turnID: r.turn.ID, recallMiss: r.recallMiss,
		recallSource: r.recall.source, baselineSource: r.baseline.source,
	}
}

func (t evalTurn) accepts(effort llm.ReasoningEffort) bool {
	return slices.Contains(t.Accepted, string(effort))
}

func requireEvalEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is unset under CI", key)
		}
		t.Skipf("%s is unset; see docs/verification/turn-recall-frozen-eval.md", key)
	}
	return value
}

func newEvalEnv(t *testing.T) evalEnv {
	t.Helper()
	cfg := llm.Config{
		Provider: requireEvalEnv(t, "TURN_EVAL_LLM_PROVIDER"), BaseURL: requireEvalEnv(t, "TURN_EVAL_LLM_BASE_URL"),
		Model: requireEvalEnv(t, "TURN_EVAL_LLM_MODEL"), APIKey: requireEvalEnv(t, "TURN_EVAL_LLM_API_KEY"),
		AdaptiveReasoning: true, TotalTimeoutSec: 60, MaxTokens: 4096,
	}
	if !prompt.IsReasoningTarget(cfg.Provider, cfg.BaseURL) {
		t.Fatalf("%s at %s is not a reasoning target: nothing would be decided", cfg.Provider, cfg.BaseURL)
	}
	embed := config.LoadEmbed()
	embedder := &embeddings.Client{BaseURL: embed.BaseURL, Client: &http.Client{Timeout: 30 * time.Second}, Dimensions: embed.Dimensions}
	t.Cleanup(embedder.Client.CloseIdleConnections)
	return evalEnv{cfg: cfg, client: openai_compat.New(cfg), classifier: prompt.NewReasoningClassifier(embedder)}
}

func (e evalEnv) agent(history []llm.Message, reading TurnReading) *LlmAgent {
	return NewLlmAgent(LlmAgentConfig{
		Client: e.client, LLM: e.cfg, Registry: tools.NewRegistry(), SessionID: "turn-recall-eval",
		UserTurns: history, Classifier: e.classifier, TurnReading: reading,
	})
}

// teach asks the teacher as the runner's background worker does: the turn's client and
// model, the typed text, and the worker's bound.
func (e evalEnv) teach(user string) (prompt.ReasoningTier, string, time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), evalTeacherTimeout)
	defer cancel()
	started := time.Now()
	tier, outcome := AskTeacher(ctx, e.client, e.cfg.Model, user)
	return tier, outcome, time.Since(started)
}

// evalMemory is one trial's identity memory: a fresh database, dropped at the end.
func evalMemory(t *testing.T) *arcadedb.Client {
	t.Helper()
	base, password := requireEvalEnv(t, "ARCADEDB_URL"), os.Getenv("ARCADEDB_PASSWORD")
	user := os.Getenv("ARCADEDB_USER")
	if user == "" {
		user = "root"
	}
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: "unused", User: user, Password: password})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	database := fmt.Sprintf("aura_turn_recall_eval_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(context.Background(), database); err != nil {
		t.Fatalf("create %s: %v", database, err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop %s: %v", database, err)
		}
	})
	client, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: database, User: user, Password: password})
	if err != nil {
		t.Fatalf("memory client: %v", err)
	}
	client = client.WithEmbedder(arcadedb.NewMemoryEmbedder(config.LoadEmbed(), func() string { return "" }))
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("memory schema: %v", err)
	}
	return client
}

type evalRecaller struct {
	client   *arcadedb.Client
	identity string
}

func (r evalRecaller) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	recall, err := r.client.RecallTurns(ctx, arcadedb.TurnRecallRequest{
		IdentityID: r.identity, Text: request.Text, ContextKey: request.ContextKey, RouteKey: request.RouteKey,
		PolicyVersion: request.PolicyVersion, SourceRef: request.SourceRef,
		DeferredTools: request.DeferredTools, IncludeLabels: request.IncludeLabels,
	})
	if err != nil {
		return TurnRecall{}, err
	}
	convert := func(turns []arcadedb.RecalledTurn) []RecalledTurn {
		out := make([]RecalledTurn, len(turns))
		for index, turn := range turns {
			out[index] = RecalledTurn(turn)
		}
		return out
	}
	return TurnRecall{UserLabels: convert(recall.UserLabels), TeacherLabels: convert(recall.TeacherLabels), ToolTurns: convert(recall.ToolTurns)}, nil
}

// learn projects the turn the way the runner persists it: the user row with its decision,
// then its answer.
func learn(t *testing.T, memory *arcadedb.Client, identity, convID string, seq int, text, key string, d TurnDecision) {
	t.Helper()
	turn := func(seq int, role, content string, decision arcadedb.TurnDecision) arcadedb.ConversationTurnProjection {
		sum := sha256.Sum256([]byte(content))
		return arcadedb.ConversationTurnProjection{
			IdentityID: identity, ConversationID: convID, Seq: seq, Role: role, Content: content,
			ContentHash: hex.EncodeToString(sum[:]), OccurredAt: time.Now().UTC(),
			SourceRef: "postgres://aura/conversations/" + convID + "/turns/" + strconv.Itoa(seq), Decision: decision,
		}
	}
	projection := arcadedb.ConversationProjection{IdentityID: identity, ConversationID: convID, Turns: []arcadedb.ConversationTurnProjection{
		turn(seq, "user", text, arcadedb.TurnDecision{
			ContextKey: key, Effort: string(d.Effort), EffortRequested: string(d.EffortRequested), EffortSource: d.EffortSource,
			RouteKey: d.RouteKey, PolicyVersion: d.PolicyVersion, OriginRef: d.OriginRef,
		}),
		turn(seq+1, "assistant", "(answered)", arcadedb.TurnDecision{}),
	}}
	if err := memory.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("project %s seq %d: %v", convID, seq, err)
	}
}

func replayTrial(t *testing.T, env evalEnv, set evalSet, split string, trial int) []evalRecord {
	t.Helper()
	ctx := context.Background()
	memory := evalMemory(t)
	identity := uuid.NewString()
	var records []evalRecord
	for _, conv := range set.Conversations {
		if split != "all" && conv.Split != split {
			continue
		}
		convID := uuid.NewString()
		var prior []llm.Message
		for index, turn := range conv.Turns {
			seq := 2*index + 1
			key, standalone := "", false
			if !turn.Attachment {
				key, standalone = TurnContextKey(prior, ""), len(prior) == 0
			}
			sourceRef := "postgres://aura/conversations/" + convID + "/turns/" + strconv.Itoa(seq)
			history := append(append([]llm.Message(nil), prior...), llm.Message{Role: llm.RoleUser, Content: turn.Text})

			baseline, baseRead := env.agent(history, TurnReading{ContextKey: key, SourceRef: sourceRef, Standalone: standalone}).readTurn(ctx)
			started := time.Now()
			decision, read := env.agent(history, TurnReading{
				Recaller: evalRecaller{client: memory, identity: identity}, ContextKey: key, SourceRef: sourceRef, Standalone: standalone,
			}).readTurn(ctx)
			reading := time.Since(started)

			record := evalRecord{
				trial: trial, split: conv.Split, turn: turn,
				baseline:   evalArm{effort: baseline.EffortRequested, source: baseline.EffortSource, ask: baseline.AskTeacher},
				recall:     evalArm{effort: decision.EffortRequested, source: decision.EffortSource, ask: decision.AskTeacher},
				recallMiss: read.recallMiss, labelDistance: read.label.Distance,
				recallDuration: read.recallDuration, reading: reading,
			}
			if baseRead.seedOK {
				record.seeds = evalArm{effort: baseRead.seedTier.Effort(), source: EffortSourceSeeds}
			} else if baseline.EffortSource == EffortSourceGreeting {
				record.seeds = evalArm{effort: baseline.EffortRequested, source: EffortSourceGreeting}
			}
			// The background teacher, in time order: a successful answer is written as this
			// turn's label before the next turn is read, as the runner's worker writes it, and
			// a failure leaves the decision as it was. The label is the tier's effort before
			// the clamp, as the runner stores it; reuse clamps it again. One answer serves both
			// arms, which read the same text with the same classifier.
			learned := decision
			if baseline.AskTeacher || decision.AskTeacher {
				tier, outcome, took := env.teach(turn.Text)
				record.teacher, record.teacherDuration = outcome, took
				if decision.AskTeacher && outcome == TeacherSuccess {
					learned.EffortSource, learned.EffortRequested = EffortSourceTeacher, tier.Effort()
				}
			}
			records = append(records, record)
			learn(t, memory, identity, convID, seq, turn.Text, key, learned)
			prior = append(history, llm.Message{Role: llm.RoleAssistant, Content: "(answered)"})
		}
	}
	return records
}

func TestTurnRecallFrozenEval(t *testing.T) {
	env := newEvalEnv(t)
	set, _ := loadFrozenEvalSet(t)
	split := os.Getenv("TURN_EVAL_SPLIT")
	if split == "" {
		split = "calibration"
	}
	if split != "calibration" && split != "final" && split != "all" {
		t.Fatalf("TURN_EVAL_SPLIT = %q, want calibration, final or all", split)
	}
	trials := 3
	if raw := os.Getenv("TURN_EVAL_TRIALS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("TURN_EVAL_TRIALS = %q", raw)
		}
		trials = parsed
	}
	// The report is rendered at cleanup so a failure in a late trial still leaves the paid
	// evidence of the earlier ones.
	var records []evalRecord
	t.Cleanup(func() {
		if len(records) == 0 {
			return
		}
		report := renderEvalReport(env.cfg.Model, split, trials, records)
		if path := os.Getenv("TURN_EVAL_REPORT"); path != "" {
			if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
				t.Errorf("write the report: %v", err)
			}
		}
		t.Log("\n" + report)
	})
	for trial := 1; trial <= trials; trial++ {
		records = append(records, replayTrial(t, env, set, split, trial)...)
	}
	for _, violation := range memoryHardToNone(records) {
		t.Errorf("memory added a hard→none: trial %d, %s %q", violation.trial, violation.turn.ID, violation.turn.Text)
	}
	rows := make([]evalGateRow, len(records))
	for index, record := range records {
		rows[index] = record.gateRow()
	}
	for _, failure := range evalGateFailures(rows, split != "calibration") {
		t.Errorf("the run does not exercise memory: %s", failure)
	}
}

// memoryHardToNone is the release criterion: a final hard turn the recall arm decided from
// memory as none while the memoryless arm did not.
func memoryHardToNone(records []evalRecord) []evalRecord {
	var out []evalRecord
	for _, record := range records {
		if record.split == "final" && record.turn.hard() && record.recall.source == EffortSourceMemory &&
			record.recall.effort == llm.ReasoningEffortNone && record.baseline.effort != llm.ReasoningEffortNone {
			out = append(out, record)
		}
	}
	return out
}

func renderEvalReport(model, split string, trials int, records []evalRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Turn recall frozen evaluation — %s, split %s, %d trial(s)\n\n", model, split, trials)
	fmt.Fprintf(&b, "Generated %s by TestTurnRecallFrozenEval.\n\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("The teacher answers in the background (spec amendment 2026-10-07): it never decides the turn it " +
		"is asked about. The seeds + teacher arm is therefore the turn's seeds decision with memory empty, and the " +
		"background teacher line says how often that arm would have had the turn labelled. The recall arm's memory " +
		"holds every earlier turn with its decision, upgraded to the teacher's label when the teacher answered.\n\n" +
		"What this does not show: the replay writes a teacher label before the next turn is read, but production " +
		"reaches memory only at the next reconcile tick (about a minute). Recall-arm memory hits are therefore an " +
		"upper bound for follow-ups sent sooner than that.\n\n")
	arms := []struct {
		name string
		pick func(evalRecord) evalArm
	}{
		{"seeds", func(r evalRecord) evalArm { return r.seeds }},
		{"seeds + teacher", func(r evalRecord) evalArm { return r.baseline }},
		{"recall", func(r evalRecord) evalArm { return r.recall }},
	}
	for _, part := range []string{"calibration", "final"} {
		var subset []evalRecord
		for _, record := range records {
			if record.split == part {
				subset = append(subset, record)
			}
		}
		if len(subset) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%d readings)\n\n| Arm | Accuracy | 95%% Wilson | Hard→none |\n|---|---|---|---|\n", part, len(subset))
		for _, arm := range arms {
			correct, hardNone := 0, 0
			for _, record := range subset {
				decided := arm.pick(record)
				if record.turn.accepts(decided.effort) {
					correct++
				}
				if record.turn.hard() && decided.effort == llm.ReasoningEffortNone {
					hardNone++
				}
			}
			lo, hi := wilson(correct, len(subset))
			fmt.Fprintf(&b, "| %s | %d/%d | %.3f–%.3f | %d |\n", arm.name, correct, len(subset), lo, hi, hardNone)
		}
		b.WriteString("\n" + evalRecallDetail(subset))
	}
	return b.String()
}

func evalRecallDetail(records []evalRecord) string {
	sources, outcomes, table, misses := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	memoryHits, memoryCorrect := 0, 0
	baselineAsked, baselineLabelled, recallAsked, recallLearned := 0, 0, 0, 0
	var recallTimes, readingTimes, teacherTimes []time.Duration
	for _, record := range records {
		sources[record.recall.source]++
		misses[missLabel(record.recallMiss)]++
		if record.teacher != "" {
			outcomes[record.teacher]++
			teacherTimes = append(teacherTimes, record.teacherDuration)
		}
		answered := record.teacher == TeacherSuccess
		if record.baseline.ask {
			baselineAsked++
			if answered {
				baselineLabelled++
			}
		}
		if record.recall.ask {
			recallAsked++
			if answered {
				recallLearned++
			}
		}
		table[string(record.recall.effort)+" × "+strings.Join(record.turn.Accepted, "|")]++
		if record.recall.source == EffortSourceMemory {
			memoryHits++
			if record.turn.accepts(record.recall.effort) {
				memoryCorrect++
			}
		}
		recallTimes = append(recallTimes, record.recallDuration)
		readingTimes = append(readingTimes, record.reading)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recall arm sources: %v.\n\n", sources)
	fmt.Fprintf(&b, "Background teacher: the seeds + teacher arm asked on %d/%d readings and would have had %d labelled; "+
		"the recall arm asked on %d and learned %d labels. Outcomes: %v. Teacher latency p50 %v, p95 %v.\n\n",
		baselineAsked, len(records), baselineLabelled, recallAsked, recallLearned, outcomes,
		percentile(teacherTimes, 0.5), percentile(teacherTimes, 0.95))
	fmt.Fprintf(&b, "Recall miss reasons: %v.\n\n", misses)
	fmt.Fprintf(&b, "Memory precision: %d/%d. Recall latency p50 %v, p95 %v. Whole reading p50 %v, p95 %v.\n\n",
		memoryCorrect, memoryHits, percentile(recallTimes, 0.5), percentile(recallTimes, 0.95),
		percentile(readingTimes, 0.5), percentile(readingTimes, 0.95))
	b.WriteString("| Decided × accepted | Readings |\n|---|---|\n")
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "| %s | %d |\n", key, table[key])
	}
	b.WriteString("\n| Trial | Turn | Seeds | Seeds + teacher | Recall (source) | Miss | Label distance | Teacher |\n|---|---|---|---|---|---|---|---|\n")
	for _, record := range records {
		distance, teacher := "—", "—"
		if record.recall.source == EffortSourceMemory {
			distance = fmt.Sprintf("%.3f", record.labelDistance)
		}
		if record.teacher != "" {
			teacher = record.teacher
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s (%s) | %s | %s | %s |\n", record.trial, record.turn.ID,
			record.seeds.effort, record.baseline.effort, record.recall.effort, record.recall.source,
			missLabel(record.recallMiss), distance, teacher)
	}
	return b.String()
}

// missLabel names the empty miss: memory answered with a label.
func missLabel(miss string) string {
	if miss == "" {
		return "label"
	}
	return miss
}
