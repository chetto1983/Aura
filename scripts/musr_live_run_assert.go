//go:build ignore

// musr_live_run_assert.go is the blocking machine-checkable half of the 01-06 two-identity
// live run (D-18): scripts/musr_live_run.sh invokes it as the final step and propagates its
// exit status, so THIS is what decides whether the phase closes -- the rubric written in
// docs/runbooks/two-identity-live-run.md is recorded as phase evidence and gates nothing
// (internal/agenteval/case.go lines 14-17: a gate that needs a model to decide whether it
// passed cannot be trusted to gate the model; that position is not amended here).
//
// It reads the four artifacts scripts/musr_live_run.sh writes into ONE fixed directory --
// transcript-a.jsonl, transcript-b.jsonl, timings.jsonl (daemon.log is evidence, not an
// assertion input) -- and applies six checks per identity pair, aggregating every failure
// (internal/agenteval/case.go's own philosophy: report every violation at once, never fix
// one and re-discover the next). Any failure exits non-zero naming the assertion and the
// offending transcript path.
//
// GREEN (01-06 Task 2): implements the assertions the RED-phase stub left unwritten. Reuses
// internal/agenteval.Case/Evidence verbatim for the token and required-tool checks (the
// package's exported surface covers exactly this shape); completion, authentication and
// timing overlap are transcript-native concerns internal/agenteval has no equivalent for
// (its Evidence comes from a Postgres-backed conversation record, not an SSE capture), so
// those three are implemented here directly rather than mirrored loosely.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/agenteval"
)

// requiredTools are the three capabilities D-17 requires both identities to exercise on
// their own data: a document search, a memory write, and a sandbox command. Named the way
// internal/agenteval.Case.RequiredTools names them -- the capability, not a pinned route --
// document_search and shell_exec are internal/agent/tools built-ins (document_search.go:35,
// shell_exec.go:86); the memory write is the arcadedb-mcp MCP tool bridged under the
// "memory" namespace (cmd/aura/memory.go:27 memoryServerName, internal/agent/mcptools/
// name.go's namespacedName joining namespace+"__"+tool), registered upstream as
// memory_upsert_fact (cmd/arcadedb-mcp/tool_memory.go:194).
var requiredTools = []string{"document_search", "memory__memory_upsert_fact", "shell_exec"}

// tokenPattern extracts an identity's own seeded marker from her final answer. Both
// scripts/musr_live_run.sh (the real run) and every fixture under
// scripts/testdata/musr_live_run/ seed a marker of the exact form MUSR-<LABEL>-<hex>, so the
// assert script never needs the seed value handed to it out of band -- it reads each
// identity's own claim of her own token from her own transcript, which is also what makes
// the cross-read check meaningful: B's transcript is asked whether it contains A's token,
// never told what to look for by a shared secret neither side leaked.
func tokenPattern(label string) *regexp.Regexp {
	return regexp.MustCompile(`MUSR-` + label + `-[0-9a-fA-F]{6,}`)
}

