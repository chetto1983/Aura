package arcadedb

import "log/slog"

// memory_recall_drops.go gives a thinned recall a voice.
//
// Both recall paths walk a candidate list and read each candidate's conversation window. A
// window that fails to read is skipped so one bad conversation cannot fail the whole recall
// — the right call, and the reason the `continue` is there. What it cost until 2026-09-06 is
// that a recall which lost half its evidence to a failing engine was indistinguishable, to
// the model and to the operator, from a memory that simply held less.
//
// The counter exists rather than a log line at the skip because the skip is inside a loop
// over candidates: logging there turns one engine failure into one line per candidate per
// recall. One line per PASS, carrying how many were lost and the last cause, is the same
// information at a bounded cost.
type recallDrops struct {
	count int
	last  error
}

func (d *recallDrops) record(err error) {
	d.count++
	d.last = err
}

// report states the loss once, naming the mode so an operator can tell which path thinned.
func (d recallDrops) report(mode string) {
	if d.count == 0 {
		return
	}
	slog.Warn("arcadedb: recall skipped conversations it could not read",
		"mode", mode, "dropped", d.count, "err", d.last)
}
