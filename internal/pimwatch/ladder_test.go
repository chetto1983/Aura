package pimwatch

import (
	"testing"
	"testing/quick"
	"time"
)

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestDecideMailLadder(t *testing.T) {
	t.Parallel()
	client := Label{ID: "l-client", Name: "Cliente"}
	ads := Label{ID: "l-ads", Name: "Pubblicità", Tier: TierLater}
	book := NewBook([]Rule{
		{Key: "boss@acme.it", Kind: KeySender, Tier: TierNow},
		{Key: "muted@acme.it", Kind: KeySender, Tier: TierLater},
		{Key: "client@acme.it", Kind: KeySender, LabelID: client.ID},
		{Key: "ads@shop.it", Kind: KeySender, LabelID: ads.ID},
		{Key: "both@shop.it", Kind: KeySender, Tier: TierToday, LabelID: ads.ID},
		{Key: "acme.it", Kind: KeyDomain, Tier: TierLater},
		{Key: "promo.it", Kind: KeyDomain, LabelID: client.ID},
	}, []Label{client, ads})

	cases := []struct {
		name   string
		from   string
		s      Signals
		want   Tier
		by     DecidedBy
		label  string
		failed bool
	}{
		{"a rule wins over a stranger", "Boss <boss@acme.it>", Signals{WrittenTo: no()}, TierNow, ByRule, "", false},
		{"a rule wins over the people floor", "Muted <muted@acme.it>", Signals{WrittenTo: yes(), ProviderImportant: true}, TierLater, ByRule, "", false},
		{"a rule wins over failed authentication, and the mark stays", "boss@acme.it", Signals{AuthFailed: true}, TierNow, ByRule, "", true},
		{"a sender rule beats a domain rule, though it gives only a label", "client@acme.it", Signals{WrittenTo: no()}, TierToday, ByDefault, client.ID, false},
		{"a domain rule applies to the rest of the domain", "other@acme.it", Signals{WrittenTo: yes(), ProviderImportant: true}, TierLater, ByRule, "", false},
		{"a label-only rule goes on down the ladder", "x@promo.it", Signals{WrittenTo: yes(), ProviderImportant: true}, TierNow, ByPeople, client.ID, false},
		{"a label-only rule still reaches rung 4", "x@promo.it", Signals{WrittenTo: no(), ProviderCategory: "promotions"}, TierLater, ByCategory, client.ID, false},
		{"a label's tier acts as a rule", "ads@shop.it", Signals{WrittenTo: yes(), ProviderImportant: true}, TierLater, ByRule, ads.ID, false},
		{"a rule's own tier beats its label's", "both@shop.it", Signals{}, TierToday, ByRule, ads.ID, false},
		{"failed authentication is today and never now", "x@y.it", Signals{AuthFailed: true, WrittenTo: yes(), ProviderImportant: true}, TierToday, ByAuthFailed, "", true},
		{"someone written to, marked important, is now", "x@y.it", Signals{WrittenTo: yes(), ProviderImportant: true}, TierNow, ByPeople, "", false},
		{"someone written to is never below today", "x@y.it", Signals{WrittenTo: yes(), ProviderCategory: "promotions"}, TierToday, ByPeople, "", false},
		{"bulk mail is not people", "x@y.it", Signals{WrittenTo: yes(), Bulk: true, ProviderImportant: true}, TierToday, ByDefault, "", false},
		{"bulk mail in a promotional category is filed", "x@y.it", Signals{WrittenTo: yes(), Bulk: true, ProviderCategory: "promotions"}, TierLater, ByCategory, "", false},
		{"importance alone raises nobody", "x@y.it", Signals{WrittenTo: no(), ProviderImportant: true}, TierToday, ByDefault, "", false},
		{"unknown is not a person", "x@y.it", Signals{ProviderImportant: true}, TierToday, ByDefault, "", false},
		{"unknown is not a stranger either: doubt never files", "x@y.it", Signals{ProviderCategory: "social"}, TierToday, ByDefault, "", false},
		{"bulk with no answer is filed by category, as the PIM skips the question for bulk", "x@y.it", Signals{Bulk: true, ProviderCategory: "social"}, TierLater, ByCategory, "", false},
		{"Gmail's promotions", "x@y.it", Signals{WrittenTo: no(), ProviderCategory: "Promotions"}, TierLater, ByCategory, "", false},
		{"Gmail's social", "x@y.it", Signals{WrittenTo: no(), ProviderCategory: " social "}, TierLater, ByCategory, "", false},
		{"Graph's other says nothing", "x@y.it", Signals{WrittenTo: no(), ProviderCategory: "other"}, TierToday, ByDefault, "", false},
		{"nothing decided is today", "x@y.it", Signals{WrittenTo: no()}, TierToday, ByDefault, "", false},
		{"an unparsable sender matches no rule", "not an address", Signals{WrittenTo: no()}, TierToday, ByDefault, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideMail(book, tc.from, tc.s)
			if got.Tier != tc.want || got.By != tc.by || got.LabelID != tc.label || got.AuthFailed != tc.failed {
				t.Fatalf("DecideMail = %+v, want tier %s by %s label %q failed %v", got, tc.want, tc.by, tc.label, tc.failed)
			}
		})
	}
}

