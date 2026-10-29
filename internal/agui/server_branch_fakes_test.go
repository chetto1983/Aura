package agui

import (
	"context"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
)

// server_branch_fakes_test.go holds the D-09 / CHAT-05 branch fake methods split out of
// server_test.go so that file stays <=600 LOC (CLAUDE.md). These satisfy the widened
// ConversationStore + Runner interfaces on the existing unit fakes; the route→store
// mapping is asserted live in conversations_branch_api_test.go against the real store.

// ForkBranch counts the forks and answers forkLeaf, so a test can tell a fork that happened
// from one the lock refused.
func (f *fakeConvStore) ForkBranch(context.Context, string, int, string, string) (int, error) {
	f.forks.Add(1)
	return f.forkLeaf, nil
}

func (f *fakeConvStore) LatestTurnSeq(context.Context, string) (int, error) {
	return f.latestSeq, f.latestErr
}

// TurnBranch records the selected leaf and replays the same scripted events as Turn, so
// the branch re-run wiring is assertable through the fake (the path-walk fidelity is the
// conversations integration test's concern).
func (s *scriptedRunner) TurnBranch(_ context.Context, _ string, leafSeq int) iter.Seq2[*agent.Event, error] {
	s.gotBranchLeaf = leafSeq
	return func(yield func(*agent.Event, error) bool) {
		for _, ev := range s.events {
			if !yield(ev, nil) {
				return
			}
		}
		if s.turnErr != nil {
			yield(nil, s.turnErr)
		}
	}
}
