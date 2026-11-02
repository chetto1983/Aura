package pimwatch

import (
	"strings"
	"testing"
	"time"
)

func mailLine(id, from, name, subject string, tier Tier) Line {
	return Line{AccountID: "work", Resource: ResourceMail, ItemID: id, From: from, FromName: name, Subject: subject, Tier: tier}
}

func TestAWrittenDigestReadsLikeTheSpec(t *testing.T) {
	known := []Line{
		mailLine("m1", "anna@acme.it", "Anna Rossi", "Re: Offerta", TierToday),
		mailLine("m2", "anna@acme.it", "Anna Rossi", "Re: Re: Offerta", TierToday),
		mailLine("m3", "luca@acme.it", "Luca Bianchi", "richiesta di conferma", TierToday),
		mailLine("m4", "ci@acme.it", "Build", "Run failed", TierToday),
		mailLine("m5", "luca@acme.it", "Luca Bianchi", "Evento annullato", TierToday),
		mailLine("m6", "news@acme.it", "", "Sondaggio", TierToday),
	}
	written := []writtenLine{
		{Section: sectionKnow, Items: []int{5}, Text: "riunione annullata, niente in agenda"},
		{Section: sectionDo, Items: []int{1, 2}, Text: "risposta su un'offerta, 2 mail"},
		{Section: sectionDo, Items: []int{3}, Text: "richiesta di conferma"},
		{Section: sectionLook, Items: []int{4}, Text: "build fallita su master"},
	}

	out := renderWritten(LangIT, time.UTC, known, nil, written, 0, unread{})

	want := strings.Join([]string{
		"Riepilogo di posta e calendario",
		"Da fare",
		"1. Anna Rossi <anna@acme.it>: risposta su un'offerta, 2 mail",
		"2. Luca Bianchi <luca@acme.it>: richiesta di conferma",
		"Da guardare",
		"3. Build <ci@acme.it>: build fallita su master",
		"Da sapere",
		"4. Luca Bianchi <luca@acme.it>: riunione annullata, niente in agenda",
		"Un'altra voce non chiede nulla.",
	}, "\n")
	if out.Notice != want {
		t.Fatalf("digest:\n%s\nwant:\n%s", out.Notice, want)
	}
}

func TestAWrittenDigestHasEnglishHeadings(t *testing.T) {
	known := []Line{
		mailLine("m1", "a@b.it", "", "uno", TierToday), mailLine("m2", "c@d.it", "", "due", TierToday),
		mailLine("m3", "e@f.it", "", "tre", TierToday), mailLine("m4", "g@h.it", "", "quattro", TierToday),
	}
	written := []writtenLine{
		{Section: sectionDo, Items: []int{1}, Text: "act"},
		{Section: sectionLook, Items: []int{2}, Text: "read"},
		{Section: sectionKnow, Items: []int{3}, Text: "aware"},
	}
	out := renderWritten(LangEN, time.UTC, known, nil, written, 0, unread{})
	mustContain(t, out.Notice, "To do\n1. a@b.it: act", "Worth a look\n2. c@d.it: read", "Good to know\n3. e@f.it: aware", "1 more asks nothing of you.")
}

// A display name is whatever the sender typed, so a line that showed it alone could read as anyone.
func TestALineSaysWhoItIsFromAndTheModelCannotChangeThat(t *testing.T) {
	cases := []struct {
		name  string
		items []Line
		want  string
	}{
		{"a name that is not the address", []Line{mailLine("m1", "x@evil.it", "Anna Rossi", "x", TierToday)}, "1. Anna Rossi <x@evil.it>: testo"},
		{"an address alone", []Line{mailLine("m1", "a@b.it", "", "x", TierToday)}, "1. a@b.it: testo"},
		{"the same person twice", []Line{mailLine("m1", "a@b.it", "", "x", TierToday), mailLine("m2", "A@B.it", "Anna", "y", TierToday)}, "1. Anna <A@B.it>: testo"},
		{"two", []Line{mailLine("m1", "a@b.it", "", "x", TierToday), mailLine("m2", "c@d.it", "", "y", TierToday)}, "1. a@b.it, c@d.it: testo"},
		{"past two", []Line{
			mailLine("m1", "a@b.it", "", "x", TierToday), mailLine("m2", "c@d.it", "", "y", TierToday),
			mailLine("m3", "e@f.it", "", "z", TierToday), mailLine("m4", "g@h.it", "", "w", TierToday),
		}, "1. a@b.it, c@d.it +2: testo"},
		{"an event with nobody", []Line{{AccountID: "work", Resource: ResourceCalendar, ItemID: "e1", Tier: TierToday}}, "1. testo"},
	}
	for _, c := range cases {
		items := make([]int, 0, len(c.items))
		for i := range c.items {
			items = append(items, i+1)
		}
		out := renderWritten(LangIT, time.UTC, c.items, nil, []writtenLine{{Section: sectionDo, Items: items, Text: "testo"}}, 0, unread{})
		if !strings.Contains(out.Notice, c.want+"\n") && !strings.HasSuffix(out.Notice, c.want) {
			t.Fatalf("%s:\n%s\nwant %q", c.name, out.Notice, c.want)
		}
	}
}

