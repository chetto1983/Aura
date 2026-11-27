package toolapprovals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMalformedInputIsUnavailableBeforeAnyQuery pins the refusals that need no database: a
// malformed key or a move the state table lacks never reaches the pool or the transaction.
func TestMalformedInputIsUnavailableBeforeAnyQuery(t *testing.T) {
	ctx := context.Background()
	store := NewStore(nil)
	owner, conv, token := uuid.NewString(), uuid.NewString(), uuid.NewString()
	input := Input{IdentityID: owner, ConversationID: conv, PauseToken: token}
	withConversation := func(c string) Input { in := input; in.ConversationID = c; return in }
	withToken := func(p string) Input { in := input; in.PauseToken = p; return in }
	for name, call := range map[string]func() error{
		"insert, malformed owner": func() error {
			return store.InsertTx(ctx, nil, Input{IdentityID: "x", ConversationID: conv, PauseToken: token})
		},
		"insert, malformed conversation": func() error { return store.InsertTx(ctx, nil, withConversation("x")) },
		"insert, malformed pause":        func() error { return store.InsertTx(ctx, nil, withToken("x")) },
		"decide to a state no decision reaches": func() error {
			_, err := store.DecideTx(ctx, nil, Decision{PauseToken: token, To: StatusRunning})
			return err
		},
		"decide, malformed pause": func() error {
			_, err := store.DecideTx(ctx, nil, Decision{PauseToken: "x", To: StatusAccepted})
			return err
		},
		"decide, malformed operator": func() error {
			_, err := store.DecideTx(ctx, nil, Decision{PauseToken: token, To: StatusAccepted, OperatorID: "x"})
			return err
		},
		"open, malformed owner":          func() error { _, err := store.Open(ctx, "x", conv); return err },
		"open calls, malformed conv":     func() error { _, err := store.OpenCalls(ctx, owner, "x"); return err },
		"decided, malformed owner":       func() error { _, err := store.Decided(ctx, "x", conv, 4); return err },
		"stale, malformed owner":         func() error { _, err := store.StaleAccepted(ctx, "x", time.Now()); return err },
		"transition, malformed owner":    func() error { return store.Transition(ctx, "x", token, StatusAccepted, StatusRunning) },
		"transition, pending -> running": func() error { return store.TransitionTx(ctx, nil, token, StatusPending, StatusRunning) },
		"transition, done -> running":    func() error { return store.TransitionTx(ctx, nil, token, StatusDone, StatusRunning) },
		"transition, malformed record":   func() error { return store.TransitionTx(ctx, nil, "x", StatusAccepted, StatusRunning) },
		"result committed, malformed owner": func() error {
			_, err := store.ResultCommitted(ctx, "x", Record{Input: input})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", name, err)
		}
	}
}
