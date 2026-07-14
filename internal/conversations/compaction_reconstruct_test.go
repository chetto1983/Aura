package conversations

import (
	"errors"
	"testing"
)

func TestReconstructActivePointerOrderAndBranchIsolation(t *testing.T) {
	cp := ReconstructionCheckpoint{BranchID: "main", SchemaVersion: 1, ProjectionVersion: 1, Manifest: NewCompactionManifest(4, []int{2}, []int{3}, []int{1}, []int{4}, []ManifestTurn{{Seq: 1, Role: "system", Content: "l0"}, {Seq: 2, Content: "old"}, {Seq: 3, Content: "tail"}, {Seq: 4, Content: "after"}}), Summary: "summary"}
	got, err := ReconstructProjection(cp, []ManifestTurn{{Seq: 4, Content: "after"}, {Seq: 5, Content: "new"}}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"l0", "summary", "tail", "new"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i].Content != want[i] {
			t.Fatalf("[%d]=%q want %q", i, got[i].Content, want[i])
		}
	}
}

func TestRecoveryUsesCompatibleLKGThenBoundedRebuild(t *testing.T) {
	bad := ReconstructionCheckpoint{SchemaVersion: 2, ProjectionVersion: 1}
	lkg := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, Manifest: NewCompactionManifest(1, nil, []int{1}, nil, nil, []ManifestTurn{{Seq: 1, Content: "ok"}})}
	got, source, err := RecoverProjection(bad, []ReconstructionCheckpoint{lkg}, []ManifestTurn{{Seq: 1, Content: "ok"}}, RecoveryPolicy{SchemaVersion: 1, ProjectionVersion: 1, MaxTurns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if source != RecoveryLKG || len(got) != 1 {
		t.Fatalf("source=%s got=%#v", source, got)
	}
	_, _, err = RecoverProjection(bad, nil, []ManifestTurn{{Seq: 1}, {Seq: 2}}, RecoveryPolicy{SchemaVersion: 1, ProjectionVersion: 1, MaxTurns: 1})
	if !errors.Is(err, ErrContextUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestRestorePreviewRejectsIncompatibleOrOversized(t *testing.T) {
	cp := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, EstimatedTokens: 20}
	if err := PreviewRestore(cp, RestoreBudget{SchemaVersion: 1, ProjectionVersion: 1, InputCapacity: 10}); !errors.Is(err, ErrContextUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestQuarantineOnCorruptCheckpoint(t *testing.T) {
	cp := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, Manifest: NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}})}
	cp.Manifest.Digest = "bad"
	_, err := ValidateCheckpoint(cp, 1, 1)
	var q *QuarantineError
	if !errors.As(err, &q) {
		t.Fatalf("got %v", err)
	}
	if q.Error() != "quarantine compaction checkpoint: "+q.Reason {
		t.Fatalf("QuarantineError.Error()=%q", q.Error())
	}
}

func TestRecoveryPrefersActivePointerOverLKG(t *testing.T) {
	active := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, Manifest: NewCompactionManifest(1, nil, []int{1}, nil, nil, []ManifestTurn{{Seq: 1, Content: "active"}})}
	lkg := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, Manifest: NewCompactionManifest(1, nil, []int{1}, nil, nil, []ManifestTurn{{Seq: 1, Content: "lkg"}})}
	got, source, err := RecoverProjection(active, []ReconstructionCheckpoint{lkg}, []ManifestTurn{{Seq: 1, Content: "active"}}, RecoveryPolicy{SchemaVersion: 1, ProjectionVersion: 1, MaxTurns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if source != RecoveryActive || len(got) != 1 || got[0].Content != "active" {
		t.Fatalf("source=%s got=%#v", source, got)
	}
}

func TestRecoveryBoundedRebuildWhenNoCompatibleCheckpoint(t *testing.T) {
	bad := ReconstructionCheckpoint{SchemaVersion: 2, ProjectionVersion: 1}
	canonical := []ManifestTurn{{Seq: 2, Content: "b"}, {Seq: 1, Content: "a"}}
	got, source, err := RecoverProjection(bad, nil, canonical, RecoveryPolicy{SchemaVersion: 1, ProjectionVersion: 1, MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if source != RecoveryRebuild || len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("source=%s got=%#v", source, got)
	}
}

func TestPreviewRestorePassesThroughValidateCheckpoint(t *testing.T) {
	cp := ReconstructionCheckpoint{SchemaVersion: 1, ProjectionVersion: 1, EstimatedTokens: 5, Manifest: NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}})}
	if err := PreviewRestore(cp, RestoreBudget{SchemaVersion: 1, ProjectionVersion: 1, InputCapacity: 10}); err != nil {
		t.Fatalf("got %v", err)
	}
	cp.Manifest.Digest = "bad"
	err := PreviewRestore(cp, RestoreBudget{SchemaVersion: 1, ProjectionVersion: 1, InputCapacity: 10})
	var q *QuarantineError
	if !errors.As(err, &q) {
		t.Fatalf("got %v", err)
	}
}
