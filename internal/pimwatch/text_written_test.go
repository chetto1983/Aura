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
	lines := []Line{
		mailLine("m1", "martina@snc.it", "Martina SNC", "Re: Tagliando", TierNow),
		mailLine("m2", "martina@snc.it", "Martina SNC", "Re: Re: Tagliando", TierToday),
		mailLine("m3", "pier@gmail.com", "PierMarSync", "richiesta offerta", TierToday),
		mailLine("m4", "no@github.com", "GitHub", "Run failed", TierToday),
		mailLine("m5", "pier@gmail.com", "PierMarSync", "Evento annullato", TierToday),
		mailLine("m6", "survey@zoom.it", "", "Sondaggio", TierToday),
	}
	written := []writtenLine{
		{Section: sectionKnow, Items: []int{5}, Text: "riunione annullata, niente in agenda"},
		{Section: sectionDo, Items: []int{1, 2}, Text: "risposta sul tagliando Corolla, 2 mail"},
		{Section: sectionDo, Items: []int{3}, Text: "richiesta offerta"},
		{Section: sectionLook, Items: []int{4}, Text: "CI Skills fallita su master"},
	}

	out := renderWritten(LangIT, time.UTC, lines, written, 0, unread{})

	want := strings.Join([]string{
		"Riepilogo di posta e calendario",
		"Da fare",
		"1. Martina SNC: risposta sul tagliando Corolla, 2 mail",
		"2. PierMarSync: richiesta offerta",
		"Da guardare",
		"3. GitHub: CI Skills fallita su master",
		"Da sapere",
		"4. PierMarSync: riunione annullata, niente in agenda",
		"Un'altra voce non chiede nulla.",
	}, "\n")
	if out.Notice != want {
		t.Fatalf("digest:\n%s\nwant:\n%s", out.Notice, want)
	}
}

func TestAWrittenDigestHasEnglishHeadings(t *testing.T) {
	lines := []Line{
		mailLine("m1", "a@b.it", "Anna", "uno", TierToday), mailLine("m2", "c@d.it", "Luca", "due", TierToday),
		mailLine("m3", "e@f.it", "Sara", "tre", TierToday), mailLine("m4", "g@h.it", "Ugo", "quattro", TierToday),
	}
	written := []writtenLine{
		{Section: sectionDo, Items: []int{1}, Text: "act"},
		{Section: sectionLook, Items: []int{2}, Text: "read"},
		{Section: sectionKnow, Items: []int{3}, Text: "aware"},
	}
	out := renderWritten(LangEN, time.UTC, lines, written, 0, unread{})
	mustContain(t, out.Notice, "To do\n1. Anna: act", "Worth a look\n2. Luca: read", "Good to know\n3. Sara: aware", "1 more asks nothing of you.")
}

func TestALineSaysWhoItIsFromAndTheModelCannotChangeThat(t *testing.T) {
	cases := []struct {
		name  string
		items []Line
		want  string
	}{
		{"one", []Line{mailLine("m1", "a@b.it", "Anna", "x", TierToday)}, "1. Anna: testo"},
		{"an address alone", []Line{mailLine("m1", "a@b.it", "", "x", TierToday)}, "1. a@b.it: testo"},
		{"the same person twice", []Line{mailLine("m1", "a@b.it", "", "x", TierToday), mailLine("m2", "A@B.it", "Anna", "y", TierToday)}, "1. Anna: testo"},
		{"two", []Line{mailLine("m1", "a@b.it", "Anna", "x", TierToday), mailLine("m2", "c@d.it", "Luca", "y", TierToday)}, "1. Anna, Luca: testo"},
		{"past two", []Line{
			mailLine("m1", "a@b.it", "Anna", "x", TierToday), mailLine("m2", "c@d.it", "Luca", "y", TierToday),
			mailLine("m3", "e@f.it", "Sara", "z", TierToday), mailLine("m4", "g@h.it", "Ugo", "w", TierToday),
		}, "1. Anna, Luca +2: testo"},
		{"an event with nobody", []Line{{AccountID: "work", Resource: ResourceCalendar, ItemID: "e1", Tier: TierToday}}, "1. testo"},
	}
	for _, c := range cases {
		items := make([]int, 0, len(c.items))
		for i := range c.items {
			items = append(items, i+1)
		}
		out := renderWritten(LangIT, time.UTC, c.items, []writtenLine{{Section: sectionDo, Items: items, Text: "testo"}}, 0, unread{})
		if !strings.Contains(out.Notice, c.want+"\n") && !strings.HasSuffix(out.Notice, c.want) {
			t.Fatalf("%s:\n%s\nwant %q", c.name, out.Notice, c.want)
		}
	}
}