// signalsFrom spreads a fuzzed byte over the signals the ladder reads.
func signalsFrom(b uint8) Signals {
	s := Signals{ProviderImportant: b&1 != 0, Bulk: b&2 != 0, AuthFailed: b&4 != 0}
	switch (b >> 3) & 3 {
	case 0:
		s.ProviderCategory = "promotions"
	case 1:
		s.ProviderCategory = "social"
	case 2:
		s.ProviderCategory = "other"
	}
	switch (b >> 5) & 3 {
	case 0:
		s.WrittenTo = yes()
	case 1:
		s.WrittenTo = no()
	}
	return s
}

// With no rule, the spec's three promises hold for every combination of signals.
func TestLadderProperties(t *testing.T) {
	t.Parallel()
	book := NewBook(nil, nil)
	decide := func(b uint8) (Signals, Decision) {
		s := signalsFrom(b)
		return s, DecideMail(book, "someone@somewhere.it", s)
	}
	checks := map[string]func(uint8) bool{
		"a stranger never reaches now": func(b uint8) bool {
			s, d := decide(b)
			return (s.WrittenTo != nil && *s.WrittenTo && !s.Bulk) || d.Tier != TierNow
		},
		"a person is never filed": func(b uint8) bool {
			s, d := decide(b)
			return s.WrittenTo == nil || !*s.WrittenTo || s.Bulk || s.AuthFailed || d.Tier != TierLater
		},
		"failed authentication is today": func(b uint8) bool {
			s, d := decide(b)
			return !s.AuthFailed || (d.Tier == TierToday && d.AuthFailed)
		},
		"doubt is never filed": func(b uint8) bool {
			s, d := decide(b)
			return s.WrittenTo != nil || s.Bulk || d.Tier != TierLater
		},
		"only a named category files": func(b uint8) bool {
			s, d := decide(b)
			return d.Tier != TierLater || fileableCategory(s.ProviderCategory)
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for b := range 256 {
				if !check(uint8(b)) { //nolint:gosec // G115: b is below 256.
					t.Fatalf("broken by %08b: %+v", b, signalsFrom(uint8(b))) //nolint:gosec // G115: as above.
				}
			}
		})
	}
}

