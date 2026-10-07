package agent

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

const (
	// teacherMargin: below it the seed bank's verdict is uncertain and the teacher is asked.
	// Calibrated on the 58-case gate on 2026-10-06 (prd.md §6), where seeds plus the teacher
	// below it scored 58/58; not independently validated (spec, "Constants").
	teacherMargin = 0.075
	// recallTimeout bounds the whole recall: client acquisition, space checks, the embedding
	// and every pool query. Proposed, not measured (spec, "Constants").
	recallTimeout = 500 * time.Millisecond
	// preloadMax is how many remembered deferred tools load before round 1: the most a tool
	// turn used in the lab VM's small sample. Every one adds its schema to the request.
	preloadMax = 3
)

// turnRead is what one reading saw, for its log line and the frozen evaluation: the seed
// verdict and the teacher's answer behind the decision, the label and the tool turn recall
// offered, and why recall offered nothing when it did not.
type turnRead struct {
	seedTier        prompt.ReasoningTier
	seedMargin      float64
	seedOK          bool
	teacherTier     prompt.ReasoningTier
	teacher         teacherOutcome
	label           RecalledTurn
	toolTurn        RecalledTurn
	preloaded       []string
	recallMiss      string
	recallDuration  time.Duration
	seedDuration    time.Duration
	teacherDuration time.Duration
}

// readTurn runs once per turn, before the first request: it decides the effort and preloads
// the deferred tools similar past turns ran (spec 2026-10-06, "Effort decision" and "Tool
// preload"). The first matching row of the spec's table wins:
//
//	composer effort > standalone greeting > compatible label (user, then teacher) >
//	seeds with margin >= teacherMargin > teacher > seeds; static low when nothing answers.
func (a *LlmAgent) readTurn(ctx context.Context) (TurnDecision, turnRead) {
	user := prompt.LastGenuineUserContent(a.history)
	decision := TurnDecision{RouteKey: routeKey(a.cfg), PolicyVersion: turnPolicyVersion}
	var read turnRead
	target := prompt.IsReasoningTarget(a.cfg.Provider, a.cfg.BaseURL)
	adaptive := a.cfg.AdaptiveReasoning && target
	greeting := a.turnReading.Standalone && prompt.IsTrivialGreeting(user)

	var recall TurnRecall
	if !greeting {
		started := time.Now()
		recall, read.recallMiss = a.recallTurns(ctx, user, decision.RouteKey, adaptive && a.reasoningOverride == "")
		read.recallDuration = time.Since(started)
		read.preloaded, read.toolTurn = a.preloadTools(recall.ToolTurns)
	}
	switch {
	case a.reasoningOverride != "":
		decision.EffortSource, decision.EffortRequested = EffortSourceUser, a.reasoningOverride
		if target {
			decision.Effort = a.cfg.ClampReasoningEffort(a.reasoningOverride)
		}
	case !adaptive:
		// This route takes no adaptive effort: nothing to decide, and the context key and
		// any preloaded tools still count.
	case greeting:
		decision.decide(a.cfg, llm.ReasoningEffortNone, EffortSourceGreeting)
	default:
		a.decideAdaptive(ctx, user, recall, &decision, &read)
	}
	a.logTurnRead(decision, read)
	recordTurnDecision(decision.EffortSource)
	return decision, read
}

func (d *TurnDecision) decide(cfg llm.Config, requested llm.ReasoningEffort, source string) {
	d.EffortRequested, d.Effort, d.EffortSource = requested, cfg.ClampReasoningEffort(requested), source
}