func TestAFailedAuthenticationStaysOnTheLine(t *testing.T) {
	forged := mailLine("m1", "x@evil.it", "", "urgente", TierToday)
	forged.AuthFailed = true
	out := renderWritten(LangIT, time.UTC, []Line{forged}, nil, []writtenLine{{Section: sectionDo, Items: []int{1}, Text: "bonifico"}}, 0, unread{})
	mustContain(t, out.Notice, "1. x@evil.it: bonifico [mittente non autenticato]")
}

func TestWhatIsOwedStandsFirstAsThePIMReadIt(t *testing.T) {
	known := []Line{mailLine("m2", "c@d.it", "", "ciao", TierToday)}
	owed := []Line{mailLine("m1", "a@b.it", "Anna", "fermo macchina", TierNow)}
	written := []writtenLine{{Section: sectionLook, Items: []int{1}, Text: "saluta"}}

	out := renderWritten(LangIT, time.UTC, known, owed, written, 0, unread{})

	mustContain(t, out.Notice, "Da guardare\n1. Anna <a@b.it>: fermo macchina\n2. c@d.it: saluta")
	if out.Refs != "1. account=work resource=mail id=m1\n2. account=work resource=mail id=m2" {
		t.Fatalf("refs = %q", out.Refs)
	}
}

func TestAnUrgentDayStopsAtTheCapAndSaysHowMany(t *testing.T) {
	owed := make([]Line, 0, maxDigestLines+5)
	for range maxDigestLines + 5 {
		owed = append(owed, mailLine("m", "a@b.it", "", "fermo", TierNow))
	}
	out := renderWritten(LangEN, time.UTC, nil, owed, nil, 0, unread{})
	mustContain(t, out.Notice, "20. a@b.it: fermo", "And 5 more.")
	if strings.Contains(out.Notice, "21. ") {
		t.Fatalf("past the cap:\n%s", out.Notice)
	}
}

func TestWhatWasToldStandsOnlyIfCitedAndIsNeverCounted(t *testing.T) {
	told := mailLine("m1", "a@b.it", "", "invito", TierNow)
	told.Told = true
	quiet := renderWritten(LangIT, time.UTC, []Line{told}, nil, nil, 0, unread{})
	if quiet != (Outcome{}) {
		t.Fatalf("outcome = %+v", quiet)
	}
	cited := renderWritten(LangIT, time.UTC, []Line{told}, nil, []writtenLine{{Section: sectionKnow, Items: []int{1}, Text: "annullata"}}, 0, unread{})
	mustContain(t, cited.Notice, "1. a@b.it: annullata")
}

func TestWhatTheModelNeverSawIsSaidApart(t *testing.T) {
	known := []Line{mailLine("m1", "a@b.it", "", "uno", TierToday), mailLine("m2", "c@d.it", "", "due", TierToday)}
	out := renderWritten(LangIT, time.UTC, known, nil, []writtenLine{{Section: sectionDo, Items: []int{1}, Text: "x"}}, 4, unread{held: []string{"t__old"}})
	mustContain(t, out.Notice, "Un'altra voce non chiede nulla.", "E altre 4 voci.", "Non ho potuto leggere: old.")
}

func TestManyQuietItemsAreCountedInThePlural(t *testing.T) {
	known := []Line{
		mailLine("m1", "a@b.it", "", "uno", TierToday), mailLine("m2", "c@d.it", "", "due", TierToday),
		mailLine("m3", "e@f.it", "", "tre", TierToday),
	}
	italian := renderWritten(LangIT, time.UTC, known, nil, nil, 0, unread{})
	mustContain(t, italian.Notice, "Altre 3 voci non chiedono nulla.")
	english := renderWritten(LangEN, time.UTC, known, nil, nil, 0, unread{})
	mustContain(t, english.Notice, "3 more ask nothing of you.")
}

func TestAMergedLineKeepsEveryAddressUnderItsNumber(t *testing.T) {
	known := []Line{mailLine("m1", "a@b.it", "Anna", "uno", TierToday), mailLine("m2", "a@b.it", "Anna", "due", TierToday), mailLine("m3", "c@d.it", "", "tre", TierToday)}
	written := []writtenLine{{Section: sectionDo, Items: []int{1, 2}, Text: "x"}, {Section: sectionLook, Items: []int{3}, Text: "y"}}

	out := renderWritten(LangEN, time.UTC, known, nil, written, 0, unread{})

	want := "1. account=work resource=mail id=m1\n1. account=work resource=mail id=m2\n2. account=work resource=mail id=m3"
	if out.Refs != want {
		t.Fatalf("refs = %q", out.Refs)
	}
	if strings.Contains(out.Head, "Anna") || !strings.Contains(out.Quoted, "1. Anna <a@b.it>: x") {
		t.Fatalf("head %q quoted %q", out.Head, out.Quoted)
	}
}
