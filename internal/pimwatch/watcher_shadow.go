package pimwatch

import (
	"context"
	"log/slog"
)

// shadowCount is what a tick tells the ledger of the centroid: counts only.
type shadowCount struct {
	on                      bool
	answered, agreed, empty int
	// failed stops asking the centroid after its first failure in the tick, so a hung embedder
	// holds the tick's alerts one shadowTimeout, not one per page.
	failed bool
}

// shadow fills each mail draft of a page with what the scoring needs: the four facts, the
// per-sender memory's tier and the centroid's answer
// (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md, decisions 7 to 9). Nothing here
// changes a verdict, and a failure only leaves part of the shadow empty: a tick never fails, or
// waits on a retry, because of it. It compares with the ladder's own tier, which the decision model
// may change after it.
func (w *Watcher) shadow(ctx context.Context, t *tick, mails []Change, drafts []VerdictDraft) {
	if len(mails) == 0 {
		return
	}
	keys := make([]string, len(mails))
	for i, mail := range mails {
		drafts[i].Shadow = &Shadow{Facts: factsOf(mail.Signals)}
		keys[i], _ = SenderKey(mail.From)
	}
	if memory, err := w.Store.SenderMemory(ctx, t.IdentityID, keys); err != nil {
		slog.Warn("mail watch: the sender memory was not read for the shadow", "error", err)
	} else {
		for i := range drafts {
			drafts[i].Shadow.SenderMemory = drafts[i].Ladder.Tier
			if tier, ok := memory[keys[i]]; ok {
				drafts[i].Shadow.SenderMemory = tier
			}
		}
	}
	if w.Centroid == nil {
		return
	}
	if t.shadow.failed {
		t.shadow.empty += len(mails)
		return
	}
	asks := make([]ShadowMail, len(mails))
	for i, mail := range mails {
		asks[i] = ShadowMail{From: mail.From, Subject: mail.Subject, Facts: drafts[i].Shadow.Facts}
	}
	answers, err := w.Centroid.Judge(ctx, t.IdentityID, asks)
	if err != nil || len(answers) != len(mails) {
		t.shadow.failed = true
		t.shadow.empty += len(mails)
		return
	}
	for i, answer := range answers {
		drafts[i].Shadow.Centroid, drafts[i].Shadow.Margin = answer.Tier, answer.Margin
		switch answer.Tier {
		case "":
			t.shadow.empty++
		case drafts[i].Ladder.Tier:
			t.shadow.answered++
			t.shadow.agreed++
		default:
			t.shadow.answered++
		}
	}
}