func (a *LlmAgent) decideAdaptive(ctx context.Context, user string, recall TurnRecall, d *TurnDecision, read *turnRead) {
	if label, ok := reusableLabel(recall); ok {
		read.label = label
		d.decide(a.cfg, llm.ReasoningEffort(label.RequestedEffort), EffortSourceMemory)
		d.OriginRef = label.SourceRef
		return
	}
	if read.recallMiss == "" {
		// Memory answered, and no row it returned is a label this turn may reuse.
		read.recallMiss = "no_compatible_label"
	}
	if strings.TrimSpace(user) == "" {
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	if a.classifier == nil {
		if tier, ok := a.teach(ctx, user, read); ok {
			d.decide(a.cfg, tier.Effort(), EffortSourceTeacher)
			return
		}
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	started := time.Now()
	verdict, ok := a.classifier.Classify(ctx, user)
	read.seedDuration = time.Since(started)
	if !ok {
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	read.seedTier, read.seedMargin, read.seedOK = verdict.Tier, verdict.Margin, true
	if verdict.Margin >= teacherMargin {
		d.decide(a.cfg, verdict.Tier.Effort(), EffortSourceSeeds)
		return
	}
	if tier, ok := a.teach(ctx, user, read); ok {
		d.decide(a.cfg, tier.Effort(), EffortSourceTeacher)
		return
	}
	d.decide(a.cfg, verdict.Tier.Effort(), EffortSourceSeeds)
}

func (a *LlmAgent) teach(ctx context.Context, user string, read *turnRead) (prompt.ReasoningTier, bool) {
	started := time.Now()
	tier, outcome := a.askTeacher(ctx, user)
	read.teacherTier, read.teacher, read.teacherDuration = tier, outcome, time.Since(started)
	return tier, outcome == teacherSuccess
}

// reusableLabel is the nearest user label, otherwise the nearest teacher label. A remembered
// effort that is not on the ladder is a miss, never a guess.
func reusableLabel(recall TurnRecall) (RecalledTurn, bool) {
	for _, pool := range [][]RecalledTurn{recall.UserLabels, recall.TeacherLabels} {
		for _, turn := range pool {
			if llm.ReasoningEffort(turn.RequestedEffort).Known() {
				return turn, true
			}
		}
	}
	return RecalledTurn{}, false
}

// recallTurns asks memory once, bounded by recallTimeout. A failure is one warning and the
// turn is read without memory; it never fails the turn.
func (a *LlmAgent) recallTurns(ctx context.Context, user, route string, labels bool) (TurnRecall, string) {
	reading := a.turnReading
	switch {
	case reading.Recaller == nil:
		return TurnRecall{}, "no_memory"
	case reading.ContextKey == "":
		return TurnRecall{}, "context_ineligible"
	case strings.TrimSpace(user) == "":
		return TurnRecall{}, "no_text"
	}
	ctx, cancel := context.WithTimeout(ctx, recallTimeout)
	defer cancel()
	recall, err := reading.Recaller.RecallTurns(ctx, TurnRecallRequest{
		Text: user, ContextKey: reading.ContextKey, RouteKey: route, PolicyVersion: turnPolicyVersion,
		SourceRef: reading.SourceRef, DeferredTools: a.deferredToolNames(), IncludeLabels: labels,
	})
	if err != nil {
		slog.Warn("turn recall failed; reading the turn without memory",
			"thread_id", redact.Line(a.sessionID), "err", redact.Line(err.Error()))
		return TurnRecall{}, "recall_error"
	}
	return recall, ""
}

// preloadTools adds the remembered tools of the nearest tool turn that still has any to the
// promoted set, the same set a tool_search result promotes into. It loads schemas only: no
// remembered call is executed and no past argument or authorization is reused.
func (a *LlmAgent) preloadTools(turns []RecalledTurn) ([]string, RecalledTurn) {
	for _, turn := range turns {
		names := a.preloadable(turn.Tools)
		if len(names) == 0 {
			continue
		}
		for _, name := range names {
			a.activated[name] = struct{}{}
		}
		return names, turn
	}
	return nil, RecalledTurn{}
}

// preloadable keeps the names still registered as deferred tools, without tool_search, sorted
// for a deterministic round-1 tools array, and at most preloadMax of them.
func (a *LlmAgent) preloadable(names []string) []string {
	if a.registry == nil {
		return nil
	}
	var out []string
	for _, name := range names {
		if name == searchTool || slices.Contains(out, name) {
			continue
		}
		if tool, ok := a.registry.Get(name); ok && tool.Spec().Deferred {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out[:min(len(out), preloadMax)]
}

func (a *LlmAgent) deferredToolNames() []string {
	if a.registry == nil {
		return nil
	}
	var names []string
	for _, tool := range a.registry.All() {
		if spec := tool.Spec(); spec.Deferred && spec.Name != searchTool {
			names = append(names, spec.Name)
		}
	}
	slices.Sort(names)
	return names
}

// logTurnRead is the one line per turn the spec's Observability section names. The label and
// the tool turn can be different past turns, so each has its own origin and distance.
func (a *LlmAgent) logTurnRead(d TurnDecision, read turnRead) {
	slog.Info("adaptive reasoning: turn read",
		"thread_id", redact.Line(a.sessionID),
		"source", d.EffortSource, "requested", string(d.EffortRequested), "effort", string(d.Effort),
		"seed_tier", string(read.seedTier), "seed_margin", read.seedMargin,
		"teacher", string(read.teacher), "teacher_tier", string(read.teacherTier),
		"label_origin", redact.Line(read.label.SourceRef), "label_distance", read.label.Distance,
		"tool_turn_origin", redact.Line(read.toolTurn.SourceRef), "tool_turn_distance", read.toolTurn.Distance,
		"recall_miss", read.recallMiss, "recall_ms", read.recallDuration.Milliseconds(),
		"seed_ms", read.seedDuration.Milliseconds(), "teacher_ms", read.teacherDuration.Milliseconds(),
		"preloaded", read.preloaded, "route", d.RouteKey, "policy", d.PolicyVersion)
}
