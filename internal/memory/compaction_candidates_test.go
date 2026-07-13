//go:build db_integration

package memory

import (
	"context"
	"errors"
	"os"
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

func digest(v string) string { return EvidenceDigest(v) }
