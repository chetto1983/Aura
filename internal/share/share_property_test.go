package share

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"pgregory.net/rapid"
)

// secretCorpus is the set of realistic leak strings this property suite
// scatters through generated histories: a send_file {"path":...} argument
// shape (send_file.go), a shell_exec-style /etc/passwd result, an
// $AURA_RUN_DIR spilled-turn sidecar path template (conversations/store.go),
// and a container id embedded in raw tool stdout. Every one of these traces
// to a real leak source named in 37F-RESEARCH.md's Redaction Inventory
// (L-01..L-09), the same corpus 37F-03's hostile fixture drew from.
var secretCorpus = []string{
	"/abs/secret/results.xlsx",
	"reading /etc/passwd on container=c3a9f1b02e7d",
	"$AURA_RUN_DIR/conversations/conv-77/5.content",
	`cannot read "/abs/secret/report.csv": permission denied`,
}

// hostileToolNames is the small pool of real tool names used when
// generating an assistant tool-call turn. The actual name is never secret
// (D-08: tool names survive by design) — only the Arguments/Content string
// that carries a secretCorpus entry is what TestPropertyRedactionTotality
// checks for absence.
var hostileToolNames = []string{"send_file", "shell_exec", "web_fetch"}

// msgKind enumerates the message shapes genHostileHistory mixes into a
// generated history.
type msgKind int

const (
	kindUser msgKind = iota
	kindAssistantPlain
	kindAssistantTool
	kindToolResult
	kindSystem
)

var allMsgKinds = []msgKind{kindUser, kindAssistantPlain, kindAssistantTool, kindToolResult, kindSystem}

// genHostileHistory draws a random []llm.Message mixing user/assistant
// (with and without tool calls)/tool/system turns via rapid, embedding one
// secretCorpus entry into every generated tool call's Arguments and every
// generated tool-result's Content. It returns the history alongside the
// exact list of secrets it planted.
//
// It GUARANTEES two things a naive "just embed a secret somewhere" generator
// would miss:
//   - at least one secret-bearing turn (a kindAssistantTool or kindToolResult
//     draw), forcing one if the random mix produced none; and
//   - at least one turn that SURVIVES projectTurns' user/assistant allowlist
//     (redact.go) — a history built entirely from kindToolResult/kindSystem
//     turns plants a secret but yields a ZERO-turn Snapshot, which would let
//     TestPropertyRedactionTotality's own anti-vacuity guard trip for the
//     wrong reason (this was caught live: rapid found exactly this case on
//     an early run of this generator, forcing this second guard to be added).
//
// A single forced draw (kindAssistantTool) resolves BOTH gaps at once: it is
// simultaneously an assistant-role turn (survives the allowlist) and a
// tool-call turn (carries a secret in Arguments) — the same reason
// TestSnapshotRedactsHostPaths in snapshot_test.go relies on the identical
// shape.
func genHostileHistory(rt *rapid.T) ([]llm.Message, []string) {
	n := rapid.IntRange(2, 12).Draw(rt, "n_messages")
	kinds := make([]msgKind, n)
	for i := 0; i < n; i++ {
		kinds[i] = rapid.SampledFrom(allMsgKinds).Draw(rt, fmt.Sprintf("kind-%d", i))
	}

	needsSecretBearer := true
	needsSurvivor := true
	for _, k := range kinds {
		if k == kindAssistantTool || k == kindToolResult {
			needsSecretBearer = false
		}
		if k == kindUser || k == kindAssistantPlain || k == kindAssistantTool {
			needsSurvivor = false
		}
	}
	if needsSecretBearer || needsSurvivor {
		forced := rapid.IntRange(0, n-1).Draw(rt, "forced_idx")
		kinds[forced] = kindAssistantTool
	}

	msgs := make([]llm.Message, 0, n)
	secrets := make([]string, 0, n)
	for i, k := range kinds {
		switch k {
		case kindUser:
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("user turn %d: can you help with this?", i)})
		case kindAssistantPlain:
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("assistant reply %d: here is what I found.", i)})
		case kindAssistantTool:
			secret := rapid.SampledFrom(secretCorpus).Draw(rt, fmt.Sprintf("secret-%d", i))
			name := rapid.SampledFrom(hostileToolNames).Draw(rt, fmt.Sprintf("toolname-%d", i))
			tc := llm.ToolCall{ID: fmt.Sprintf("call_%d", i), Type: "function"}
			tc.Function.Name = name
			tc.Function.Arguments = fmt.Sprintf(`{"path":%q}`, secret)
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{tc}})
			secrets = append(secrets, secret)
		case kindToolResult:
			secret := rapid.SampledFrom(secretCorpus).Draw(rt, fmt.Sprintf("secret-%d", i))
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, Content: "result: " + secret, ToolCallID: fmt.Sprintf("call_%d", i)})
			secrets = append(secrets, secret)
		case kindSystem:
			msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: fmt.Sprintf("system directive %d", i)})
		}
	}
	return msgs, secrets
}

