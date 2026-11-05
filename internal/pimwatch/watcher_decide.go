package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/decide"
)

const (
	// DecideBudget is the model time one tick's calls share. A mail page reserves what is left of it,
	// so the run bound must hold PageReserve + DecideBudget.
	DecideBudget = 30 * time.Second
	// decideThreshold is the probability a label needs to be applied.
	decideThreshold = 0.5
)

// Decider asks the decision model which label fits a mail, on the route it reads each tick.
type Decider interface {
	RouteReader
	Choose(ctx context.Context, route decide.Route, state string, q decide.Question) (decide.Answer, error)
}

// decideStep is the decision model's part of one tick. note says why it is off for a reason the
// ledger should name; an off route is no reason. The counts are what the summary tells.
type decideStep struct {
	on, failed, answered         bool
	note                         string
	route                        decide.Route
	question                     decide.Question
	labelIDs                     map[string]string
	budget, spent                time.Duration
	labelled, none, left, noText int
	inputTokens                  int
}

// remaining is the model time the tick may still spend, 0 once the step is off, failed or spent.
func (s *decideStep) remaining() time.Duration {
	if !s.on || s.failed {
		return 0
	}
	return max(s.budget-s.spent, 0)
}

func (s *decideStep) summary() string {
	switch {
	case s.note != "":
		return "decide: " + s.note
	case !s.on:
		return ""
	}
	line := fmt.Sprintf("decide: %d labelled, %d none, %d left, %d no text, %d input tokens", s.labelled, s.none, s.left, s.noText, s.inputTokens)
	if s.failed {
		line += " (failed)"
	}
	return line
}

// openDecide reads the route once per tick, so a route switched in the cockpit holds from the next
// tick. A route or consent it cannot read switches the step off, so the ladder never waits for it.
func (w *Watcher) openDecide(ctx context.Context, t *tick) {
	if w.Decider == nil {
		return
	}
	route, err := w.Decider.Route(ctx)
	switch {
	case err != nil:
		slog.Warn("mail watch: the decision model's route cannot be read", "error", err)
		t.decide.note = "route unreadable"
		return
	case route.Off():
		return
	}
	if withheld := w.withheld(ctx, t, route); withheld != "" {
		t.decide.note = withheld
		return
	}
	labels := t.book.DescribedLabels()
	if len(labels) == 0 {
		t.decide.note = "no described label"
		return
	}
	question, ids := mailQuestion(t.lang, labels)
	budget := w.DecideTime
	if budget <= 0 {
		budget = DecideBudget
	}
	t.decide = decideStep{on: true, route: route, question: question, labelIDs: ids, budget: budget}
}

// withheld says why a hosted route may not read the account's mail now, "" when it may. The person
// can withdraw the consent at any moment, so decide reads it again for every mail page.
func (w *Watcher) withheld(ctx context.Context, t *tick, route decide.Route) string {
	if !route.Hosted() {
		return ""
	}
	consent, err := w.Store.CloudDecide(ctx, t.IdentityID, t.AccountID)
	switch {
	case err != nil:
		slog.Warn("mail watch: the consent for the cloud decision model cannot be read", "error", err)
		return "consent unreadable"
	case !consent:
		return "no consent"
	}
	return ""
}

// decide asks the model about each mail of a page that no rule labelled. Whatever it cannot ask
// keeps the ladder's verdict, so the model never holds a page back.
func (w *Watcher) decide(ctx context.Context, t *tick, mails []Change, drafts []VerdictDraft, lines map[string]Line) {
	step := &t.decide
	if !step.on || w.withheld(ctx, t, step.route) != "" {
		return
	}
	for i, mail := range mails {
		switch {
		case drafts[i].Decision.LabelID != "":
			continue
		case strings.TrimSpace(mail.Snippet) == "":
			drafts[i].ModelSkipped = true
			step.noText++
		case step.remaining() <= 0:
			drafts[i].ModelSkipped = true
			step.left++
		default:
			w.ask(ctx, t, mail, &drafts[i], lines)
		}
	}
}

// ask puts one mail to the model. The budget is the real time the calls took, so a slow PIM read
// spends none of it. A call cut by the budget or by the run's end leaves the rest to the ladder; any
// other failure stops the model for the tick and stays on the account until an answer clears it.
func (w *Watcher) ask(ctx context.Context, t *tick, mail Change, draft *VerdictDraft, lines map[string]Line) {
	step := &t.decide
	callCtx, cancel := context.WithTimeout(ctx, step.remaining())
	defer cancel()
	start := time.Now()
	answer, err := w.Decider.Choose(callCtx, step.route, mailState(t.lang, mail.Subject, mail.Snippet), step.question)
	step.spent += time.Since(start)
	if err != nil {
		draft.ModelSkipped = true
		step.left++
		if callCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			step.spent = step.budget
			return
		}
		step.failed = true
		w.recordDecide(ctx, t, decide.ErrorClass(err))
		return
	}
	step.inputTokens += answer.InputTokens
	if !step.answered {
		step.answered = true
		w.recordDecide(ctx, t, "")
	}
	labelID := step.labelIDs[answer.Choice]
	p := answer.Probabilities[answer.Choice]
	name := answer.Model
	if name == "" {
		name = step.route.Name()
	}
	applied := labelID != "" && p >= decideThreshold
	draft.Model = &ModelAnswer{LabelID: labelID, Probability: p, Name: name, Options: len(step.question.Options), Applied: applied}
	if !applied {
		step.none++
		return
	}
	step.labelled++
	draft.Decision = ApplyModel(t.book, draft.Ladder, mail.Signals, labelID)
	line := lines[mail.ID]
	line.Tier = draft.Decision.Tier
	lines[mail.ID] = line
}

// recordDecide keeps the failure for the cockpit. Not keeping it costs only the display, so it never
// fails the tick.
func (w *Watcher) recordDecide(ctx context.Context, t *tick, class string) {
	if err := w.Store.RecordDecideFailure(ctx, t.IdentityID, t.AccountID, class); err != nil {
		slog.Warn("mail watch: the decision model's failure was not recorded", "class", class, "error", err)
	}
}
