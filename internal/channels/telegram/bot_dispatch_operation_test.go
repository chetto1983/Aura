package telegram

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

type operationCapturingTurn struct {
	mu  sync.Mutex
	got []idempotency.Operation
}

func (r *operationCapturingTurn) driver() turnDriver {
	return func(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
		op, _ := idempotency.OperationFromContext(ctx)
		r.mu.Lock()
		r.got = append(r.got, op)
		r.mu.Unlock()
		return func(yield func(*agent.Event, error) bool) {
			yield(textEvent("ok"), nil)
		}
	}
}

func (r *operationCapturingTurn) snapshot() []idempotency.Operation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]idempotency.Operation, len(r.got))
	copy(out, r.got)
	return out
}

func TestTelegramTurnMintsDistinctInteractiveOperationRoots(t *testing.T) {
	t.Parallel()

	const (
		chatID     = int64(5551)
		identityID = "aaaaaaaa-0000-4000-8000-00000000000a"
	)
	recorder := &operationCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.profileAccounts = multiIdentityAccountFake{ids: map[int64]string{chatID: identityID}}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	for i, text := range []string{"primo turno", "secondo turno"} {
		msg := chatMsg(chatID)
		msg.ID = 7001 + i
		tg.runTurn(context.Background(), msgContext(bot, msg), chatID, text, false)
		tg.wg.Wait()
	}

	got := recorder.snapshot()
	if len(got) != 2 {
		t.Fatalf("turn operations = %d, want 2", len(got))
	}
	for i, operation := range got {
		if operation.Key.IdentityID != identityID {
			t.Errorf("operation %d identity = %q, want %q", i, operation.Key.IdentityID, identityID)
		}
		if operation.Key.Scope != idempotency.ScopeCLICommand {
			t.Errorf("operation %d scope = %q, want %q", i, operation.Key.Scope, idempotency.ScopeCLICommand)
		}
		if !strings.HasPrefix(operation.Key.Key, "telegram-turn:") {
			t.Errorf("operation %d key = %q, want telegram-turn prefix", i, operation.Key.Key)
		}
		if operation.Fingerprint == ([32]byte{}) {
			t.Errorf("operation %d has an empty fingerprint", i)
		}
	}
	if got[0].Key.Key == got[1].Key.Key {
		t.Fatalf("two turns shared operation key %q", got[0].Key.Key)
	}
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Fatal("two turns shared an operation fingerprint")
	}
}

func TestTelegramTurnOperationIsStableForRetriedUpdate(t *testing.T) {
	t.Parallel()

	const (
		chatID     = int64(5552)
		messageID  = 8123
		identityID = "aaaaaaaa-0000-4000-8000-00000000000b"
	)
	ctx := identityctx.WithIdentityID(context.Background(), identityID)

	firstCtx, err := telegramTurnOperation(ctx, chatID, messageID)
	if err != nil {
		t.Fatalf("first telegramTurnOperation: %v", err)
	}
	retryCtx, err := telegramTurnOperation(ctx, chatID, messageID)
	if err != nil {
		t.Fatalf("retry telegramTurnOperation: %v", err)
	}
	first, ok := idempotency.OperationFromContext(firstCtx)
	if !ok {
		t.Fatal("first update has no operation")
	}
	retry, ok := idempotency.OperationFromContext(retryCtx)
	if !ok {
		t.Fatal("retried update has no operation")
	}
	wantKey := fmt.Sprintf("telegram-turn:%s:%d", convID(chatID), messageID)
	if first.Key.Key != wantKey {
		t.Fatalf("operation key = %q, want %q", first.Key.Key, wantKey)
	}
	if first.Key != retry.Key || first.Fingerprint != retry.Fingerprint {
		t.Fatalf("retried update changed operation: first=%+v retry=%+v", first, retry)
	}
}
