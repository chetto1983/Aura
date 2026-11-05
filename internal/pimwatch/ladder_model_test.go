package pimwatch

import "testing"

func TestApplyModel(t *testing.T) {
	t.Parallel()
	ads := Label{ID: "l-ads", Name: "Advertising", Discard: true}
	urgent := Label{ID: "l-urgent", Name: "Urgent", Tier: TierNow}
	filed := Label{ID: "l-filed", Name: "Filed", Tier: TierLater}
	plain := Label{ID: "l-plain", Name: "Plain"}
	book := NewBook(nil, []Label{ads, urgent, filed, plain})
	today := func(by DecidedBy) Decision { return Decision{Tier: TierToday, By: by} }

	cases := []struct {
		name      string
		ladder    Decision
		s         Signals
		label     string
		tier      Tier
		by        DecidedBy
		discarded bool
	}{
		{"a rule's tier is final", Decision{Tier: TierToday, By: ByRule}, Signals{WrittenTo: no()}, urgent.ID, TierToday, ByRule, false},
		{"failed authentication is never raised", Decision{Tier: TierToday, By: ByAuthFailed, AuthFailed: true}, Signals{AuthFailed: true, WrittenTo: no()}, urgent.ID, TierToday, ByAuthFailed, false},
		{"someone written to is never discarded", today(ByPeople), Signals{WrittenTo: yes()}, ads.ID, TierToday, ByPeople, false},
		{"someone written to may be raised", today(ByPeople), Signals{WrittenTo: yes()}, urgent.ID, TierNow, ByModel, false},
		{"unknown and not bulk is never lowered", today(ByDefault), Signals{}, filed.ID, TierToday, ByDefault, false},
		{"unknown and bulk is discarded", today(ByDefault), Signals{Bulk: true}, ads.ID, TierLater, ByModel, true},
		{"a stranger is raised", today(ByDefault), Signals{WrittenTo: no()}, urgent.ID, TierNow, ByModel, false},
		{"a tier the ladder already gave keeps the ladder's rung", Decision{Tier: TierLater, By: ByCategory}, Signals{WrittenTo: no()}, filed.ID, TierLater, ByCategory, false},
		{"a label with no tier changes no tier", today(ByDefault), Signals{WrittenTo: no()}, plain.ID, TierToday, ByDefault, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ApplyModel(book, tc.ladder, tc.s, tc.label)
			if got.Tier != tc.tier || got.By != tc.by || got.Discarded != tc.discarded || got.LabelID != tc.label {
				t.Fatalf("ApplyModel = %+v, want %s by %s, discarded %v, label %s", got, tc.tier, tc.by, tc.discarded, tc.label)
			}
			if got.AuthFailed != tc.ladder.AuthFailed {
				t.Fatalf("the authentication mark moved: %+v", got)
			}
		})
	}
}

func TestTierRankOrdersBySoonness(t *testing.T) {
	t.Parallel()
	if tierRank(TierLater) != 1 || tierRank(TierToday) != 2 || tierRank(TierNow) != 3 || tierRank("") != 0 {
		t.Fatalf("ranks = %d %d %d %d", tierRank(TierLater), tierRank(TierToday), tierRank(TierNow), tierRank(""))
	}
}
