package pimwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSortDigestPutsNowFirstThenLabelsThenTheRest(t *testing.T) {
	labels := []Label{{ID: "l1"}, {ID: "l2"}}
	verdicts := []Verdict{
		{ItemID: "none", Tier: TierToday},
		{ItemID: "second", Tier: TierToday, LabelID: "l2"},
		{ItemID: "first", Tier: TierToday, LabelID: "l1"},
		{ItemID: "urgent", Tier: TierNow, LabelID: "l2"},
		{ItemID: "stale", Tier: TierToday, LabelID: "removed"},
	}
	sortDigest(verdicts, labels)
	got := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		got = append(got, v.ItemID)
	}
	if strings.Join(got, " ") != "urgent first second none stale" {
		t.Fatalf("order = %v", got)
	}
}

type staticRoster struct {
	ids []string
	err error
}

func (s staticRoster) IdentityIDs(context.Context) ([]string, error) { return s.ids, s.err }

// partExpirer fails one identity and removes one row from each of the others.
type partExpirer struct {
	failing string
	seen    []string
}

var errExpire = errors.New("expire failed")

func (p *partExpirer) ExpireVerdicts(_ context.Context, identityID string, _ time.Time, _ int) (int, error) {
	p.seen = append(p.seen, identityID)
	if identityID == p.failing {
		return 0, errExpire
	}
	return 1, nil
}

func TestSweepVisitsEveryIdentityAndCarriesOnPastOneThatFails(t *testing.T) {
	expirer := &partExpirer{failing: "b"}
	sweep := Sweep{Store: expirer, Identities: staticRoster{ids: []string{"a", "b", "c"}}}

	removed, err := sweep.SweepExpired(context.Background(), time.Now())
	if removed != 2 || !errors.Is(err, errExpire) || len(expirer.seen) != 3 {
		t.Fatalf("removed %d, err %v, seen %v", removed, err, expirer.seen)
	}
}

func TestSweepReportsARosterThatCannotBeRead(t *testing.T) {
	sweep := Sweep{Identities: staticRoster{err: errExpire}}
	if _, err := sweep.SweepExpired(context.Background(), time.Now()); !errors.Is(err, errExpire) {
		t.Fatalf("err = %v", err)
	}
}
