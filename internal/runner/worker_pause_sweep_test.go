package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

type scriptedWorkerPauseLister struct {
	due        []ExpiredWorkerPause
	err        error
	identities []string
	cutoffs    []time.Time
	limits     []int
}

func (s *scriptedWorkerPauseLister) ListExpiredWorkerPauses(_ context.Context, identityID string, cutoff time.Time, limit int) ([]ExpiredWorkerPause, error) {
	s.identities = append(s.identities, identityID)
	s.cutoffs = append(s.cutoffs, cutoff)
	s.limits = append(s.limits, limit)
	return s.due, s.err
}

// scriptedWorkerPauseExpirer is the interface seam the rollback-on-failure contract is
// asserted through daemon-free: an error from it means NOTHING was committed for that
// row (the real PoolWorkerPauseExpirer is one transaction), so the sweep must neither
// count it nor go on as if it had.
type scriptedWorkerPauseExpirer struct {
	errs     map[string]error
	expiries []WorkerPauseExpiry
}

func (s *scriptedWorkerPauseExpirer) ExpireWorkerPause(_ context.Context, expiry WorkerPauseExpiry) error {
	s.expiries = append(s.expiries, expiry)
	return s.errs[expiry.Claim.Token]
}

func expiredWorkerPauseFixture(token, jobID string) ExpiredWorkerPause {
	return ExpiredWorkerPause{
		JobID:      jobID,
		IdentityID: "identity-1",
		Pause: askuser.Pending{
			Token:           token,
			ConversationID:  "conv-origin",
			Kind:            "clarification",
			Question:        "Which region should I deploy to?",
			ToolCallID:      "call-ask-" + token,
			PendingActionID: "fence-" + token,
			OwningWorkerID:  jobID,
		},
	}
}

func TestExpireWorkerPausesBuildsAFencedExpiryWithAnAssistantTrace(t *testing.T) {
	due := expiredWorkerPauseFixture("tok-1", "job-1")
	lister := &scriptedWorkerPauseLister{due: []ExpiredWorkerPause{due}}
	expirer := &scriptedWorkerPauseExpirer{}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(), WorkerPauseSweepDeps{Lister: lister, Expirer: expirer},
		"identity-1", now, 10*time.Minute, 7)
	if err != nil || expired != 1 {
		t.Fatalf("ExpireWorkerPauses = %d, %v; want 1, nil", expired, err)
	}
	if len(lister.identities) != 1 || lister.identities[0] != "identity-1" || lister.limits[0] != 7 {
		t.Fatalf("lister called with identities=%v limits=%v; want [identity-1] [7]", lister.identities, lister.limits)
	}
	if want := now.Add(-10 * time.Minute); !lister.cutoffs[0].Equal(want) {
		t.Fatalf("cutoff = %s, want now-ttl = %s", lister.cutoffs[0], want)
	}
	if len(expirer.expiries) != 1 {
		t.Fatalf("expirer received %d expiries, want 1", len(expirer.expiries))
	}
	got := expirer.expiries[0]
	if got.Claim.Token != "tok-1" || got.Claim.ExpectActionID != "fence-tok-1" {
		t.Fatalf("claim = %#v; want token tok-1 fenced by fence-tok-1 (D-12)", got.Claim)
	}
	if got.Claim.Answer.Action != askuser.ActionExpired || got.Claim.Answer.Content != expiredWorkerPauseContent {
		t.Fatalf("answer = %#v; want the distinct worker-pause expiry refusal", got.Claim.Answer)
	}
	turn := got.Claim.Turn
	if turn.ConversationID != "conv-origin" || turn.Role != llm.RoleAssistant || turn.ToolCallID != "" {
		t.Fatalf("trace turn = %#v; want a plain assistant turn in the origin conversation (a RoleTool turn keyed by the worker's tool_call_id would be an orphan there)", turn)
	}
	if !strings.Contains(turn.Content, "job-1") || !strings.Contains(turn.Content, due.Pause.Question) {
		t.Fatalf("trace %q does not name which worker asked and what went unanswered", turn.Content)
	}
	if got.JobID != "job-1" || got.IdentityID != "identity-1" || got.ErrorMessage == "" {
		t.Fatalf("queue resolution = job %q identity %q error %q; want the parked row named with a non-empty error_message", got.JobID, got.IdentityID, got.ErrorMessage)
	}
}

