package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/pimwatch"
)

const (
	// pimWatchMaxDuration is two ticks of a backlog: the watcher stops starting pages short of it
	// and the next tick goes on, so a large mailbox never runs into the bound.
	pimWatchMaxDuration  = 2 * time.Minute
	pimDigestMaxDuration = 5 * time.Minute
)

var errPIMWatchUnconfigured = errors.New("the mail watch has no PIM sidecar to read")

// PIMWatcher runs one account's tick. The live *pimwatch.Watcher satisfies it.
type PIMWatcher interface {
	Tick(ctx context.Context, in pimwatch.TickInput) (pimwatch.Outcome, error)
}

// PIMWatchHandler runs one account's watch. It adds no judgment of its own: pimwatch decides, and
// this file carries the run's text to the dispatcher. A nil Watcher is a deployment that lost its
// sidecar, which fails the run and so pauses the task with the reason. Locale picks the language
// of that report, and a nil one reads as English.
type PIMWatchHandler struct {
	Watcher PIMWatcher
	Locale  pimwatch.Locale
}

// Meta declares the kind. A missed tick is not worth a catch-up, because the next one reads
// everything from the cursor anyway.
func (PIMWatchHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindPIMWatch, MaxDuration: pimWatchMaxDuration}
}

// Run runs one tick.
func (h PIMWatchHandler) Run(ctx context.Context, job Job) (string, error) {
	accountID := watchedAccount(job.Payload)
	summary, err := h.tick(ctx, job, accountID)
	if err != nil {
		return summary, pimwatch.WatchPauseReport(reportLanguage(ctx, h.Locale), accountID, err)
	}
	return summary, nil
}

func (h PIMWatchHandler) tick(ctx context.Context, job Job, accountID string) (string, error) {
	if h.Watcher == nil {
		return "", errPIMWatchUnconfigured
	}
	if accountID == "" {
		return "", errors.New("the watch task names no account: enable the watch again")
	}
	out, err := h.Watcher.Tick(ctx, pimwatch.TickInput{
		IdentityID: identityctx.IdentityID(ctx), AccountID: accountID, RunID: job.RunID,
	})
	tellOutcome(job, out)
	return out.Summary, err
}

func watchedAccount(raw []byte) string {
	var payload pimwatch.TaskPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return payload.AccountID
}

func reportLanguage(ctx context.Context, locale pimwatch.Locale) pimwatch.Lang {
	if locale == nil {
		return pimwatch.LangEN
	}
	return locale.Language(ctx, identityctx.IdentityID(ctx))
}

// tellOutcome hands the dispatcher what the person reads. It always tells, even when there is
// nothing to say: a handler that never told gets its summary pushed, and a quiet tick must stay
// quiet. The attacker's text is framed in the record only, because the record is what the model
// reads next turn. The notification is plain text for a person.
func tellOutcome(job Job, out pimwatch.Outcome) {
	if job.Tell == nil {
		return
	}
	record := out.Head
	if out.Refs != "" {
		record += "\n\n" + out.Refs
	}
	if out.Quoted != "" {
		record = fmt.Sprintf("%s\n\n%s", record, agent.FrameUntrustedPromptContent(agent.WatchRecordSource, out.Quoted))
	}
	job.Tell(out.Notice, record)
}

// PIMDigester writes an identity's digest. The live *pimwatch.Digester satisfies it.
type PIMDigester interface {
	Run(ctx context.Context, identityID string) (pimwatch.Outcome, error)
}

// PIMDigestHandler writes the identity's daily digest.
type PIMDigestHandler struct {
	Digester PIMDigester
	Locale   pimwatch.Locale
}

// Meta declares the kind. A digest missed while the process was down is worth running on recovery:
// it is the only delivery for everything below `now`.
func (PIMDigestHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindPIMDigest, MaxDuration: pimDigestMaxDuration, ReschedulesOnRecovery: true}
}

// Run writes one digest.
func (h PIMDigestHandler) Run(ctx context.Context, job Job) (string, error) {
	summary, err := h.write(ctx, job)
	if err != nil {
		return summary, pimwatch.DigestPauseReport(reportLanguage(ctx, h.Locale), err)
	}
	return summary, nil
}

func (h PIMDigestHandler) write(ctx context.Context, job Job) (string, error) {
	if h.Digester == nil {
		return "", errPIMWatchUnconfigured
	}
	out, err := h.Digester.Run(ctx, identityctx.IdentityID(ctx))
	tellOutcome(job, out)
	return out.Summary, err
}
