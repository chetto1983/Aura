package agui

import (
	"context"
	"iter"
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/runner"
)

// server_run_branch.go: which branch a run continues, and the seqs it reports back (spec
// 2026-10-27, D2, D7 and D11). The client names the last turn of the branch it has on screen
// as aura.parent_seq. The run reports the turn it started from and the turn it ended on, so
// the cockpit can address its live messages by seq, as it does the ones a reload gave it.

// continueBranch validates aura.parent_seq and puts it on ctx for the runner. It writes the
// refusal and reports false when the seq is not positive (400) or names no turn (404). It
// also refuses with 409 while a pause is pending, unless the seq is the turn written last: a
// resume appends after that turn, and a run on another branch in between would graft the
// resumed result onto it.
func (s *Server) continueBranch(ctx context.Context, w http.ResponseWriter, threadID string, parentSeq *int) (context.Context, bool) {
	if parentSeq == nil {
		return ctx, true
	}
	latest, ok := s.requireTurnSeq(ctx, w, threadID, "aura.parent_seq", *parentSeq)
	if !ok {
		return ctx, false
	}
	if *parentSeq != latest {
		pending, err := s.threadHasPendingPause(ctx, threadID)
		if err != nil {
			writeStoreErr(w, err)
			return ctx, false
		}
		if pending {
			http.Error(w, "answer the pending request before continuing another branch", http.StatusConflict)
			return ctx, false
		}
	}
	return runner.WithBranchLeaf(ctx, *parentSeq), true
}

// requireTurnSeq checks that seq, the request field named name, is a turn of the thread. It
// writes the refusal and reports false when seq is not positive (400) or lies above the turn
// written last (404). Seqs are dense, so every seq up to that one names a turn. It returns
// the turn written last; a refusal returns the zero values, which callers do not read.
func (s *Server) requireTurnSeq(ctx context.Context, w http.ResponseWriter, threadID, name string, seq int) (latest int, ok bool) {
	if seq <= 0 {
		http.Error(w, name+" must be a positive turn seq", http.StatusBadRequest)
		return
	}
	last, err := s.conv.LatestTurnSeq(scopedCtx(ctx), threadID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if seq > last {
		http.Error(w, "turn not found", http.StatusNotFound)
		return
	}
	return last, true
}

func (s *Server) threadHasPendingPause(ctx context.Context, threadID string) (bool, error) {
	if s.approvals == nil {
		return false, nil
	}
	pendings, err := s.approvals.ListPendingAllForIdentity(ctx, scopedIdentityID(ctx), defaultApprovalsLimit)
	if err != nil {
		return false, err
	}
	for _, p := range pendings {
		if p.ConversationID == threadID {
			return true, nil
		}
	}
	return false, nil
}

// startTurn drives the user turn the run endpoint was given, through the runner variant that
// keeps the model's text apart from the visible one when the two differ, and reports its seqs.
func (s *Server) startTurn(ctx context.Context, threadID string, userMsg, modelUserMsg *string) iter.Seq2[*agent.Event, error] {
	head := &runner.TurnHead{}
	ctx = runner.WithTurnHead(ctx, head)
	turn := s.run.Turn(ctx, threadID, modelUserMsg)
	if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
		if split, ok := s.run.(modelUserMessageRunner); ok {
			turn = split.TurnWithModelUserMessage(ctx, threadID, *userMsg, *modelUserMsg)
		}
	}
	return s.reportTurnSeqs(ctx, threadID, head, turn)
}

// reportTurnSeqs passes the run's events through. It adds a head_seq state delta before the
// first event once the run has recorded its head, so a run that pauses still reports it. It
// adds a leaf_seq delta, the turn written last, after the last event. A conversation with
// no turns has no leaf, and gets no delta.
func (s *Server) reportTurnSeqs(ctx context.Context, threadID string, head *runner.TurnHead, turn iter.Seq2[*agent.Event, error]) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		reported := false
		for ev, err := range turn {
			if !reported && head.Seq() > 0 {
				reported = true
				if !yield(seqDelta("head_seq", head.Seq()), nil) {
					return
				}
			}
			if !yield(ev, err) {
				return
			}
		}
		leaf, err := s.conv.LatestTurnSeq(scopedCtx(ctx), threadID)
		if err != nil {
			slog.Warn("agui: read the run's last turn (the stream ends without leaf_seq)",
				"thread", redact.Line(threadID), "err", redact.Line(err.Error()))
			return
		}
		if leaf <= 0 {
			return
		}
		last := seqDelta("leaf_seq", leaf)
		if !reported && head.Seq() > 0 {
			last.Actions.StateDelta["head_seq"] = head.Seq()
		}
		yield(last, nil)
	}
}

func seqDelta(key string, seq int) *agent.Event {
	return &agent.Event{Actions: agent.Actions{StateDelta: map[string]any{key: seq}}}
}
