// Two-tier loop-guard dedup (D-18/A2), split out of budget.go per RESEARCH #1 so
// neither file approaches the 600-LOC no-god-class cap (CLAUDE.md).
//
// WHY two-tier and WHY result is VETO-only: the original (name,args,result)-in-hash
// design was FAIL-OPEN — any tool returning a volatile field (timestamp, page-token,
// request-id) never produces a repeating triple, so a runaway loop is never detected
// and slips past the guard. A2 reversed it: the primary fingerprint is
// sha256(name + canonical_json(args)) ONLY, checked in BeforeToolCall BEFORE the tool
// re-executes (so repeated side effects are blocked). A bounded result preview is
// recorded later by AfterToolResult and used ONLY as a progress VETO: if the args
// repeat but the latest result preview CHANGED, the next BeforeToolCall suppresses
// dedup and treats it as progress. Volatile-result tools therefore fail SAFE (look
// like progress) instead of fail-open.
//
// CALLER-CANONICALIZES CONTRACT (B2): BeforeToolCall and AfterToolResult both accept
// PRE-CANONICALIZED argsCanonicalJSON []byte. The CALLER runs
// internal/canonicaljson.Marshal(args) before calling — these methods do NOT call
// canonicaljson internally. Plan 05 Task 2 (the LoopAgent caller) and this callee
// agree on this contract; keeping canonicalization in the caller lets the loop hash
// once per turn and avoids a hidden re-serialization.
package agent

import (
	"crypto/sha256"
	"strings"
)

// dedupRing is a per-branch ring buffer of recent tool-call fingerprints plus the
// latest result preview per fingerprint. Capacity is >= max(window, 4): WINDOW=3
// governs period-1 (A-A-A) repeat detection, while >=4 slots are needed to observe
// a period-2 ping-pong (A-B-A-B) (D-20).
type dedupRing struct {
	window  int
	entries []fingerprint                     // ring buffer, newest appended; len capped at cap(entries)
	results map[fingerprint][sha256.Size]byte // latest result-preview hash per fingerprint (progress veto)
}

type fingerprint [sha256.Size]byte

// ringCapacity is max(window, 4) — period-2 ping-pong needs >=4 slots (D-20).
func ringCapacity(window int) int {
	if window < 4 {
		return 4
	}
	return window
}

// newDedupRing builds an empty ring sized for the given consecutive-repeat window.
func newDedupRing(window int) *dedupRing {
	if window < 1 {
		window = 1
	}
	c := ringCapacity(window)
	return &dedupRing{
		window:  window,
		entries: make([]fingerprint, 0, c),
		results: make(map[fingerprint][sha256.Size]byte, c),
	}
}

// computeFingerprint is the primary tier: sha256(name + canonical_json(args)).
// The args bytes are assumed already canonical (B2 caller-canonicalizes contract).
func computeFingerprint(name string, argsCanonicalJSON []byte) fingerprint {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write(argsCanonicalJSON)
	var fp fingerprint
	copy(fp[:], h.Sum(nil))
	return fp
}

// push appends fp to the ring, evicting the oldest entry when at capacity.
func (r *dedupRing) push(fp fingerprint) {
	c := cap(r.entries)
	if len(r.entries) < c {
		r.entries = append(r.entries, fp)
		return
	}
	copy(r.entries, r.entries[1:])
	r.entries[c-1] = fp
}

// countConsecutive returns the number of trailing entries equal to fp (period-1).
func (r *dedupRing) countConsecutive(fp fingerprint) int {
	n := 0
	for i := len(r.entries) - 1; i >= 0; i-- {
		if r.entries[i] != fp {
			break
		}
		n++
	}
	return n
}

// isPingPong reports an A-B-A-B period-2 cycle ending at the would-be next fp==A:
// the last three entries are A,B,A (so appending A makes A,B,A,B). period-2 (D-20).
func (r *dedupRing) isPingPong(fp fingerprint) bool {
	n := len(r.entries)
	if n < 3 {
		return false
	}
	a, b, a2 := r.entries[n-3], r.entries[n-2], r.entries[n-1]
	return a == fp && a2 == fp && a == a2 && b != fp
}

// BeforeToolCall is the PRE-EXECUTION dedup gate (D-18). The caller passes the
// already-canonical args bytes (B2). It returns (true, "dedup") when the same
// fingerprint has repeated to the window threshold (period-1) OR forms a period-2
// ping-pong AND no progress veto applies — i.e. the loop should terminate before
// the side effect re-runs. Exempt tools (AURA_LOOP_DEDUP_EXEMPT_TOOLS, D-19) never
// dedup. The fingerprint is recorded so the next call can detect the repeat.
func (b *Budget) BeforeToolCall(name string, argsCanonicalJSON []byte) (dedup bool, reason string) {
	if _, exempt := b.exemptTools[name]; exempt {
		b.dedupRing.push(computeFingerprint(name, argsCanonicalJSON))
		return false, ""
	}
	fp := computeFingerprint(name, argsCanonicalJSON)
	r := b.dedupRing

	// Progress veto (D-18 fail-safe): if this fingerprint was seen and its last
	// recorded result preview marked progress (cleared from results), do not dedup.
	_, resultStillStale := r.results[fp]

	period1 := r.countConsecutive(fp)+1 >= r.window
	period2 := r.isPingPong(fp)

	r.push(fp)

	if (period1 || period2) && resultStillStale {
		return true, "dedup"
	}
	return false, ""
}

// AfterToolResult records the bounded result preview for the fingerprint as a
// PROGRESS VETO (D-18). The caller passes the same already-canonical args (B2).
// If the result preview for a repeated fingerprint CHANGED since last time, the
// veto is set (progress) so the next BeforeToolCall suppresses dedup; if it is
// unchanged, the stale marker stays so period-1/period-2 repeat can terminate.
// resultPreview is truncated to the budget's resultCap before hashing (A7).
func (b *Budget) AfterToolResult(name string, argsCanonicalJSON, resultPreview []byte) {
	if _, exempt := b.exemptTools[name]; exempt {
		return
	}
	fp := computeFingerprint(name, argsCanonicalJSON)
	r := b.dedupRing

	preview := resultPreview
	if b.resultCap > 0 && len(preview) > b.resultCap {
		preview = preview[:b.resultCap]
	}
	h := sha256.Sum256(preview)

	prev, seen := r.results[fp]
	if seen && prev != h {
		// Result changed for the same args → progress. Clear the stale marker so
		// the next BeforeToolCall does NOT dedup (fail-safe veto).
		delete(r.results, fp)
		return
	}
	// First sighting or unchanged result → mark/keep stale so repeats can dedup.
	r.results[fp] = h
}

// parseExemptTools turns the AURA_LOOP_DEDUP_EXEMPT_TOOLS CSV into a lookup set
// (D-19). Blank entries are dropped; surrounding whitespace is trimmed.
func parseExemptTools(csv string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, raw := range strings.Split(csv, ",") {
		name := strings.TrimSpace(raw)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}
