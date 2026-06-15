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
	"os"
	"strings"
	"sync"
)

// dedupRing is a per-branch ring buffer of recent tool-call fingerprints plus the
// latest result preview per fingerprint. Capacity is >= max(window, 4): WINDOW=3
// governs period-1 (A-A-A) repeat detection, while >=4 slots are needed to observe
// a period-2 ping-pong (A-B-A-B) (D-20).
type dedupRing struct {
	mu      sync.Mutex
	window  int
	entries []fingerprint               // ring buffer, newest appended; len capped at cap(entries)
	results map[fingerprint]resultTrack // per-fingerprint result-preview progress tracking (veto)
}

// resultTrack records the latest result-preview hash for a fingerprint plus how
// many CONSECUTIVE times that exact hash has repeated. A changing result resets
// repeats to 0 (progress veto, D-18): dedup only fires when the result has been
// stable across the repeat window, so volatile-result tools fail SAFE.
type resultTrack struct {
	hash    [sha256.Size]byte
	repeats int
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
		results: make(map[fingerprint]resultTrack, c),
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
// Caller must hold r.mu.
func (r *dedupRing) push(fp fingerprint) {
	c := cap(r.entries)
	if len(r.entries) < c {
		r.entries = append(r.entries, fp)
		return
	}
	evicted := r.entries[0]
	copy(r.entries, r.entries[1:])
	r.entries[c-1] = fp
	if !r.containsEntry(evicted) {
		delete(r.results, evicted)
	}
}

// countConsecutive returns the number of trailing entries equal to fp (period-1).
// Caller must hold r.mu.
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
// Caller must hold r.mu.
func (r *dedupRing) isPingPong(fp fingerprint) bool {
	n := len(r.entries)
	if n < 3 {
		return false
	}
	a, b, a2 := r.entries[n-3], r.entries[n-2], r.entries[n-1]
	return a == fp && a2 == fp && b != fp
}

// isRepeatedCycle reports a period-3+ loop that has already completed two stable
// cycles and is about to start a third. Caller must hold r.mu.
func (r *dedupRing) isRepeatedCycle(fp fingerprint) bool {
	n := len(r.entries)
	for period := 3; period*2 <= n; period++ {
		if r.entries[n-period] != fp {
			continue
		}
		if r.equalBlocks(n-period*2, n-period, period) && r.stableBlock(n-period, period) {
			return true
		}
	}
	return false
}

func (r *dedupRing) equalBlocks(a, b, n int) bool {
	for i := 0; i < n; i++ {
		if r.entries[a+i] != r.entries[b+i] {
			return false
		}
	}
	return true
}

func (r *dedupRing) stableBlock(start, n int) bool {
	for i := 0; i < n; i++ {
		track, seen := r.results[r.entries[start+i]]
		if !seen || track.repeats < 1 {
			return false
		}
	}
	return true
}

func (r *dedupRing) containsEntry(fp fingerprint) bool {
	for _, entry := range r.entries {
		if entry == fp {
			return true
		}
	}
	return false
}

// BeforeToolCall is the PRE-EXECUTION dedup gate (D-18). The caller passes the
// already-canonical args bytes (B2). It returns (true, "dedup") when the same
// fingerprint has repeated to the window threshold (period-1) OR forms a period-2
// ping-pong AND no progress veto applies — i.e. the loop should terminate before
// the side effect re-runs. Exempt tools (AURA_LOOP_DEDUP_EXEMPT_TOOLS, D-19) never
// dedup. The fingerprint is recorded so the next call can detect the repeat.
func (b *Budget) BeforeToolCall(name string, argsCanonicalJSON []byte) (dedup bool, reason string) {
	fp := computeFingerprint(name, argsCanonicalJSON)
	r := b.dedupRing
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exempt := b.exemptTools[name]; exempt {
		r.push(fp)
		return false, ""
	}

	period1 := r.countConsecutive(fp)+1 >= r.window
	period2 := r.isPingPong(fp)
	periodN := r.isRepeatedCycle(fp)

	// Progress veto (D-18 fail-safe): dedup only when the recorded result preview
	// for this fingerprint has stayed UNCHANGED for as many repeats as the matched
	// tier requires. A result that keeps changing resets repeats to 0 (see
	// AfterToolResult), so a volatile-result tool looks like progress and fails
	// SAFE, never fail-open.
	//
	// AfterToolResult records the result on FIRST sighting (repeats=0) and bumps
	// repeats on every later same-args/same-result call. So by the time a tier's
	// shape is detected, repeats counts the prior STABLE repeats:
	//   - period-1: the Nth consecutive call needs the result stable across all of
	//     them. By the Nth BeforeToolCall the result was recorded N-1 times, so a
	//     fully stable result has repeats == N-2; period-1 fires at N==window, hence
	//     stableP1 == repeats+2 >= window. This is window-parameterized and aligns
	//     exactly with countConsecutive+1 >= window for every window >= 1.
	//   - period-2: isPingPong is a FIXED period-2 detector (last three == A,B,A).
	//     Its evidence is two stable sightings of A, i.e. repeats(A) >= 1, which is
	//     INDEPENDENT of window. Gating it on the period-1 window threshold (the old
	//     code) wrongly suppressed ping-pong for window >= 4, where repeats+2 >= window
	//     is false even though the period-2 shape is present (WR-03).
	track, seen := r.results[fp]
	stableP1 := seen && track.repeats+2 >= r.window
	stableP2 := seen && track.repeats >= 1

	r.push(fp)

	if (period1 && stableP1) || (period2 && stableP2) || periodN {
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
	r.mu.Lock()
	defer r.mu.Unlock()

	preview := resultPreview
	if b.resultCap > 0 && len(preview) > b.resultCap {
		preview = preview[:b.resultCap]
	}
	h := sha256.Sum256(preview)

	prev, seen := r.results[fp]
	if seen && prev.hash == h {
		// Same args, same result again → one more stable repeat (a real loop).
		r.results[fp] = resultTrack{hash: h, repeats: prev.repeats + 1}
		return
	}
	// First sighting OR the result changed → progress. Reset the repeat counter so
	// the next BeforeToolCall does NOT dedup (fail-safe veto, D-18).
	r.results[fp] = resultTrack{hash: h, repeats: 0}
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

// ExemptToolsFromEnv returns the AURA_LOOP_DEDUP_EXEMPT_TOOLS allowlist plus any
// extra tool names, as a fresh set (D-19). It lets a caller compose the operator's
// env exemptions with its own (e.g. the dry-run tool) and pass the result to
// NewBudget via BudgetOptions.ExemptTools — no process-global env mutation (WR-04).
func ExemptToolsFromEnv(extra ...string) map[string]struct{} {
	out := parseExemptTools(os.Getenv(envDedupExemptTools))
	for _, name := range extra {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}
