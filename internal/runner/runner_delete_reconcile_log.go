package runner

import (
	"fmt"
	"strings"
)

// runner_delete_reconcile_log.go shapes what the reconciler SAYS when many identities fail
// for one reason.
//
// Measured 2026-09-06 on a live stack: a single missing ArcadeDB admin credential made
// every identity fail tenant auth, and the two reconcilers rendered that as ten joined
// lines per cycle, once a minute, each one the same sentence with a different UUID in
// front. The cause was stated ten times and understood zero times, and the volume hid the
// one detail that mattered — that all ten shared a cause, so there was one thing to fix.

// collapseJoined renders a joined error as ONE sentence when its parts share a cause, and
// verbatim when they do not. It never drops information a reader needs: the count says how
// wide the failure is, the shared tail says what it is, and one whole part is kept as the
// example so a reader can still see the full shape.
func collapseJoined(err error) string {
	if err == nil {
		return ""
	}
	parts := joinedParts(err)
	if len(parts) < 2 {
		return err.Error()
	}
	shared := commonSuffix(parts)
	if len(shared) < minSharedCauseLen {
		return err.Error()
	}
	return fmt.Sprintf("%d failures share one cause: %s | example: %s", len(parts), shared, parts[0])
}

// minSharedCauseLen keeps the collapse honest: a handful of shared characters (a trailing
// quote, one common word) is not evidence of a common cause, and reporting it as one would
// merge failures a reader has to tell apart.
const minSharedCauseLen = 24

// joinedParts returns the leaves of an errors.Join, or nil when err is not one.
func joinedParts(err error) []string {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	var out []string
	for _, part := range joined.Unwrap() {
		if part != nil {
			out = append(out, part.Error())
		}
	}
	return out
}

// commonSuffix returns the longest tail every message ends with, trimmed forward to a
// separator so the result starts at a word rather than mid-token. Comparing bytes is safe
// here because the cut is then moved to an ASCII boundary.
func commonSuffix(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	shared := messages[0]
	for _, m := range messages[1:] {
		shared = shared[len(shared)-sharedTail(shared, m):]
	}
	// Move the start forward to the first separator so the sentence begins cleanly instead
	// of inside a UUID or half a word.
	if idx := strings.Index(shared, ": "); idx >= 0 {
		shared = shared[idx+2:]
	}
	return strings.TrimSpace(shared)
}

// sharedTail counts the bytes a and b end with in common.
func sharedTail(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}