// frame is one line of a transcript jsonl file: an AG-UI SSE frame the harness captured,
// with the receive-time wall clock the harness stamped on it.
type frame struct {
	TS    float64         `json:"ts"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type toolCallStartData struct {
	ToolCallID   string `json:"toolCallId"`
	ToolCallName string `json:"toolCallName"`
}

type textContentData struct {
	Delta string `json:"delta"`
}

// timingEntry is one line of timings.jsonl: a tool call's start or end, wall-clock stamped,
// identity-labelled so the overlap check can pair "a" intervals against "b" intervals.
type timingEntry struct {
	Identity   string  `json:"identity"`
	Tool       string  `json:"tool"`
	ToolCallID string  `json:"tool_call_id"`
	Phase      string  `json:"phase"` // "start" or "end"
	TS         float64 `json:"ts"`
}

type interval struct {
	start, end float64
}

func main() {
	fixture := flag.String("fixture", "", "run against a committed fixture under scripts/testdata/musr_live_run/<name> instead of --transcripts")
	transcriptsDir := flag.String("transcripts", "artifacts/musr-live-run", "directory holding transcript-a.jsonl, transcript-b.jsonl, timings.jsonl")
	flag.Parse()

	dir := *transcriptsDir
	if *fixture != "" {
		dir = filepath.Join("scripts", "testdata", "musr_live_run", *fixture)
	}

	failures := run(dir)
	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, "FAIL: "+f)
		}
		fmt.Fprintf(os.Stderr, "musr_live_run_assert: %d failing assertion(s) against %s\n", len(failures), dir)
		os.Exit(1)
	}
	fmt.Printf("musr_live_run_assert: OK — both identities completed, required tools fired, tokens separated, timings overlap (%s)\n", dir)
}

// run evaluates every assertion against the transcripts under dir and returns one message
// per violation, aggregated (never stop at the first failure — internal/agenteval/case.go's
// Check does the same for exactly the same reason).
func run(dir string) []string {
	var failures []string

	pathA := filepath.Join(dir, "transcript-a.jsonl")
	pathB := filepath.Join(dir, "transcript-b.jsonl")
	framesA, errA := readFrames(pathA)
	framesB, errB := readFrames(pathB)

	// Completion (D-18): an empty transcript, or one that ends without RUN_FINISHED, FAILS
	// the run outright — it is not a low score. Checked before anything else because every
	// later check is meaningless against a transcript that never happened.
	if msg, ok := completionFailure(pathA, framesA, errA); !ok {
		failures = append(failures, msg)
	}
	if msg, ok := completionFailure(pathB, framesB, errB); !ok {
		failures = append(failures, msg)
	}

	// Authentication: the first frame of a real SSE stream is RUN_STARTED, which the AG-UI
	// gateway never emits for an unauthenticated or thread-unowned request (server_run.go's
	// owner-scoped 404 fires before the stream opens). A transcript that starts with
	// anything else — or nothing — means the identity never got a session.
	if len(framesA) > 0 && framesA[0].Event != "RUN_STARTED" {
		failures = append(failures, fmt.Sprintf("authentication: %s does not open with RUN_STARTED (first event: %s) — identity A never got a session", pathA, framesA[0].Event))
	}
	if len(framesB) > 0 && framesB[0].Event != "RUN_STARTED" {
		failures = append(failures, fmt.Sprintf("authentication: %s does not open with RUN_STARTED (first event: %s) — identity B never got a session", pathB, framesB[0].Event))
	}

	evidenceA := evidenceFromFrames(framesA)
	evidenceB := evidenceFromFrames(framesB)

	// Required tools + expected/cross-read tokens, one identity at a time, reusing
	// internal/agenteval.Case.Check verbatim (not mirrored) for the token and tool
	// assertions — the exact evaluation semantics the package already carries.
	failures = append(failures, checkIdentity("A", pathA, evidenceA, evidenceB.Answer)...)
	failures = append(failures, checkIdentity("B", pathB, evidenceB, evidenceA.Answer)...)

	// Overlap (D-17): the timing file must show at least one interval where both
	// identities have a tool call in flight simultaneously — a run that took turns is not
	// concurrent, whatever else it proved.
	timingsPath := filepath.Join(dir, "timings.jsonl")
	timings, errT := readTimings(timingsPath)
	if errT != nil {
		failures = append(failures, fmt.Sprintf("overlap: could not read %s: %v", timingsPath, errT))
	} else if !hasOverlap(timings) {
		failures = append(failures, fmt.Sprintf("overlap: no interval in %s shows both identities' tool calls in flight at the same time — the run took turns rather than racing", timingsPath))
	}

	return failures
}

func completionFailure(path string, frames []frame, readErr error) (string, bool) {
	if readErr != nil {
		return fmt.Sprintf("completion: %s could not be read (%v) — an unreadable or missing transcript is a FAILED run, not a low score", path, readErr), false
	}
	if len(frames) == 0 {
		return fmt.Sprintf("completion: %s is empty — a FAILED run, not a low score", path), false
	}
	last := frames[len(frames)-1].Event
	if last != "RUN_FINISHED" {
		return fmt.Sprintf("completion: %s does not end with a terminal RUN_FINISHED frame (last event: %s) — a FAILED run, not a low score", path, last), false
	}
	return "", true
}

// checkIdentity runs the required-tool and token checks for one identity's transcript,
// prefixing every failure with the transcript path so a reader knows which side failed.
func checkIdentity(label, path string, evidence agenteval.Evidence, otherAnswer string) []string {
	var out []string

	// RequiredTools passes on ANY ONE match per internal/agenteval semantics, so each
	// capability needs its own Case to require ALL THREE, not merely one of the three.
	for _, tool := range requiredTools {
		c := agenteval.Case{RequiredTools: []string{tool}}
		for _, f := range c.Check(evidence) {
			out = append(out, fmt.Sprintf("required-tools[%s] %s: %s", label, path, f))
		}
	}

	ownPattern := tokenPattern(label)
	ownToken := ownPattern.FindString(evidence.Answer)
	if ownToken == "" {
		out = append(out, fmt.Sprintf("expected-token[%s] %s: answer does not carry identity %s's own seeded token (pattern MUSR-%s-<hex>) — answer: %s",
			label, path, label, label, truncate(evidence.Answer, 300)))
		return out // nothing to cross-read-check without a token to look for
	}

	c := agenteval.Case{AnswerContains: []string{ownToken}}
	// otherToken is the OTHER identity's own token, extracted from HER transcript — not a
	// shared secret either side was told to avoid, which is what makes a leak here
	// self-evident rather than an assumption. Guarded: an empty AnswerMustNotContain entry
	// would match agenteval.Case's strings.Contains(answer, "") vacuously (always true),
	// turning "the other identity never got a token either" into a false cross-read fail.
	if otherToken := tokenPattern(otherLabel(label)).FindString(otherAnswer); otherToken != "" {
		c.AnswerMustNotContain = []string{otherToken}
	}
	for _, f := range c.Check(evidence) {
		out = append(out, fmt.Sprintf("cross-read[%s] %s: %s", label, path, f))
	}
	return out
}

func otherLabel(label string) string {
	if label == "A" {
		return "B"
	}
	return "A"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// readFrames parses a transcript jsonl file into an ordered slice of frame. A missing file
// is a read error (surfaced as a completion failure); a present-but-empty file returns a
// nil slice with no error, which completionFailure treats as an empty transcript.
func readFrames(path string) ([]frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var frames []frame
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fr frame
		if err := json.Unmarshal([]byte(line), &fr); err != nil {
			return nil, fmt.Errorf("%s: invalid frame line %q: %w", path, truncate(line, 200), err)
		}
		frames = append(frames, fr)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return frames, nil
}

// evidenceFromFrames builds an agenteval.Evidence from a parsed transcript: Answer is the
// concatenation, in frame order, of every TEXT_MESSAGE_CONTENT delta (mirroring how the AG-
// UI translator streams one assistant message as a sequence of deltas — translator.go);
// Tools is the ordered list of TOOL_CALL_START toolCallName values, repeats included, the
// same shape internal/agenteval.EvidenceFromMessages produces from the durable DB record.
func evidenceFromFrames(frames []frame) agenteval.Evidence {
	var evidence agenteval.Evidence
	var answer strings.Builder
	for _, fr := range frames {
		switch fr.Event {
		case "TOOL_CALL_START":
			var d toolCallStartData
			if err := json.Unmarshal(fr.Data, &d); err == nil && d.ToolCallName != "" {
				evidence.Tools = append(evidence.Tools, d.ToolCallName)
			}
		case "TEXT_MESSAGE_CONTENT":
			var d textContentData
			if err := json.Unmarshal(fr.Data, &d); err == nil {
				answer.WriteString(d.Delta)
			}
		}
	}
	evidence.Answer = strings.TrimSpace(answer.String())
	return evidence
}

// readTimings parses timings.jsonl into an ordered slice of timingEntry.
func readTimings(path string) ([]timingEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []timingEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e timingEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("%s: invalid timing line %q: %w", path, truncate(line, 200), err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// intervalsFor pairs each identity's start/end timing entries by tool_call_id into closed
// wall-clock intervals. An unpaired start (no matching end) is dropped — a tool call that
// never reported an end cannot be asserted to overlap anything.
func intervalsFor(entries []timingEntry, identity string) []interval {
	starts := map[string]float64{}
	var out []interval
	for _, e := range entries {
		if e.Identity != identity {
			continue
		}
		switch e.Phase {
		case "start":
			starts[e.ToolCallID] = e.TS
		case "end":
			if s, ok := starts[e.ToolCallID]; ok {
				out = append(out, interval{start: s, end: e.TS})
				delete(starts, e.ToolCallID)
			}
		}
	}
	return out
}

// hasOverlap reports whether any "a" interval and any "b" interval share wall-clock time.
func hasOverlap(entries []timingEntry) bool {
	as := intervalsFor(entries, "a")
	bs := intervalsFor(entries, "b")
	for _, a := range as {
		for _, b := range bs {
			if a.start < b.end && b.start < a.end {
				return true
			}
		}
	}
	return false
}
