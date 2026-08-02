package adaptive

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryRecallContractsAcceptFrozenOrderAndLiveMetadata(t *testing.T) {
	ownerID := uuid.Must(uuid.NewV7())
	actions := []SnapshotAction{
		{ActionID: StaticActionID},
		{ActionID: "long_term_top_4"},
		{ActionID: "long_term_top_8"},
	}
	if _, err := NewPolicySnapshot(SnapshotSpec{
		Scope: SnapshotScope{
			OwnerID: ownerID, Domain: DomainMemoryRecall,
			Point: PointMemoryRecall, ProviderID: "openrouter",
			ModelID: "production-model", PolicyVersion: "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: StaticActionID,
		FeatureSchema: []SnapshotFeatureDefinition{
			{Key: FeatureQueryLength, Center: 0, Scale: 1},
			{Key: FeatureRecallLimit, Center: 0, Scale: 1},
		},
		Actions: actions,
	}); err != nil {
		t.Fatalf("memory snapshot: %v", err)
	}

	results, revisions, err := NewMemoryRecallDeliveryMetadata(
		[]MemoryRecallResult{
			{
				Kind: ResultMemoryPreference,
				ID:   "11111111-1111-4111-8111-111111111111",
			},
			{
				Kind: ResultMemoryEntity,
				ID:   "22222222-2222-4222-8222-222222222222",
			},
		},
		MemoryRecallRevisionValues{
			CorpusEpoch: 42,
			Retriever:   "arcadedb-long-term-v1",
			Reranker:    "none-v1",
			Embedding:   "openai/granite-embedding-v1@768",
			Index:       "entity_embedding_idx+preference_embedding_idx@768",
		},
	)
	if err != nil {
		t.Fatalf("memory delivery metadata: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if err := revisions.validate(); err != nil {
		t.Fatalf("revisions: %v", err)
	}
}

func TestMemoryRecallSnapshotRejectsSortedNonCatalogActions(t *testing.T) {
	_, err := NewPolicySnapshot(SnapshotSpec{
		Scope: SnapshotScope{
			OwnerID: uuid.Must(uuid.NewV7()), Domain: DomainMemoryRecall,
			Point: PointMemoryRecall, ProviderID: "openrouter",
			ModelID: "production-model", PolicyVersion: "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: StaticActionID,
		FeatureSchema: []SnapshotFeatureDefinition{
			{Key: FeatureQueryLength, Center: 0, Scale: 1},
			{Key: FeatureRecallLimit, Center: 0, Scale: 1},
		},
		Actions: []SnapshotAction{
			{ActionID: StaticActionID},
			{ActionID: "zzz"},
		},
	})
	if err == nil {
		t.Fatal("memory snapshot accepted a sorted non-catalog action")
	}
}