// genArtifacts draws 0-3 ArtifactMeta entries for the JSON round-trip
// property. Redaction totality does not need artifacts (the leak surface
// under test is turns), so this generator is only wired into
// TestPropertyJSONRoundTrip.
func genArtifacts(rt *rapid.T) []ArtifactMeta {
	n := rapid.IntRange(0, 3).Draw(rt, "n_artifacts")
	out := make([]ArtifactMeta, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ArtifactMeta{
			AssetID:   fmt.Sprintf("asset-%d", i),
			FileName:  fmt.Sprintf("file-%d.txt", i),
			MIMEType:  "text/plain",
			SizeBytes: int64(rapid.IntRange(0, 1<<20).Draw(rt, fmt.Sprintf("size-%d", i))),
		})
	}
	return out
}

// TestPropertyRedactionTotality is the single most valuable test in this
// phase: the SC3 universal, stated machine-checkably. For every generated
// history and every secret planted into it (send_file/shell_exec-shaped
// arguments and results, a sidecar path template, a permission-denied error
// carrying a path), the secret must be absent from BOTH Markdown() and
// JSON() of BuildSnapshot(history).
//
// The anti-vacuity guard below (>=1 secret planted, >=1 turn in the
// resulting Snapshot) is load-bearing: an empty Snapshot or a
// secret-free history would let this property pass trivially without
// proving anything, which is exactly the mutant a totality property must
// not let live.
//
// rapid.Check runs 100 generated cases per test by default (raised via
// -rapid.checks, halved under -short); on failure it prints a deterministic
// -rapid.seed=N a human can pass back to rapid.Check/-run to reproduce the
// exact failing draw byte-for-byte, so a red run here is never a one-off.
func TestPropertyRedactionTotality(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		msgs, secrets := genHostileHistory(rt)
		if len(secrets) == 0 {
			rt.Fatalf("anti-vacuity guard failed: generated history embedded zero secrets")
		}

		snap, err := BuildSnapshot(ConvMeta{Title: "prop-totality", Model: "m"}, msgs, nil, time.Now())
		if err != nil {
			rt.Fatalf("BuildSnapshot: %v", err)
		}
		if len(snap.Turns) == 0 {
			rt.Fatalf("anti-vacuity guard failed: snapshot has zero turns (totality would pass vacuously)")
		}

		md := string(snap.Markdown())
		data, err := snap.JSON()
		if err != nil {
			rt.Fatalf("JSON: %v", err)
		}
		js := string(data)

		for _, secret := range secrets {
			if strings.Contains(md, secret) {
				rt.Fatalf("Markdown() leaked planted secret %q", secret)
			}
			if strings.Contains(js, secret) {
				rt.Fatalf("JSON() leaked planted secret %q", secret)
			}
		}
	})
}

// TestPropertyRedactionIdempotent is the fixpoint property: BuildSnapshot
// applied to an already-projected turn set finds nothing further to strip.
// The second pass re-expresses each SnapshotTurn as a plain llm.Message
// (Role + Text only — there is nowhere on SnapshotTurn to put a ToolCall
// back, by construction), so this also confirms projectTurns' allowlist has
// no path that could resurrect something the first pass removed.
func TestPropertyRedactionIdempotent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		msgs, _ := genHostileHistory(rt)
		first, err := BuildSnapshot(ConvMeta{Title: "prop-idem", Model: "m"}, msgs, nil, time.Now())
		if err != nil {
			rt.Fatalf("BuildSnapshot: %v", err)
		}

		reprojected := make([]llm.Message, 0, len(first.Turns))
		for _, turn := range first.Turns {
			reprojected = append(reprojected, llm.Message{Role: turn.Role, Content: turn.Text})
		}
		second, err := BuildSnapshot(ConvMeta{Title: "prop-idem", Model: "m"}, reprojected, nil, first.SnapshotAt)
		if err != nil {
			rt.Fatalf("BuildSnapshot (second pass): %v", err)
		}

		if len(second.Turns) != len(first.Turns) {
			rt.Fatalf("second pass turn count = %d, want %d (fixpoint)", len(second.Turns), len(first.Turns))
		}
		for i, turn := range second.Turns {
			if turn.Role != first.Turns[i].Role || turn.Text != first.Turns[i].Text {
				rt.Fatalf("turn %d changed on reprojection: got %+v, want role/text of %+v", i, turn, first.Turns[i])
			}
		}
	})
}

// TestPropertyJSONRoundTrip is the D-07 "lossless structured round-trip"
// property, over generated Snapshots rather than a single fixture: for
// every generated Snapshot s, unmarshalling JSON(s) yields a Snapshot
// reflect.DeepEqual to s. now is built with time.Now().UTC().Truncate, not
// time.Now() directly — Truncate strips the monotonic clock reading, which
// JSON marshal/unmarshal always strips anyway; comparing against a raw
// time.Now() value would fail DeepEqual for a reason that has nothing to do
// with the adapter under test.
func TestPropertyJSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		msgs, _ := genHostileHistory(rt)
		artifacts := genArtifacts(rt)
		title := rapid.StringN(0, 40, -1).Draw(rt, "title")
		now := time.Now().UTC().Truncate(time.Second)

		snap, err := BuildSnapshot(ConvMeta{Title: title, Model: "m", CreatedAt: now}, msgs, artifacts, now)
		if err != nil {
			rt.Fatalf("BuildSnapshot: %v", err)
		}

		data, err := snap.JSON()
		if err != nil {
			rt.Fatalf("JSON: %v", err)
		}
		var got Snapshot
		if err := json.Unmarshal(data, &got); err != nil {
			rt.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(got, snap) {
			rt.Fatalf("round trip mismatch:\n got=%+v\nwant=%+v", got, snap)
		}
	})
}
