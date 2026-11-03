//go:build db_integration

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

func TestDelegatedInvocationPersistsArgumentsAndElapsedTime(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	convID := seedConversation(t, pool)
	for _, tc := range []struct {
		name               string
		mutating, canceled bool
		callErr            error
	}{
		{name: "read"},
		{name: "write", mutating: true},
		{name: "error", callErr: errors.New("provider unavailable")},
		{name: "canceled read", canceled: true, callErr: context.Canceled},
		{name: "canceled write", mutating: true, canceled: true, callErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := newKey(convID, tc.name)
			args := json.RawMessage(`{"period":"1y","symbols":["AAPL"]}`)
			startedAt := time.Now().UTC().Add(-12491 * time.Millisecond).Truncate(time.Microsecond)
			ctx, cancel := context.WithCancel(WithDelegatedDispatch(WithReservationKey(ownerCtx(), key, "broker_analysis", args, startedAt)))
			defer cancel()
			verdict, err := g.Decide(ctx, tools.Spec{Name: "broker_analysis", Mutating: tc.mutating}, args, key)
			if err != nil || verdict.Decision != Allow || verdict.Replay != nil {
				t.Fatalf("Decide = %+v, %v", verdict, err)
			}
			if tc.canceled {
				cancel()
			}
			g.FinishCall(ctx, tools.ToolResult{Preview: "actual result", Bytes: 13}, tc.callErr)
			starts, ends := tripleEvents(t, store, key)
			if len(starts) != 1 || len(ends) != 1 {
				t.Fatalf("start/end rows = %d/%d, want 1/1", len(starts), len(ends))
			}
			for _, event := range []toolinvocations.Event{starts[0], ends[0]} {
				if event.Arguments != string(args) || event.ArgsBytes != len(args) {
					t.Errorf("%s args = %q/%d", event.Event, event.Arguments, event.ArgsBytes)
				}
				if !event.StartedAt.Equal(startedAt) {
					t.Errorf("%s started_at = %v, want %v", event.Event, event.StartedAt, startedAt)
				}
			}
			end := ends[0]
			if end.DurationMS < 12491 || end.DurationMS > 17491 {
				t.Errorf("duration_ms = %d, want measured dispatch interval near 12,491ms", end.DurationMS)
			}
			// Postgres rounds to microseconds while the writer computes milliseconds.
			elapsed := end.EndedAt.Sub(end.StartedAt).Milliseconds()
			if delta := elapsed - end.DurationMS; delta < -1 || delta > 1 {
				t.Errorf("duration_ms = %d, timestamp interval = %d", end.DurationMS, elapsed)
			}
			if !end.Timestamp.Equal(end.EndedAt) {
				t.Errorf("end timestamp = %v, ended_at = %v", end.Timestamp, end.EndedAt)
			}
			wantStatus, wantError := "ok", ""
			if tc.callErr != nil {
				wantStatus, wantError = "error", tc.callErr.Error()
			}
			if end.Status != wantStatus || end.Error != wantError || end.ResultPreview != "actual result" {
				t.Errorf("end = %+v", end)
			}
			// Existing first-writer semantics must retain the actual terminal fact.
			g.FinishCall(ctx, tools.ToolResult{Preview: "later duplicate"}, errors.New("late failure"))
			starts, ends = tripleEvents(t, store, key)
			if len(starts) != 1 || len(ends) != 1 || ends[0].ID != end.ID || ends[0].ResultPreview != "actual result" {
				t.Fatalf("duplicate replaced terminal evidence: %+v", ends)
			}
		})
	}
}