func TestExpireWorkerPausesTTLAtOrBelowZeroDisablesExpiry(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		lister := &scriptedWorkerPauseLister{due: []ExpiredWorkerPause{expiredWorkerPauseFixture("tok-1", "job-1")}}
		expirer := &scriptedWorkerPauseExpirer{}
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(), WorkerPauseSweepDeps{Lister: lister, Expirer: expirer},
			"identity-1", time.Now(), ttl, 10)
		if err != nil || expired != 0 {
			t.Fatalf("ttl=%s: ExpireWorkerPauses = %d, %v; want 0, nil (disabled)", ttl, expired, err)
		}
		if len(lister.identities) != 0 || len(expirer.expiries) != 0 {
			t.Fatalf("ttl=%s: a disabled sweep must not list or expire anything (listed %d, expired %d)", ttl, len(lister.identities), len(expirer.expiries))
		}
	}
}

func TestExpireWorkerPausesErrorAndConflictPaths(t *testing.T) {
	testErr := errors.New("worker pause sweep test error")
	first := expiredWorkerPauseFixture("first", "job-first")
	second := expiredWorkerPauseFixture("second", "job-second")
	ttl := time.Minute

	t.Run("unconfigured deps", func(t *testing.T) {
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(), WorkerPauseSweepDeps{}, "identity-1", time.Now(), ttl, 10)
		if expired != 0 || err == nil {
			t.Fatalf("ExpireWorkerPauses = %d, %v; want 0 and error", expired, err)
		}
	})

	t.Run("empty identity fails closed", func(t *testing.T) {
		lister := &scriptedWorkerPauseLister{due: []ExpiredWorkerPause{first}}
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(), WorkerPauseSweepDeps{Lister: lister, Expirer: &scriptedWorkerPauseExpirer{}},
			"", time.Now(), ttl, 10)
		if expired != 0 || err == nil || len(lister.identities) != 0 {
			t.Fatalf("ExpireWorkerPauses = %d, %v (listed %d); want 0, error, nothing listed", expired, err, len(lister.identities))
		}
	})

	t.Run("list error", func(t *testing.T) {
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(),
			WorkerPauseSweepDeps{Lister: &scriptedWorkerPauseLister{err: testErr}, Expirer: &scriptedWorkerPauseExpirer{}},
			"identity-1", time.Now(), ttl, 10)
		if expired != 0 || !errors.Is(err, testErr) {
			t.Fatalf("ExpireWorkerPauses = %d, %v; want 0 and wrapped list error", expired, err)
		}
	})

	t.Run("vanished pause is skipped and the sweep continues", func(t *testing.T) {
		expirer := &scriptedWorkerPauseExpirer{errs: map[string]error{"first": askuser.ErrPauseNotFound}}
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(),
			WorkerPauseSweepDeps{Lister: &scriptedWorkerPauseLister{due: []ExpiredWorkerPause{first, second}}, Expirer: expirer},
			"identity-1", time.Now(), ttl, 10)
		if err != nil || expired != 1 {
			t.Fatalf("ExpireWorkerPauses = %d, %v; want 1, nil", expired, err)
		}
		if len(expirer.expiries) != 2 || expirer.expiries[1].Claim.Token != "second" {
			t.Fatalf("expiries = %#v; want both candidates attempted in order", expirer.expiries)
		}
	})

	t.Run("expirer failure is not counted and stops the pass", func(t *testing.T) {
		expirer := &scriptedWorkerPauseExpirer{errs: map[string]error{"second": testErr}}
		expired, err := (&Runner{}).ExpireWorkerPauses(context.Background(),
			WorkerPauseSweepDeps{Lister: &scriptedWorkerPauseLister{due: []ExpiredWorkerPause{first, second}}, Expirer: expirer},
			"identity-1", time.Now(), ttl, 10)
		if expired != 1 || !errors.Is(err, testErr) {
			t.Fatalf("ExpireWorkerPauses = %d, %v; want 1 and the wrapped expirer error (a failed trace never counts as expired)", expired, err)
		}
	})
}
