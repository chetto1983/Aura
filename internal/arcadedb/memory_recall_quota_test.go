package arcadedb

import "testing"

// The split has to leave BOTH kinds able to answer. Measured 2026-09-04 on the operator's
// memory, "is the answer in the block" collapsed for whichever kind fell under two slots:
// one fact slot scored 0.93 and zero scored 0.00, and the turn side fell the same way.
// That floor, not a tuned ratio, is what this pins.
func TestRecallQuotaKeepsBothKindsAbleToAnswer(t *testing.T) {
	for _, limit := range []int{4, 5, 8, 12, 20} {
		quota := recallQuotaFor(limit)
		if quota.Facts+quota.Turns != limit {
			t.Fatalf("limit %d split into %d+%d, which is not the budget asked for",
				limit, quota.Facts, quota.Turns)
		}
		if quota.Facts < recallMinSlotsPerKind || quota.Turns < recallMinSlotsPerKind {
			t.Fatalf("limit %d gave %+v, below the floor either kind needs to answer", limit, quota)
		}
		if quota.Facts < quota.Turns {
			t.Fatalf("limit %d gave %+v: a conversation arrives as a window of turns, so the "+
				"wordier kind must not hold the larger share", limit, quota)
		}
	}
}

// A caller asking for one or two records cannot be given a floor of two per kind, and
// silently rounding the budget up would spend context the caller declined. Below the
// point where the split is meaningful the limit is honoured whole.
func TestRecallQuotaHonoursABudgetTooSmallToSplit(t *testing.T) {
	for limit := range 2 * recallMinSlotsPerKind {
		quota := recallQuotaFor(limit)
		if quota.Facts != limit || quota.Turns != 0 {
			t.Fatalf("limit %d gave %+v, want the whole budget and no split", limit, quota)
		}
	}
}

// Facts lead is the ONLY cross-kind ordering decision in the recall path. Within a kind
// the engine's ranking must survive untouched: reordering it here would reintroduce, in
// Go, the cross-type comparison the split exists to remove.
func TestMergeRecallRankingsPreservesEachKindsOwnOrder(t *testing.T) {
	facts := []recallRankedRID{{rid: "#1:0", score: 0.4}, {rid: "#1:1", score: 0.9}}
	turns := []recallRankedRID{{rid: "#2:0", score: 0.7}}
	merged := mergeRecallRankings(facts, turns)

	want := []string{"#1:0", "#1:1", "#2:0"}
	if len(merged) != len(want) {
		t.Fatalf("merged %d rankings, want %d", len(merged), len(want))
	}
	for index, rid := range want {
		if merged[index].rid != rid {
			t.Fatalf("merged = %+v, want %v: facts first, each kind in its own order", merged, want)
		}
	}
}

// The quota is a ceiling on ADMITTED evidence, and a conversation is only admitted once
// its window survives. Booking the slot before that read would let a dropped window spend
// a slot, returning fewer conversations than the caller's budget allows — the failure the
// two-call shape exists to prevent.
func TestRecallAdmissionDoesNotSpendASlotOnADroppedTurn(t *testing.T) {
	quota := recallQuota{Facts: 2, Turns: 2}
	var admitted recallAdmission

	if !admitted.canAdmitTurn(quota) {
		t.Fatal("first conversation refused with the quota untouched")
	}
	// The window came back empty, so nothing is booked.
	if !admitted.canAdmitTurn(quota) {
		t.Fatal("a conversation that was never booked consumed its slot")
	}
	admitted.bookTurn()
	admitted.bookTurn()
	if admitted.canAdmitTurn(quota) {
		t.Fatalf("quota %+v admitted a third conversation", quota)
	}
}

// Both quotas spent is the only condition that may stop the walk early. Stopping while
// one kind still has room would hand the ranking's tail the power to end the block.
func TestRecallAdmissionStopsOnlyWhenBothKindsAreFull(t *testing.T) {
	quota := recallQuota{Facts: 1, Turns: 1}
	var admitted recallAdmission

	if admitted.exhausted(quota) {
		t.Fatal("exhausted before anything was admitted")
	}
	if !admitted.admitFact(quota) {
		t.Fatal("first fact refused")
	}
	if admitted.exhausted(quota) {
		t.Fatal("stopped with the conversation quota still unspent")
	}
	if admitted.admitFact(quota) {
		t.Fatalf("quota %+v admitted a second fact", quota)
	}
	admitted.bookTurn()
	if !admitted.exhausted(quota) {
		t.Fatal("both quotas spent and the walk would still continue")
	}
}

// The regression the split exists for, at the level a caller sees it.
//
// A fact ranking long enough to fill the whole budget used to do exactly that: evidence
// was cut at `limit` walking one mixed list, so conversations after that cut never
// arrived and a question whose answer was a past exchange got a block of facts about
// something else. Measured on the live memory before the split, at the same five-slot
// budget, the fact side scored 0.79 and the conversation side survived only because the
// renderer capped conversations separately. With the quota neither kind can starve the
// other.
func TestRecallSplitsTheBudgetInsteadOfLettingOneKindFillIt(t *testing.T) {
	factRanking := `{"result":[{"rid":"#10:1","score":0.9},{"rid":"#10:2","score":0.8},` +
		`{"rid":"#10:3","score":0.7},{"rid":"#10:4","score":0.6},{"rid":"#10:5","score":0.5}]}`
	turnRanking := `{"result":[{"rid":"#20:1","score":0.4}]}`
	facts := `{"result":[` +
		`{"@rid":"#10:1","statement":"Davide keeps the blue notebook.","predicate":"keeps","subject":"Davide","object":"blue notebook","valid_from":"2026-01-01T00:00:00Z","sources":[]},` +
		`{"@rid":"#10:2","statement":"Davide lives in Caraglio.","predicate":"lives_in","subject":"Davide","object":"Caraglio","valid_from":"2026-01-01T00:00:00Z","sources":[]},` +
		`{"@rid":"#10:3","statement":"Davide works at Pmsync.","predicate":"works_at","subject":"Davide","object":"Pmsync","valid_from":"2026-01-01T00:00:00Z","sources":[]},` +
		`{"@rid":"#10:4","statement":"Davide prefers Go.","predicate":"prefers","subject":"Davide","object":"Go","valid_from":"2026-01-01T00:00:00Z","sources":[]},` +
		`{"@rid":"#10:5","statement":"Davide has a dog named Olaf.","predicate":"has_pet","subject":"Davide","object":"Olaf","valid_from":"2026-01-01T00:00:00Z","sources":[]}]}`
	empty := `{"result":[]}`
	// fact ranking, turn ranking, fact hydration, turn hydration, the conversation
	// window, then the seed traversals the entity expansion asks for.
	client, _ := recordingClient(t, factRanking, turnRanking, facts, recallAnchorRow,
		recallWindowRows, empty, empty, empty)

	result, err := client.RecallMemory(t.Context(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "blue notebook", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}

	quota := recallQuotaFor(5)
	var gotFacts, gotTurns int
	for _, item := range result.Evidence {
		switch item.Kind {
		case RecallEvidenceFact:
			gotFacts++
		case RecallEvidenceConversation:
			gotTurns++
		}
	}
	if gotFacts > quota.Facts {
		t.Fatalf("%d facts admitted against a quota of %d: %+v", gotFacts, quota.Facts, result.Retrieval)
	}
	if gotTurns == 0 {
		t.Fatalf("a five-deep fact ranking starved the conversation quota entirely: %+v", result.Evidence)
	}
}
