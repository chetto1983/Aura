package pimwatch

import "context"

// shadowCount is what a tick tells the ledger of the centroid: counts only.
type shadowCount struct {
	on                      bool
	answered, agreed, empty int
}

// shadow fills each mail draft of a page with what the scoring needs: the four facts, the
// per-sender memory's tier and the centroid's answer
// (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md, decisions 7 to 9). Nothing here
// changes a verdict, and a failure only leaves part of the shadow empty: a tick never fails, or
// waits on a retry, because of it.
func (w *Watcher) shadow(ctx context.Context, t *tick, mails []Change, drafts []VerdictDraft) {
	if len(mails) == 0 {
		return
	}
	keys := make([]string, len(mails))
	for i, mail := range mails {
		drafts[i].Shadow = &Shadow{Facts: factsOf(mail.Signals)}
		keys[i], _ = SenderKey(mail.From)
	}
	if memory, err := w.Store.SenderMemory(ctx, t.IdentityID, keys); err == nil {
		for i := range drafts {
			drafts[i].Shadow.SenderMemory = drafts[i].Decision.Tier
			if tier, ok := memory[keys[i]]; ok {
				drafts[i].Shadow.SenderMemory = tier
			}
		}
	}
	if w.Centroid == nil {
		return
	}
	asks := make([]ShadowMail, len(mails))
	for i, mail := range mails {
		asks[i] = ShadowMail{From: mail.From, Subject: mail.Subject, Facts: drafts[i].Shadow.Facts}
	}
	answers, err := w.Centroid.Judge(ctx, t.IdentityID, asks)
	if err != nil || len(answers) != len(mails) {
		t.shadow.empty += len(mails)
		return
	}
	for i, answer := range answers {
		drafts[i].Shadow.Centroid, drafts[i].Shadow.Margin = answer.Tier, answer.Margin
		switch answer.Tier {
		case "":
			t.shadow.empty++
		case drafts[i].Decision.Tier:
			t.shadow.answered++
			t.shadow.agreed++
		default:
			t.shadow.answered++
		}
	}
}
