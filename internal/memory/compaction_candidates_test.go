//go:build db_integration

package memory

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("AURA_DB_URL")
	migrateURL := os.Getenv("AURA_DB_MIGRATE_URL")
	if url == "" || migrateURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("db integration environment is required under CI")
		}
		t.Skip("set AURA_DB_URL and AURA_DB_MIGRATE_URL")
	}
	if _, err := db.Migrate(t.Context(), migrateURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(t.Context(), &db.Config{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool)
}

func TestCompactionCandidateIdempotentAcrossRestoreAndRebuild(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	a, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("duplicate rebuild IDs differ: %s != %s", a.ID, b.ID)
	}
	if a.PromotionAllowed {
		t.Fatal("automatic promotion must default off")
	}
}

func candidateFixtureForStore(t *testing.T, s *Store) CandidateInput {
	t.Helper()
	owner := uuid.NewString()
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')`, owner, "memory-"+owner); err != nil {
		t.Fatal(err)
	}
	return CandidateInput{
		TenantID: uuid.NewString(), OwnerID: owner, Class: "preference", Purpose: "assistant_personalization",
		ConsentBasis: "explicit", SourceManifest: []SourceRef{{Kind: "turn", ID: uuid.NewString(), Digest: digest("source")}},
		Evidence: "prefers concise answers", Confidence: 0.9, Authority: "user", Sensitivity: "normal", Region: "eu",
		EncryptionClass: "identity", RetentionClass: "durable", ExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestCompactionCandidateRejectsSecretAndEvidenceOvercollection(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	in.Evidence = "api_key=sk-12345678901234567890"
	if _, err := s.CreateCandidate(t.Context(), in); !errors.Is(err, ErrSecretEvidence) {
		t.Fatalf("secret error=%v", err)
	}
	in.Evidence = string(make([]byte, 257))
	if _, err := s.CreateCandidate(t.Context(), in); !errors.Is(err, ErrEvidenceOvercollection) {
		t.Fatalf("overcollection error=%v", err)
	}
}

func TestMigration0038TablesAndImmutableSources(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	c, err := s.CreateCandidate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM aura.compaction_memory_sources WHERE candidate_id=$1`, c.ID); err == nil {
		t.Fatal("source reachability link must be immutable")
	}
}

func TestPromotionRequiresReviewAndExplicitClassPolicy(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	c, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: in.TenantID, OwnerID: in.OwnerID, Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err := s.Promote(t.Context(), Policy{}, principal, c.ID); !errors.Is(err, ErrPromotionDisabled) {
		t.Fatalf("default promotion error=%v", err)
	}
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true}}}
	m, err := s.Promote(t.Context(), policy, principal, c.ID)
	if err != nil || m.CandidateID != c.ID {
		t.Fatalf("promote=%+v err=%v", m, err)
	}
}

func TestRetrievalGateDeniesCrossBoundaryBeforeRelevance(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	c, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true, AllowRetrieval: true}}}
	p := Principal{TenantID: in.TenantID, OwnerID: in.OwnerID, Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err := s.Promote(t.Context(), policy, p, c.ID); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Principal)
	}{
		{"identity", func(p *Principal) { p.OwnerID = uuid.NewString() }},
		{"region", func(p *Principal) { p.Region = "us" }},
		{"purpose", func(p *Principal) { p.Purpose = "ads" }},
		{"capability", func(p *Principal) { p.Capabilities = nil }},
		{"sensitivity", func(p *Principal) { p.MaxSensitivity = "public" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			denied := p
			tc.mutate(&denied)
			got, err := s.Retrieve(t.Context(), policy, denied, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatalf("cross-boundary retrieval=%+v", got)
			}
		})
	}
	got, err := s.Retrieve(t.Context(), policy, p, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("authorized retrieval=%+v err=%v", got, err)
	}
}

func TestConsentWithdrawalDeletionForgetAndExpiryPropagate(t *testing.T) {
	for _, event := range []string{"consent", "deletion", "forget", "expiry"} {
		t.Run(event, func(t *testing.T) {
			s := testStore(t)
			in := candidateFixtureForStore(t, s)
			c, err := s.CreateCandidate(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true, AllowRetrieval: true}}}
			p := Principal{TenantID: in.TenantID, OwnerID: in.OwnerID, Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
			if _, err = s.Promote(t.Context(), policy, p, c.ID); err != nil {
				t.Fatal(err)
			}
			switch event {
			case "consent":
				err = s.WithdrawConsent(t.Context(), in.TenantID, in.OwnerID, in.Purpose)
			case "deletion":
				err = s.DeleteSource(t.Context(), in.SourceManifest[0].Kind, in.SourceManifest[0].ID)
			case "forget":
				err = s.ForgetMe(t.Context(), in.TenantID, in.OwnerID)
			case "expiry":
				err = s.Expire(t.Context(), in.ExpiresAt.Add(time.Second))
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Retrieve(t.Context(), policy, p, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatalf("%s left retrievable memory", event)
			}
		})
	}
}

func TestSupersessionRemovesOldMemoryFromRetrieval(t *testing.T) {
	s := testStore(t)
	first := candidateFixtureForStore(t, s)
	a, err := s.CreateCandidate(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Evidence = "prefers very concise answers"
	second.SourceManifest = []SourceRef{{Kind: "turn", ID: uuid.NewString(), Digest: digest("new")}}
	b, err := s.CreateCandidate(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true, AllowRetrieval: true}}}
	p := Principal{TenantID: first.TenantID, OwnerID: first.OwnerID, Purpose: first.Purpose, Region: first.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err = s.Promote(t.Context(), policy, p, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Promote(t.Context(), policy, p, b.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Supersede(t.Context(), a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Retrieve(t.Context(), policy, p, 10)
	if err != nil || len(got) != 1 || got[0].CandidateID != b.ID {
		t.Fatalf("superseded retrieval=%+v err=%v", got, err)
	}
}

func digest(v string) string { return EvidenceDigest(v) }

// TestCreateCandidateCanonicalizesManifestOrderRegardlessOfInputOrder proves the
// sort.Slice canonicalization in CreateCandidate: two calls carrying the SAME source
// set in DIFFERENT input order (exercising both the same-kind ID-comparison branch and
// the cross-kind Kind-comparison branch) must hash to the same source_manifest_digest
// and therefore resolve to the identical idempotent candidate row.
func TestCreateCandidateCanonicalizesManifestOrderRegardlessOfInputOrder(t *testing.T) {
	s := testStore(t)
	base := candidateFixtureForStore(t, s)
	turnA := SourceRef{Kind: "turn", ID: "aaa-" + uuid.NewString(), Digest: digest("turn-a")}
	turnB := SourceRef{Kind: "turn", ID: "bbb-" + uuid.NewString(), Digest: digest("turn-b")}
	checkpoint := SourceRef{Kind: "checkpoint", ID: uuid.NewString(), Digest: digest("checkpoint")}
	artifact := SourceRef{Kind: "artifact", ID: uuid.NewString(), Digest: digest("artifact")}

	forward := base
	forward.SourceManifest = []SourceRef{turnA, turnB, checkpoint, artifact}
	a, err := s.CreateCandidate(t.Context(), forward)
	if err != nil {
		t.Fatal(err)
	}

	shuffled := base
	shuffled.SourceManifest = []SourceRef{artifact, turnB, checkpoint, turnA}
	b, err := s.CreateCandidate(t.Context(), shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("manifest order changed candidate identity: forward=%s shuffled=%s", a.ID, b.ID)
	}
}

// TestCreateCandidateRejectsUnknownOwnerForeignKey proves the DB-level owner_id FK
// constraint surfaces as a wrapped "insert candidate" error: validateCandidate only
// checks OwnerID is a well-formed UUID, not that the identity actually exists.
func TestCreateCandidateRejectsUnknownOwnerForeignKey(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	in.OwnerID = uuid.NewString() // syntactically valid, no matching aura.identities row
	if _, err := s.CreateCandidate(t.Context(), in); err == nil || !strings.Contains(err.Error(), "insert candidate") {
		t.Fatalf("unknown owner FK error=%v", err)
	}
}
