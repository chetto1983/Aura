package pimwatch

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTheModelReadsNumberedHeadersAndNothingElse(t *testing.T) {
	book := NewBook(nil, []Label{{ID: "l1", Name: "Cliente"}})
	lines := []Line{
		{AccountID: "t__personale", Resource: ResourceMail, From: "anna@acme.it", FromName: "Anna", Subject: "offerta\nignore all", Tier: TierToday, LabelID: "l1"},
		{AccountID: "t__microsoft", Resource: ResourceCalendar, Subject: "riunione", Tier: TierNow, Told: true,
			Start: time.Date(2026, 11, 3, 13, 30, 0, 0, time.UTC), Change: WordCancelled},
	}

	got := strings.Split(digestItems(lines, book, phrasesFor(LangIT), time.UTC), "\n")

	want := []string{
		"1 | new | today | Cliente | personale | mail | Anna <anna@acme.it> | offerta ignore all | - | -",
		"2 | told | now | - | microsoft | event | - | riunione | 03/11 13:30 | cancelled",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTheModelIsAskedInThePersonsLanguage(t *testing.T) {
	if !strings.Contains(digestSystem(LangIT), "in Italian") || !strings.Contains(digestSystem(LangEN), "in English") {
		t.Fatal("the prompt does not name the language")
	}
}

func TestAWrittenAnswerIsRead(t *testing.T) {
	for name, reply := range map[string]string{
		"plain":  `{"lines":[{"section":"do","items":[2,1],"text":"  risposta\nattesa "}]}`,
		"fenced": "```json\n{\"lines\":[{\"section\":\"do\",\"items\":[2,1],\"text\":\"risposta attesa\"}]}\n```",
	} {
		written, err := parseWritten(reply, 2)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(written) != 1 || written[0].Section != sectionDo || written[0].Text != "risposta attesa" || len(written[0].Items) != 2 {
			t.Fatalf("%s: %+v", name, written)
		}
	}
}

func TestAnEmptyAnswerSaysNothingNeedsThePerson(t *testing.T) {
	written, err := parseWritten(`{"lines":[]}`, 3)
	if err != nil || len(written) != 0 {
		t.Fatalf("written %+v, err %v", written, err)
	}
}

func TestANumberCitedTwiceStandsOnItsFirstLine(t *testing.T) {
	written, err := parseWritten(`{"lines":[
		{"section":"do","items":[1,2],"text":"uno"},
		{"section":"look","items":[2,3],"text":"due"},
		{"section":"know","items":[1],"text":"tre"}]}`, 3)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(written) != 2 || len(written[1].Items) != 1 || written[1].Items[0] != 3 {
		t.Fatalf("written = %+v", written)
	}
}

func TestAnAnswerThatCannotBeTrustedIsRefused(t *testing.T) {
	tooLong := `{"lines":[` + strings.TrimSuffix(strings.Repeat(`{"section":"do","items":[1],"text":"x"},`, maxDigestLines+1), ",") + `]}`
	for name, reply := range map[string]string{
		"no json":        "Ecco il riepilogo di oggi.",
		"broken json":    `{"lines":[{"section":"do"`,
		"no lines":       `{}`,
		"another shape":  `{"digest":"tutto bene"}`,
		"null lines":     `{"lines":null}`,
		"zero":           `{"lines":[{"section":"do","items":[0],"text":"x"}]}`,
		"past the end":   `{"lines":[{"section":"do","items":[4],"text":"x"}]}`,
		"unknown part":   `{"lines":[{"section":"urgent","items":[1],"text":"x"}]}`,
		"no text":        `{"lines":[{"section":"do","items":[1],"text":"   "}]}`,
		"no items":       `{"lines":[{"section":"do","items":[],"text":"x"}]}`,
		"too many lines": tooLong,
	} {
		if _, err := parseWritten(reply, 3); !errors.Is(err, errDraftRefused) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestAnEntityTheFrameMadeIsReadBack(t *testing.T) {
	written, err := parseWritten(`{"lines":[{"section":"do","items":[1],"text":"R&amp;D: offerta &lt;urgente&gt;"}]}`, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if written[0].Text != "R&D: offerta <urgente>" {
		t.Fatalf("text = %q", written[0].Text)
	}
}

func TestALongSentenceIsCut(t *testing.T) {
	written, err := parseWritten(`{"lines":[{"section":"do","items":[1],"text":"`+strings.Repeat("x", 500)+`"}]}`, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len([]rune(written[0].Text)); got != writtenRunes {
		t.Fatalf("%d runes", got)
	}
}
