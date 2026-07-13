package conversations

import (
	"fmt"
	"sort"
)

// ReconstructionCheckpoint is the pure reader input selected through an active pointer.
type ReconstructionCheckpoint struct {
	BranchID                         string
	SchemaVersion, ProjectionVersion int
	Manifest                         VersionedCompactionManifest
	Summary                          string
	EstimatedTokens                  int
}

// QuarantineError marks a corrupt or incompatible record for operator-visible quarantine.
type QuarantineError struct{ Reason string }

func (e *QuarantineError) Error() string { return "quarantine compaction checkpoint: " + e.Reason }

// ValidateCheckpoint dispatches by stored schema/projection version and verifies digests.
func ValidateCheckpoint(cp ReconstructionCheckpoint, schemaVersion, projectionVersion int) (ReconstructionCheckpoint, error) {
	if cp.SchemaVersion != schemaVersion || cp.ProjectionVersion != projectionVersion {
		return ReconstructionCheckpoint{}, fmt.Errorf("incompatible checkpoint version: %w", ErrContextUnavailable)
	}
	if err := cp.Manifest.Verify(); err != nil {
		return ReconstructionCheckpoint{}, &QuarantineError{Reason: err.Error()}
	}
	return cp, nil
}

// ReconstructProjection returns L0/protected, internal summary, tail, then post-watermark turns.
func ReconstructProjection(cp ReconstructionCheckpoint, canonical []ManifestTurn, schemaVersion, projectionVersion int) ([]ManifestTurn, error) {
	cp, err := ValidateCheckpoint(cp, schemaVersion, projectionVersion)
	if err != nil {
		return nil, err
	}
	bySeq := map[int]ManifestTurn{}
	for _, turn := range cp.Manifest.Turns {
		bySeq[turn.Seq] = turn
	}
	for _, turn := range canonical {
		bySeq[turn.Seq] = turn
	}
	out := make([]ManifestTurn, 0)
	for _, seq := range cp.Manifest.ProtectedTurnSeqs {
		out = append(out, bySeq[seq])
	}
	if cp.Summary != "" {
		out = append(out, ManifestTurn{Role: "internal", Content: cp.Summary})
	}
	for _, seq := range cp.Manifest.TailTurnSeqs {
		out = append(out, bySeq[seq])
	}
	post := make([]ManifestTurn, 0)
	for _, turn := range canonical {
		if turn.Seq > cp.Manifest.CapturedWatermarkSeq {
			post = append(post, turn)
		}
	}
	sort.Slice(post, func(i, j int) bool { return post[i].Seq < post[j].Seq })
	out = append(out, post...)
	return out, nil
}

// RecoverySource records which bounded recovery tier succeeded.
type RecoverySource string

const (
	// RecoveryActive means the active pointer validated successfully.
	RecoveryActive RecoverySource = "active"
	// RecoveryLKG means a compatible last-known-good checkpoint was selected.
	RecoveryLKG RecoverySource = "last_known_good"
	// RecoveryRebuild means the bounded canonical selector supplied the projection.
	RecoveryRebuild RecoverySource = "bounded_rebuild"
)

// RecoveryPolicy bounds compatible readers and canonical rebuild size.
type RecoveryPolicy struct{ SchemaVersion, ProjectionVersion, MaxTurns int }

// RecoverProjection follows active, compatible LKG, bounded canonical rebuild, then failure.
func RecoverProjection(active ReconstructionCheckpoint, lkg []ReconstructionCheckpoint, canonical []ManifestTurn, p RecoveryPolicy) ([]ManifestTurn, RecoverySource, error) {
	if got, err := ReconstructProjection(active, canonical, p.SchemaVersion, p.ProjectionVersion); err == nil {
		return got, RecoveryActive, nil
	}
	for _, cp := range lkg {
		if got, err := ReconstructProjection(cp, canonical, p.SchemaVersion, p.ProjectionVersion); err == nil {
			return got, RecoveryLKG, nil
		}
	}
	if p.MaxTurns > 0 && len(canonical) <= p.MaxTurns {
		out := append([]ManifestTurn(nil), canonical...)
		sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
		return out, RecoveryRebuild, nil
	}
	return nil, "", ErrContextUnavailable
}

// RestoreBudget is the current model/version compatibility envelope.
type RestoreBudget struct{ SchemaVersion, ProjectionVersion, InputCapacity int }

// PreviewRestore proves compatibility and budget before any pointer mutation.
func PreviewRestore(cp ReconstructionCheckpoint, b RestoreBudget) error {
	if cp.SchemaVersion != b.SchemaVersion || cp.ProjectionVersion != b.ProjectionVersion || cp.EstimatedTokens > b.InputCapacity {
		return ErrContextUnavailable
	}
	_, err := ValidateCheckpoint(cp, b.SchemaVersion, b.ProjectionVersion)
	return err
}
