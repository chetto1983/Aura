//go:build db_integration

package telegram

import (
	"strconv"
	"testing"
)

// With no recipients named, an identity push goes to the account seen most recently (spec
// 2026-10-28-telegram-push-recipients, R6). The database's own order sent it to the account seen
// least: each update moves the writer's row to the end of the heap (measured 2026-10-28).
func TestIdentityPushReachesTheAccountSeenLast(t *testing.T) {
	s := New(migratedPool(t))
	ctx := ownerCtx()
	identityID := userIdentity(t, s.pool, "seen-last")
	const first, second int64 = 9_000_130_001, 9_000_130_002
	linkThroughOnboarding(t, s, identityID, first)
	linkThroughOnboarding(t, s, identityID, second)
	reached := func() (push, approval string) {
		t.Helper()
		tg := NewChannel(Deps{Offline: true})
		bot := &sendRecorder{}
		tg.deliverBot, tg.deliverResolver = bot, s
		if ok, err := tg.Deliver(ctx, identityID, "promemoria"); err != nil || !ok {
			t.Fatalf("identity push = (%v, %v)", ok, err)
		}
		if ok, err := tg.DeliverApproval(ctx, identityID, "token", "task-1", "reminder"); err != nil || !ok {
			t.Fatalf("approval push = (%v, %v)", ok, err)
		}
		sends := bot.recorded()
		if len(sends) != 2 {
			t.Fatalf("sends = %+v, want one push and one approval", sends)
		}
		return sends[0].to.Recipient(), sends[1].to.Recipient()
	}
	for _, step := range []struct {
		name  string
		touch int64
		want  int64
	}{
		{"neither has written: the one linked last", 0, second},
		{"the first writes", first, first},
		{"the second writes", second, second},
	} {
		if step.touch != 0 {
			if err := s.TouchLastSeen(ctx, identityID, step.touch); err != nil {
				t.Fatal(err)
			}
		}
		want := strconv.FormatInt(step.want, 10)
		if push, approval := reached(); push != want || approval != want {
			t.Errorf("%s: push reached %s and approval %s, want %s", step.name, push, approval, want)
		}
	}
}
