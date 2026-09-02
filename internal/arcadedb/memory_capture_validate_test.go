package arcadedb

import (
	"math"
	"strings"
	"testing"
	"time"
)

func acceptedDurableArtifactCapture() AcceptedCapture {
	const artifact = "/workspace/artifacts/report.txt"
	capture := acceptedExplicitCapture(acceptedCaptureKey("b"), "run-a", string(WriterParent), "Rome")
	capture.SourceKind = CaptureSourceDurableArtifact
	capture.ArtifactRef = artifact
	capture.Subject = artifact
	capture.Predicate = "durable_artifact"
	capture.Object = "write"
	capture.Statement = "The agent wrote " + artifact + "."
	capture.SourceRefs = append(capture.SourceRefs, "artifact:"+artifact)
	return capture
}

// This validator is the last thing between a tool's claim and a durable graph write, and
// each refusal is the reason a specific forgery cannot land. The live tier reported the
// whole function uncovered on 2026-09-02.
func TestValidateAcceptedCaptureForGraphRefusesAnIneligibleCapture(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*AcceptedCapture)
		reason string
	}{
		"idempotency key is the wrong length": {
			func(c *AcceptedCapture) { c.IdempotencyKey = strings.Repeat("a", 63) },
			"lowercase SHA-256",
		},
		"idempotency key is uppercase": {
			func(c *AcceptedCapture) { c.IdempotencyKey = strings.Repeat("A", 64) },
			"lowercase SHA-256",
		},
		"idempotency key is not hex": {
			func(c *AcceptedCapture) { c.IdempotencyKey = strings.Repeat("z", 64) },
			"lowercase SHA-256",
		},
		"no conversation": {
			func(c *AcceptedCapture) { c.ConversationID = "" },
			"missing required direct provenance",
		},
		"no tool call": {
			func(c *AcceptedCapture) { c.ToolCallID = "" },
			"missing required direct provenance",
		},
		"no observation time": {
			func(c *AcceptedCapture) { c.ObservedAt = time.Time{} },
			"missing required direct provenance",
		},
		"actor role is neither parent nor worker": {
			func(c *AcceptedCapture) { c.ActorRole = "operator" },
			"actor role must be",
		},
		"confidence is not a number": {
			func(c *AcceptedCapture) { c.Confidence = math.NaN() },
			"confidence must be in (0,1]",
		},
		"confidence is infinite": {
			func(c *AcceptedCapture) { c.Confidence = math.Inf(1) },
			"confidence must be in (0,1]",
		},
		"confidence is zero": {
			func(c *AcceptedCapture) { c.Confidence = 0 },
			"confidence must be in (0,1]",
		},
		"confidence is above one": {
			func(c *AcceptedCapture) { c.Confidence = 1.5 },
			"confidence must be in (0,1]",
		},
		"validity window closes before it opens": {
			func(c *AcceptedCapture) {
				c.ValidFrom = acceptedCaptureTime
				c.ValidTo = acceptedCaptureTime.Add(-time.Hour)
			},
			"valid_to must be after valid_from",
		},
		"target fact key is not a digest": {
			func(c *AcceptedCapture) { c.TargetFactKey = "not-a-digest" },
			"target fact key must be SHA-256",
		},
		"more source refs than the cap": {
			func(c *AcceptedCapture) {
				refs := make([]string, defaultMemoryLimits.SourceMemoryIDs)
				for index := range refs {
					refs[index] = "memory:fact-a"
				}
				c.SourceRefs = refs
			},
			"source refs exceeds",
		},
		"conversation id is over its rune limit": {
			func(c *AcceptedCapture) {
				c.ConversationID = strings.Repeat("c", defaultMemoryLimits.SourceMemoryIDRunes+1)
			},
			"conversation id",
		},
		"a source ref names something else entirely": {
			func(c *AcceptedCapture) { c.SourceRefs = append(c.SourceRefs, "artifact:/etc/passwd") },
			"is not allowed for",
		},
		"an explicit fact carries an artifact ref": {
			func(c *AcceptedCapture) { c.ArtifactRef = "/workspace/artifacts/report.txt" },
			"cannot carry an artifact ref",
		},
		"source kind is not eligible": {
			func(c *AcceptedCapture) { c.SourceKind = "guessed" },
			"is not eligible",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			capture := acceptedExplicitCapture(acceptedCaptureKey("a"), "run-a", string(WriterParent), "Rome")
			testCase.mutate(&capture)
			err := validateAcceptedCaptureForGraph(capture, defaultMemoryLimits)
			if err == nil {
				t.Fatalf("validateAcceptedCaptureForGraph accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestValidateAcceptedCaptureForGraphGuardsTheDurableArtifactShape(t *testing.T) {
	t.Parallel()
	if err := validateAcceptedCaptureForGraph(acceptedDurableArtifactCapture(), defaultMemoryLimits); err != nil {
		t.Fatalf("a well-formed durable artifact capture was rejected: %v", err)
	}

	cases := map[string]struct {
		mutate func(*AcceptedCapture)
		reason string
	}{
		"artifact capture supersedes a fact": {
			func(c *AcceptedCapture) { c.Supersedes = true },
			"cannot supersede facts",
		},
		"artifact capture targets a fact": {
			func(c *AcceptedCapture) { c.TargetFactKey = acceptedCaptureKey("c") },
			"cannot supersede facts",
		},
		"artifact ref escapes the workspace": {
			func(c *AcceptedCapture) {
				c.ArtifactRef = "/etc/passwd"
				c.Subject = c.ArtifactRef
				c.SourceRefs[len(c.SourceRefs)-1] = "artifact:" + c.ArtifactRef
			},
			"invalid structured evidence",
		},
		"artifact ref is not clean": {
			func(c *AcceptedCapture) {
				c.ArtifactRef = "/workspace/../etc/passwd"
				c.Subject = c.ArtifactRef
				c.SourceRefs[len(c.SourceRefs)-1] = "artifact:" + c.ArtifactRef
			},
			"invalid structured evidence",
		},
		"subject does not name the artifact": {
			func(c *AcceptedCapture) { c.Subject = "something else" },
			"invalid structured evidence",
		},
		"predicate is not durable_artifact": {
			func(c *AcceptedCapture) { c.Predicate = "mentions" },
			"invalid structured evidence",
		},
		"object is neither write nor patch": {
			func(c *AcceptedCapture) { c.Object = "delete" },
			"invalid structured evidence",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			capture := acceptedDurableArtifactCapture()
			testCase.mutate(&capture)
			err := validateAcceptedCaptureForGraph(capture, defaultMemoryLimits)
			if err == nil {
				t.Fatalf("validateAcceptedCaptureForGraph accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestValidateCaptureSourceRefsRequiresTheDirectProvenanceTriple(t *testing.T) {
	t.Parallel()
	// The three refs naming the conversation, the tool call and the user turn are what make
	// a capture attributable; a capture that drops one is not attributable at all.
	for index := range 3 {
		capture := acceptedExplicitCapture(acceptedCaptureKey("a"), "run-a", string(WriterParent), "Rome")
		dropped := capture.SourceRefs[index]
		capture.SourceRefs = append(capture.SourceRefs[:index:index], capture.SourceRefs[index+1:]...)
		err := validateCaptureSourceRefs(capture, defaultMemoryLimits)
		if err == nil {
			t.Fatalf("validateCaptureSourceRefs accepted a capture without %q", dropped)
		}
	}

	// An explicit fact may cite the memory it supersedes; a durable artifact may not.
	capture := acceptedExplicitCapture(acceptedCaptureKey("a"), "run-a", string(WriterParent), "Rome")
	capture.SourceRefs = append(capture.SourceRefs, "memory:fact-a")
	if err := validateCaptureSourceRefs(capture, defaultMemoryLimits); err != nil {
		t.Fatalf("an explicit fact citing a prior memory was rejected: %v", err)
	}
	capture.SourceRefs[len(capture.SourceRefs)-1] = "memory:"
	if err := validateCaptureSourceRefs(capture, defaultMemoryLimits); err == nil {
		t.Fatal("validateCaptureSourceRefs accepted an empty memory ref")
	}

	over := acceptedExplicitCapture(acceptedCaptureKey("a"), "run-a", string(WriterParent), "Rome")
	over.SourceRefs = append(over.SourceRefs, "memory:"+strings.Repeat("m", defaultMemoryLimits.SourceMemoryIDRunes))
	if err := validateCaptureSourceRefs(over, defaultMemoryLimits); err == nil {
		t.Fatal("validateCaptureSourceRefs accepted a source ref over its rune limit")
	}
}
