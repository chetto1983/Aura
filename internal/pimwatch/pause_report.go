package pimwatch

import (
	"errors"
	"fmt"
)

// pausingError gives a failed run the words a person reads if it turns out to pause the task. Only
// the scheduler knows that, after the run, so they travel on the error, and the cause stays
// underneath for the ledger, the retry and the silence it may ask for.
type pausingError struct {
	cause  error
	report string
}

func (e *pausingError) Error() string { return e.cause.Error() }

func (e *pausingError) Unwrap() error { return e.cause }

// PauseReport is the scheduler's text for the run that pauses the task.
func (e *pausingError) PauseReport() string { return e.report }

type pausePhrases struct {
	resource  string // account, resource
	watch     string // account
	digest    string
	anAccount string
}

var pauseTexts = map[Lang]pausePhrases{
	LangIT: {
		resource:  "Ho messo in pausa il controllo di %[1]s: non riesco a leggere %[2]s. Apri l'account nel cockpit per vedere cosa serve e riprenderlo.",
		watch:     "Ho messo in pausa il controllo di %[1]s dopo troppi errori di fila. Apri l'account nel cockpit per vedere cosa serve e riprenderlo.",
		digest:    "Ho messo in pausa il riepilogo di posta e calendario dopo troppi errori di fila. Riparte quando riaccendi il controllo di un account.",
		anAccount: "un account",
	},
	LangEN: {
		resource:  "I paused the watch on %[1]s: I cannot read %[2]s. Open the account in the cockpit to see what it needs, and resume it from there.",
		watch:     "I paused the watch on %[1]s after too many failures in a row. Open the account in the cockpit to see what it needs, and resume it from there.",
		digest:    "I paused the mail and calendar digest after too many failures in a row. It starts again when you turn an account's watch back on.",
		anAccount: "an account",
	},
}

func pausePhrasesFor(lang Lang) pausePhrases {
	if p, ok := pauseTexts[lang]; ok {
		return p
	}
	return pauseTexts[LangEN]
}

// WatchPauseReport gives a failed watch run its report: in the identity's language, naming the
// account and what cannot be read, with no code. accountID is empty on a task that lost it.
func WatchPauseReport(lang Lang, accountID string, err error) error {
	p := pausePhrasesFor(lang)
	account := p.anAccount
	if accountID != "" {
		account = accountName(accountID)
	}
	report := fmt.Sprintf(p.watch, account)
	if hard, ok := errors.AsType[*HardFailure](err); ok {
		report = fmt.Sprintf(p.resource, account, phrasesFor(lang).resource(hard.Resource))
	}
	return &pausingError{cause: err, report: report}
}

// DigestPauseReport gives a failed digest run its report.
func DigestPauseReport(lang Lang, err error) error {
	return &pausingError{cause: err, report: pausePhrasesFor(lang).digest}
}