func TestARuleWinsWhateverTheSignalsSay(t *testing.T) {
	t.Parallel()
	book := NewBook([]Rule{{Key: "boss@acme.it", Kind: KeySender, Tier: TierLater}}, nil)
	check := func(b uint8) bool {
		d := DecideMail(book, "boss@acme.it", signalsFrom(b))
		return d.Tier == TierLater && d.By == ByRule
	}
	if err := quick.Check(check, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSenderKey(t *testing.T) {
	t.Parallel()
	cases := []struct{ from, address, domain string }{
		{"Mario Rossi <Mario.Rossi@ACME.it>", "mario.rossi@acme.it", "acme.it"},
		{"plain@acme.it", "plain@acme.it", "acme.it"},
		{`"Rossi, Mario" <m@acme.it>`, "m@acme.it", "acme.it"},
		{"nobody", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		if address, domain := SenderKey(tc.from); address != tc.address || domain != tc.domain {
			t.Errorf("SenderKey(%q) = %q, %q", tc.from, address, domain)
		}
	}
}

func TestBookLabelName(t *testing.T) {
	t.Parallel()
	book := NewBook(nil, []Label{{ID: "l", Name: "Cliente"}})
	if got := book.LabelName("l"); got != "Cliente" {
		t.Fatalf("LabelName = %q", got)
	}
	if got := book.LabelName("absent"); got != "" {
		t.Fatalf("LabelName(absent) = %q", got)
	}
}

func TestTierValid(t *testing.T) {
	t.Parallel()
	for _, tier := range []Tier{TierNow, TierToday, TierLater} {
		if !tier.Valid() {
			t.Errorf("%q is not valid", tier)
		}
	}
	for _, tier := range []Tier{"", "soon", "NOW"} {
		if tier.Valid() {
			t.Errorf("%q is valid", tier)
		}
	}
}

func TestDecideEventNamesWhatHappened(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	soon := now.Add(3 * time.Hour)
	cases := []struct {
		name string
		e    EventFacts
		want ChangeWord
	}{
		{"an invitation", EventFacts{Type: ChangeAdded, Start: soon, Fingerprint: "a"}, WordNew},
		{"a moved event", EventFacts{Type: ChangeUpdated, Start: soon, Fingerprint: "b", Prior: "a", Seen: true}, WordMoved},
		{"an added change to a known event", EventFacts{Type: ChangeAdded, Start: soon, Fingerprint: "b", Prior: "a", Seen: true}, WordMoved},
		{"a cancellation on an update", EventFacts{Type: ChangeUpdated, Start: soon, Cancelled: true, Fingerprint: "a"}, WordCancelled},
		{"a delete of a known event", EventFacts{Type: ChangeDeleted, Start: soon, Prior: "a", Seen: true}, WordCancelled},
		{"Google's delete with a time", EventFacts{Type: ChangeDeleted, Start: soon, Cancelled: true, Fingerprint: "a"}, WordCancelled},
		{"the person's own move", EventFacts{Type: ChangeUpdated, Start: soon, OrganizerIsSelf: true, Fingerprint: "b", Prior: "a", Seen: true}, WordMoved},
		{"nothing shows what changed", EventFacts{Type: ChangeUpdated, Start: soon, Fingerprint: "a"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, told := DecideEvent(now, tc.e)
			if !told || got.Change != tc.want {
				t.Fatalf("DecideEvent = %+v, told %v, want %q", got, told, tc.want)
			}
		})
	}
}

func TestDecideEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	soon, far := now.Add(3*time.Hour), now.Add(48*time.Hour)
	cases := []struct {
		name string
		e    EventFacts
		want Tier
		told bool
	}{
		{"a new invitation within 24 hours", EventFacts{Type: ChangeAdded, Start: soon, Fingerprint: "a"}, TierNow, true},
		{"exactly 24 hours away", EventFacts{Type: ChangeAdded, Start: now.Add(24 * time.Hour), Fingerprint: "a"}, TierNow, true},
		{"a new invitation next week", EventFacts{Type: ChangeAdded, Start: far, Fingerprint: "a"}, TierToday, true},
		{"a moved event within 24 hours", EventFacts{Type: ChangeUpdated, Start: soon, Fingerprint: "b", Prior: "a", Seen: true}, TierNow, true},
		{"a moved event far off", EventFacts{Type: ChangeUpdated, Start: far, Fingerprint: "b", Prior: "a", Seen: true}, TierToday, true},
		{"a meeting moved away from the next 24 hours", EventFacts{Type: ChangeUpdated, Start: far, KeptStart: soon, Fingerprint: "b", Prior: "a", Seen: true}, TierNow, true},
		{"a meeting moved into the next 24 hours", EventFacts{Type: ChangeUpdated, Start: soon, KeptStart: far, Fingerprint: "b", Prior: "a", Seen: true}, TierNow, true},
		{"a meeting moved from far to far", EventFacts{Type: ChangeUpdated, Start: far, KeptStart: far.Add(24 * time.Hour), Fingerprint: "b", Prior: "a", Seen: true}, TierToday, true},
		{"a meeting moved away after its kept time passed", EventFacts{Type: ChangeUpdated, Start: far, KeptStart: now.Add(-time.Hour), Fingerprint: "b", Prior: "a", Seen: true}, TierToday, true},
		{"the person's own meeting moved away", EventFacts{Type: ChangeUpdated, Start: far, KeptStart: soon, OrganizerIsSelf: true, Fingerprint: "b", Prior: "a", Seen: true}, TierLater, true},
		{"a new description says nothing", EventFacts{Type: ChangeUpdated, Start: soon, Fingerprint: "a", Prior: "a", Seen: true}, "", false},
		{"a cancelled event within 24 hours", EventFacts{Type: ChangeDeleted, Start: soon, Prior: "a", Seen: true}, TierNow, true},
		{"a deleted event with no time", EventFacts{Type: ChangeDeleted, Prior: "a", Seen: true}, TierToday, true},
		{"a delete of a cancellation already told says nothing", EventFacts{Type: ChangeDeleted, Start: soon, Prior: "a", PriorWord: WordCancelled, Seen: true}, "", false},
		{"a delete of a moved event is still a cancellation", EventFacts{Type: ChangeDeleted, Start: soon, Prior: "a", PriorWord: WordMoved, Seen: true}, TierNow, true},
		{"the person's own known meeting deleted", EventFacts{Type: ChangeDeleted, Start: soon, Prior: "a", OrganizerIsSelf: true, Seen: true}, TierLater, true},
		{"a repeated delete says nothing", EventFacts{Type: ChangeDeleted, Prior: deletedFingerprint, Seen: true}, "", false},
		{"an event already over", EventFacts{Type: ChangeAdded, Start: now.Add(-time.Hour), Fingerprint: "a"}, TierToday, true},
		{"the person's own event", EventFacts{Type: ChangeAdded, Start: soon, OrganizerIsSelf: true, Fingerprint: "a"}, TierLater, true},
		{"an update to an event older than the watch proves no move", EventFacts{Type: ChangeUpdated, Start: soon, Fingerprint: "a"}, TierToday, true},
		{"a cancellation is a fact with no row", EventFacts{Type: ChangeUpdated, Start: soon, Cancelled: true, Fingerprint: "a"}, TierNow, true},
		{"a cancellation far off goes to the digest", EventFacts{Type: ChangeUpdated, Start: far, Cancelled: true, Fingerprint: "a"}, TierToday, true},
		{"the person's own cancellation", EventFacts{Type: ChangeUpdated, Start: soon, Cancelled: true, OrganizerIsSelf: true, Fingerprint: "a"}, TierLater, true},
		{"a cancellation told once says nothing again", EventFacts{Type: ChangeUpdated, Start: soon, Cancelled: true, Fingerprint: "a", Prior: "a", Seen: true}, "", false},
		{"a delete nobody can describe", EventFacts{Type: ChangeDeleted}, "", false},
		{"Google's delete with a time and no row", EventFacts{Type: ChangeDeleted, Start: soon, Cancelled: true, Fingerprint: "a"}, TierNow, true},
		{"Google's delete far off goes to the digest", EventFacts{Type: ChangeDeleted, Start: far, Cancelled: true, Fingerprint: "a"}, TierToday, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, told := DecideEvent(now, tc.e)
			if told != tc.told || got.Tier != tc.want {
				t.Fatalf("DecideEvent = %+v, told %v, want %s, %v", got, told, tc.want, tc.told)
			}
			if told && got.By != ByCalendar {
				t.Fatalf("decided by %s", got.By)
			}
		})
	}
}
