package swarm

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// delegation_card_test.go is the daemon-free coverage for capRunes,
// DelegationRecordCard, TelegramDelegationMessage (both the N=1 locked
// shapes and the N>1 fan-out block) and DelegationReportMarkdown (51-11).

func TestCapRunes(t *testing.T) {
	if got := capRunes("abc", 10); got != "abc" {
		t.Fatalf("capRunes(short, 10) = %q, want unchanged", got)
	}
	if got := capRunes("", 10); got != "" {
		t.Fatalf("capRunes(empty, 10) = %q, want empty", got)
	}

	long := "abcdefghijklmnopqrstuvwxyz"
	got := capRunes(long, 10)
	if n := utf8.RuneCountInString(got); n != 10 {
		t.Fatalf("capRunes(long, 10) = %q (%d runes), want exactly 10", got, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("capRunes(long, 10) = %q, want a trailing ellipsis rune", got)
	}
}

func TestCapRunesMultibyteNeverProducesInvalidUTF8(t *testing.T) {
	// Mix of multibyte runes (CJK, box-drawing, an emoji) so a naive byte-index
	// cut would split one mid-character.
	s := strings.Repeat("界️é★", 20)
	total := utf8.RuneCountInString(s)
	for n := 0; n <= total+5; n++ {
		got := capRunes(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("capRunes(s, %d) produced invalid UTF-8: %q", n, got)
		}
	}
}

func TestDelegationRecordCard(t *testing.T) {
	elapsed := 90 * time.Second

	t.Run("ok", func(t *testing.T) {
		r := ChildReport{ChildID: "w1-abc", Status: StatusOK, Goal: "Summarize the quarterly report", Summary: "Done, 12 pages summarized."}
		card := DelegationRecordCard(r, elapsed, "")
		if !strings.HasPrefix(card, "✅ ") {
			t.Fatalf("ok card missing success glyph: %q", card)
		}
		if !strings.Contains(card, "Summarize the quarterly report") {
			t.Fatalf("ok card missing goal: %q", card)
		}
		if !strings.Contains(card, "w1-abc") {
			t.Fatalf("ok card missing child id: %q", card)
		}
		if !strings.Contains(card, "Done, 12 pages summarized.") {
			t.Fatalf("ok card missing summary: %q", card)
		}
		if strings.Contains(card, "Report completo") {
			t.Fatalf("ok card with empty artifactName should carry no artifact line: %q", card)
		}
	})

	t.Run("ok with artifact pointer", func(t *testing.T) {
		r := ChildReport{ChildID: "w1-abc", Status: StatusOK, Goal: "goal", Summary: "summary"}
		card := DelegationRecordCard(r, elapsed, "w1-abc.md")
		if !strings.Contains(card, "w1-abc.md") {
			t.Fatalf("ok card with a non-empty artifactName must name it: %q", card)
		}
	})

	t.Run("failed", func(t *testing.T) {
		r := ChildReport{ChildID: "w1", Status: StatusFailed, Goal: "goal", Error: "tool exploded"}
		card := DelegationRecordCard(r, elapsed, "")
		if !strings.HasPrefix(card, "❌ ") {
			t.Fatalf("failed card missing failure glyph: %q", card)
		}
		if !strings.Contains(card, "tool exploded") {
			t.Fatalf("failed card missing error text: %q", card)
		}
	})

	t.Run("dead_letter", func(t *testing.T) {
		r := ChildReport{ChildID: "w1", Status: StatusDeadLetter, Goal: "goal", Attempts: 8}
		card := DelegationRecordCard(r, elapsed, "")
		if !strings.HasPrefix(card, "⚠️ ") {
			t.Fatalf("dead_letter card missing warning glyph: %q", card)
		}
		if !strings.Contains(card, "8") {
			t.Fatalf("dead_letter card missing the attempt count: %q", card)
		}
	})

	t.Run("empty report still renders a non-empty card", func(t *testing.T) {
		r := ChildReport{ChildID: "w1", Status: StatusOK}
		card := DelegationRecordCard(r, elapsed, "")
		if card == "" {
			t.Fatal("card for a report with no summary must not be empty")
		}
	})

	t.Run("multibyte goal never produces invalid UTF-8", func(t *testing.T) {
		goal := strings.Repeat("界", 200)
		r := ChildReport{ChildID: "w1", Status: StatusOK, Goal: goal, Summary: "s"}
		card := DelegationRecordCard(r, elapsed, "")
		if !utf8.ValidString(card) {
			t.Fatal("card with a multibyte goal produced invalid UTF-8")
		}
		if n := utf8.RuneCountInString(strings.SplitN(card, "\n", 2)[0]); n > maxCardGoalRunes+2 {
			// +2: the glyph itself plus the separating space are on the same line.
			t.Fatalf("ok card's first line is %d runes, goal cap is %d", n, maxCardGoalRunes)
		}
	})
}

func TestDelegationReportMarkdown(t *testing.T) {
	longSummary := strings.Repeat("s", 5000)
	r := ChildReport{ChildID: "w1", Status: StatusOK, Goal: "the goal", Summary: longSummary}
	md := DelegationReportMarkdown(r, 90*time.Second)
	if !strings.Contains(md, longSummary) {
		t.Fatal("report markdown must carry the WHOLE summary, uncapped")
	}
	if !strings.Contains(md, "the goal") || !strings.Contains(md, "w1") {
		t.Fatalf("report markdown missing goal/child id heading: %q", md[:200])
	}

	failed := ChildReport{ChildID: "w2", Status: StatusFailed, Goal: "g", Error: strings.Repeat("e", 5000)}
	mdFailed := DelegationReportMarkdown(failed, time.Minute)
	if !strings.Contains(mdFailed, failed.Error) {
		t.Fatal("report markdown must carry the WHOLE error text, uncapped")
	}
}

func TestTelegramDelegationMessage(t *testing.T) {
	t.Run("nil reports", func(t *testing.T) {
		msg := TelegramDelegationMessage(nil)
		if msg == "" || !strings.HasSuffix(msg, delegationClosingLine) {
			t.Fatalf("TelegramDelegationMessage(nil) = %q, want a non-empty message ending in the closing line", msg)
		}
	})

	t.Run("empty reports", func(t *testing.T) {
		msg := TelegramDelegationMessage([]ChildReport{})
		if msg == "" || !strings.HasSuffix(msg, delegationClosingLine) {
			t.Fatalf("TelegramDelegationMessage([]) = %q, want a non-empty message ending in the closing line", msg)
		}
	})

	t.Run("single ok is the locked shape", func(t *testing.T) {
		msg := TelegramDelegationMessage([]ChildReport{{Status: StatusOK, Goal: "goal", Summary: "summary"}})
		want := "✅ Worker completato: goal\n\nsummary\n\n" + delegationClosingLine
		if msg != want {
			t.Fatalf("single ok message =\n%q\nwant\n%q", msg, want)
		}
	})

	t.Run("single failed is the locked shape", func(t *testing.T) {
		msg := TelegramDelegationMessage([]ChildReport{{Status: StatusFailed, Goal: "goal", Error: "boom"}})
		want := "❌ Worker fallito: goal\n\nboom\n\n" + delegationClosingLine
		if msg != want {
			t.Fatalf("single failed message =\n%q\nwant\n%q", msg, want)
		}
	})

	t.Run("single dead_letter carries no summary paragraph", func(t *testing.T) {
		msg := TelegramDelegationMessage([]ChildReport{{Status: StatusDeadLetter, Goal: "goal", Attempts: 8}})
		want := "⚠️ Worker non consegnato dopo 8 tentativi: goal\n\n" + delegationClosingLine
		if msg != want {
			t.Fatalf("dead_letter message =\n%q\nwant\n%q", msg, want)
		}
	})

	t.Run("fanout is ordered by goal index", func(t *testing.T) {
		reports := []ChildReport{
			{GoalIndex: 1, Status: StatusFailed, Goal: "second"},
			{GoalIndex: 0, Status: StatusOK, Goal: "first"},
		}
		msg := TelegramDelegationMessage(reports)
		firstLine := strings.SplitN(msg, "\n", 2)[0]
		if !strings.Contains(firstLine, "first") {
			t.Fatalf("fanout message not ordered by goal index, first line = %q", firstLine)
		}
		if !strings.HasSuffix(msg, delegationClosingLine) {
			t.Fatalf("fanout message must end with the closing line: %q", msg)
		}
	})

	t.Run("all-failed carries no batch verdict, one glyph per worker", func(t *testing.T) {
		reports := []ChildReport{
			{GoalIndex: 0, Status: StatusFailed, Goal: "a"},
			{GoalIndex: 1, Status: StatusFailed, Goal: "b"},
			{GoalIndex: 2, Status: StatusFailed, Goal: "c"},
		}
		msg := TelegramDelegationMessage(reports)
		if got := strings.Count(msg, "❌"); got != 3 {
			t.Fatalf("all-failed message carries %d failure glyphs, want 3: %q", got, msg)
		}
	})

	t.Run("stalled uses the clock glyph and the bloccato label", func(t *testing.T) {
		msg := TelegramDelegationMessage([]ChildReport{
			{GoalIndex: 0, Status: StatusOK, Goal: "a"},
			{GoalIndex: 1, Status: StatusStalled, Goal: "b"},
		})
		if !strings.Contains(msg, "⏱ b: bloccato") {
			t.Fatalf("stalled fanout line missing the clock glyph / bloccato label: %q", msg)
		}
	})

	t.Run("fanout budget holds for N in 2..12 with 500-rune goals", func(t *testing.T) {
		for n := 2; n <= 12; n++ {
			var reports []ChildReport
			for i := 0; i < n; i++ {
				reports = append(reports, ChildReport{GoalIndex: i, Status: StatusOK, Goal: strings.Repeat("x", 500)})
			}
			lines := telegramFanoutLines(reports)
			body := strings.Join(lines, "\n")
			if got := utf8.RuneCountInString(body); got > maxCardSummaryRunes {
				t.Fatalf("N=%d: fanout body is %d runes, want <= %d", n, got, maxCardSummaryRunes)
			}

			rendered := len(lines)
			dropped := 0
			last := lines[len(lines)-1]
			if strings.HasPrefix(last, "+") {
				var parsed int
				if _, err := fmt.Sscanf(last, "+%d altri", &parsed); err == nil {
					dropped = parsed
					rendered--
				}
			}
			if rendered+dropped != n {
				t.Fatalf("N=%d: rendered(%d)+dropped(%d) != N(%d), lines=%v", n, rendered, dropped, n, lines)
			}
		}
	})
}
