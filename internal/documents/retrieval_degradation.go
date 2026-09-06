package documents

import (
	"log/slog"
	"sync"
	"time"
)

// retrieval_degradation.go gives the cascade's degradations a voice.
//
// A degraded retrieval is a successful call by design (see RetrievalStatus): the turn never
// fails because ArcadeDB or the embedder is down. The cost of that design, measured live on
// 2026-09-06, is that a permanently broken passage index answered every document_search with
// `arcadedb_unavailable` and empty documents while the server logged NOTHING — the wiring had
// discarded the index error at boot and the fused read discarded it again on every call. The
// operator saw an agent that "cannot find documents"; the engine was healthy and the reason
// was one dropped error away.
//
// So the reason travels on the wire AND the cause is stated in the log. It is rate-limited
// because document_search runs on agent turns: logging every failure is how one
// misconfiguration becomes a log flood.

// degradationRestateAfter is how long an UNCHANGED degradation stays quiet before it is
// restated. The restatement matters as much as the suppression: an index that has been
// broken for an hour should still be visible in the last minute of logs, not only the first.
const degradationRestateAfter = time.Minute

// degradationLog remembers what it last said about each reason so an unchanged cause is
// stated once, then again only after degradationRestateAfter.
type degradationLog struct {
	mu   sync.Mutex
	last map[string]degradationEntry
	now  func() time.Time // nil ⇒ time.Now; a test injects its own clock
}

type degradationEntry struct {
	cause string
	at    time.Time
}

func (l *degradationLog) clock() time.Time {
	if l == nil || l.now == nil {
		return time.Now()
	}
	return l.now()
}

// state reports whether this (reason, cause) pair should be logged now, recording it when so.
// A nil receiver never suppresses: a retriever built without a log still says everything once
// per call rather than silently swallowing, which is the failure this file exists to end.
func (l *degradationLog) state(reason, cause string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if prev, ok := l.last[reason]; ok && prev.cause == cause && now.Sub(prev.at) < degradationRestateAfter {
		return false
	}
	if l.last == nil {
		l.last = map[string]degradationEntry{}
	}
	l.last[reason] = degradationEntry{cause: cause, at: now}
	return true
}

// warn states a degradation's cause, rate-limited by state.
func (l *degradationLog) warn(reason, cause, identityID string) {
	if !l.state(reason, cause) {
		return
	}
	slog.Warn("documents: retrieval degraded to cards",
		"reason", reason, "cause", cause, "identity", identityID)
}
