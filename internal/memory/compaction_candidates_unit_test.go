package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// validCandidateInput returns a baseline CandidateInput that passes validateCandidate;
// individual fields are mutated per table case to exercise each rejection branch.
func validCandidateInput() CandidateInput {
	return CandidateInput{
		TenantID: uuid.NewString(), OwnerID: uuid.NewString(), Class: "preference", Purpose: "assistant_personalization",
		ConsentBasis:    "explicit",
		SourceManifest:  []SourceRef{{Kind: "turn", ID: uuid.NewString(), Digest: strings.Repeat("a", 64)}},
		Evidence:        "prefers concise answers",
		Confidence:      0.9,
		Authority:       "user",
		Sensitivity:     "normal",
		Region:          "eu",
		EncryptionClass: "identity",
		RetentionClass:  "durable",
		ExpiresAt:       time.Now().Add(time.Hour),
	}
}

func TestValidateCandidateRejectionBranches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*CandidateInput)
		wantErr error  // set when the branch returns a sentinel error
		wantSub string // set when the branch returns a plain errors.New; substring match
	}{
		{"malformed tenant", func(in *CandidateInput) { in.TenantID = "not-a-uuid" }, nil, "candidate tenant"},
		{"malformed owner", func(in *CandidateInput) { in.OwnerID = "not-a-uuid" }, nil, "candidate owner"},
		{"evidence overcollection", func(in *CandidateInput) { in.Evidence = strings.Repeat("x", 257) }, ErrEvidenceOvercollection, ""},
		{"evidence exactly at limit is allowed", func(in *CandidateInput) { in.Evidence = strings.Repeat("x", 256) }, nil, ""},
		{"secret evidence api_key", func(in *CandidateInput) { in.Evidence = "api_key: sk-1234567890123456" }, ErrSecretEvidence, ""},
		{"secret evidence password", func(in *CandidateInput) { in.Evidence = "password=hunter2hunter2" }, ErrSecretEvidence, ""},
		{"secret evidence bare token", func(in *CandidateInput) { in.Evidence = "sk-abcdefghijklmnopqrst" }, ErrSecretEvidence, ""},
		{"empty evidence", func(in *CandidateInput) { in.Evidence = "   " }, nil, "evidence and source manifest are required"},
		{"empty source manifest", func(in *CandidateInput) { in.SourceManifest = nil }, nil, "evidence and source manifest are required"},
		{"confidence below zero", func(in *CandidateInput) { in.Confidence = -0.1 }, nil, "confidence or expiry is invalid"},
		{"confidence above one", func(in *CandidateInput) { in.Confidence = 1.1 }, nil, "confidence or expiry is invalid"},
		{"zero expiry", func(in *CandidateInput) { in.ExpiresAt = time.Time{} }, nil, "confidence or expiry is invalid"},
		{"invalid authority", func(in *CandidateInput) { in.Authority = "system" }, nil, "authority must be user or tool"},
		{"tool authority is allowed", func(in *CandidateInput) { in.Authority = "tool" }, nil, ""},
		{"missing encryption class", func(in *CandidateInput) { in.EncryptionClass = "" }, nil, "policy metadata is incomplete"},
		{"missing region", func(in *CandidateInput) { in.Region = "" }, nil, "policy metadata is incomplete"},
		{"missing purpose", func(in *CandidateInput) { in.Purpose = "" }, nil, "policy metadata is incomplete"},
		{"missing consent basis", func(in *CandidateInput) { in.ConsentBasis = "" }, nil, "policy metadata is incomplete"},
		{"missing class", func(in *CandidateInput) { in.Class = "" }, nil, "policy metadata is incomplete"},
		{"invalid source kind", func(in *CandidateInput) {
			in.SourceManifest = []SourceRef{{Kind: "session", ID: uuid.NewString(), Digest: strings.Repeat("a", 64)}}
		}, nil, "source kind is invalid"},
		{"checkpoint source kind is allowed", func(in *CandidateInput) {
			in.SourceManifest = []SourceRef{{Kind: "checkpoint", ID: uuid.NewString(), Digest: strings.Repeat("a", 64)}}
		}, nil, ""},
		{"artifact source kind is allowed", func(in *CandidateInput) {
			in.SourceManifest = []SourceRef{{Kind: "artifact", ID: uuid.NewString(), Digest: strings.Repeat("a", 64)}}
		}, nil, ""},
		{"non-hex source digest", func(in *CandidateInput) {
			in.SourceManifest = []SourceRef{{Kind: "turn", ID: uuid.NewString(), Digest: strings.Repeat("z", 64)}}
		}, nil, "source digest is invalid"},
		{"short source digest", func(in *CandidateInput) {
			in.SourceManifest = []SourceRef{{Kind: "turn", ID: uuid.NewString(), Digest: strings.Repeat("a", 40)}}
		}, nil, "source digest is invalid"},
		{"valid baseline passes", func(in *CandidateInput) {}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCandidateInput()
			tc.mutate(&in)
			err := validateCandidate(in)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("validateCandidate() err=%v, want %v", err, tc.wantErr)
				}
			case tc.wantSub != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
					t.Fatalf("validateCandidate() err=%v, want substring %q", err, tc.wantSub)
				}
			default:
				if err != nil {
					t.Fatalf("validateCandidate() unexpected err=%v", err)
				}
			}
		})
	}
}

func TestEvidenceDigestIsDeterministicAndDistinct(t *testing.T) {
	want := sha256.Sum256([]byte("prefers concise answers"))
	if got := EvidenceDigest("prefers concise answers"); got != hex.EncodeToString(want[:]) {
		t.Fatalf("EvidenceDigest mismatch: got %q", got)
	}
	if EvidenceDigest("a") == EvidenceDigest("b") {
		t.Fatal("EvidenceDigest collided for distinct inputs")
	}
	if EvidenceDigest("") == "" {
		t.Fatal("EvidenceDigest must not be empty for empty input")
	}
}

func TestNewIDReturnsDistinctParsableUUIDs(t *testing.T) {
	a, b := newID(), newID()
	if a == b {
		t.Fatalf("newID produced duplicate IDs: %s", a)
	}
	if _, err := uuid.Parse(a); err != nil {
		t.Fatalf("newID produced unparsable UUID %q: %v", a, err)
	}
	if _, err := uuid.Parse(b); err != nil {
		t.Fatalf("newID produced unparsable UUID %q: %v", b, err)
	}
}