func TestAFailedAuthenticationStaysOnTheLine(t *testing.T) {
	forged := mailLine("m1", "x@evil.it", "Banca", "urgente", TierToday)
	forged.AuthFailed = true
	out := renderWritten(LangIT, time.UTC, []Line{forged}, []writtenLine{{Section: sectionDo, Items: []int{1}, Text: "bonifico"}}, 0, unread{})
	mustContain(t, out.Notice, "1. Banca: bonifico [mittente non autenticato]")
}

func TestAnUntoldNowStandsEvenIfTheModelLeftItOut(t *testing.T) {
	lines := []Line{mailLine("m1", "a@b.it", "Anna", "fermo macchina", TierNow), mailLine("m2", "c@d.it", "Luca", "ciao", TierToday)}
	out := renderWritten(LangIT, time.UTC, lines, []writtenLine{{Section: sectionDo, Items: []int{2}, Text: "saluta"}}, 0, unread{})
	mustContain(t, out.Notice, "Da guardare\n2. Anna <a@b.it>: fermo macchina")
	if strings.Contains(out.Notice, "non chiedono") {
		t.Fatalf("counted as well:\n%s", out.Notice)
	}
}

func TestATenfoldUrgentDayStopsAtTheCapAndSaysHowMany(t *testing.T) {
	lines := make([]Line, 0, maxDigestLines+5)
	for range maxDigestLines + 5 {
		lines = append(lines, mailLine("m", "a@b.it", "", "fermo", TierNow))
	}
	out := renderWritten(LangEN, time.UTC, lines, nil, 0, unread{})
	mustContain(t, out.Notice, "20. a@b.it: fermo", "And 5 more.")
	if strings.Contains(out.Notice, "21. ") {
		t.Fatalf("past the cap:\n%s", out.Notice)
	}
}

func TestWhatWasToldStandsOnlyIfCitedAndIsNeverCounted(t *testing.T) {
	told := mailLine("m1", "a@b.it", "Anna", "invito", TierNow)
	told.Told = true
	quiet := renderWritten(LangIT, time.UTC, []Line{told}, nil, 0, unread{})
	if quiet != (Outcome{}) {
		t.Fatalf("outcome = %+v", quiet)
	}
	cited := renderWritten(LangIT, time.UTC, []Line{told}, []writtenLine{{Section: sectionKnow, Items: []int{1}, Text: "annullata"}}, 0, unread{})
	mustContain(t, cited.Notice, "1. Anna: annullata")
}

func TestWhatTheModelNeverSawIsSaidApart(t *testing.T) {
	lines := []Line{mailLine("m1", "a@b.it", "Anna", "uno", TierToday), mailLine("m2", "c@d.it", "Luca", "due", TierToday)}
	out := renderWritten(LangIT, time.UTC, lines, []writtenLine{{Section: sectionDo, Items: []int{1}, Text: "x"}}, 4, unread{held: []string{"t__old"}})
	mustContain(t, out.Notice, "Un'altra voce non chiede nulla.", "E altre 4 voci.", "Non ho potuto leggere: old.")
}

func TestManyQuietItemsAreCountedInThePlural(t *testing.T) {
	lines := []Line{
		mailLine("m1", "a@b.it", "Anna", "uno", TierToday), mailLine("m2", "c@d.it", "Luca", "due", TierToday),
		mailLine("m3", "e@f.it", "Sara", "tre", TierToday),
	}
	italian := renderWritten(LangIT, time.UTC, lines, nil, 0, unread{})
	mustContain(t, italian.Notice, "Altre 3 voci non chiedono nulla.")
	english := renderWritten(LangEN, time.UTC, lines, nil, 0, unread{})
	mustContain(t, english.Notice, "3 more ask nothing of you.")
}

func TestAMergedLineKeepsEveryAddressUnderItsNumber(t *testing.T) {
	lines := []Line{mailLine("m1", "a@b.it", "Anna", "uno", TierToday), mailLine("m2", "a@b.it", "Anna", "due", TierToday), mailLine("m3", "c@d.it", "Luca", "tre", TierToday)}
	written := []writtenLine{{Section: sectionDo, Items: []int{1, 2}, Text: "x"}, {Section: sectionLook, Items: []int{3}, Text: "y"}}

	out := renderWritten(LangEN, time.UTC, lines, written, 0, unread{})

	want := "1. account=work resource=mail id=m1\n1. account=work resource=mail id=m2\n2. account=work resource=mail id=m3"
	if out.Refs != want {
		t.Fatalf("refs = %q", out.Refs)
	}
	if strings.Contains(out.Head, "Anna") || !strings.Contains(out.Quoted, "1. Anna: x") {
		t.Fatalf("head %q quoted %q", out.Head, out.Quoted)
	}
}
