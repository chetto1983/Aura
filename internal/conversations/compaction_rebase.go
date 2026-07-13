package conversations

import (
	"context"
	"errors"
	"fmt"
)

// ErrRebaseUnitTooLarge rejects a canonical unit that cannot fit without splitting.
var ErrRebaseUnitTooLarge = errors.New("canonical semantic unit exceeds rebase chunk capacity")

// RebaseQuality is the frozen scorer output used to choose incremental or canonical work.
type RebaseQuality struct {
	Generation           int
	Coverage, Entailment float64
	Similarity           float64
	Depth                int
}

// NeedsCanonicalRebase applies the exact generation and drift gates.
func NeedsCanonicalRebase(q RebaseQuality) bool {
	return q.Generation >= 4 || q.Depth >= 4 || q.Coverage < 1 || q.Entailment < .98 || q.Similarity < .90
}

// CanonicalSemanticUnit is an indivisible source unit for hierarchical reduction.
type CanonicalSemanticUnit struct {
	Seqs               []int
	Tokens             int
	ManifestDigest     string
	LedgerReferences   []string
	ArtifactReferences []string
}

// RebaseContract freezes every external interpretation dependency.
type RebaseContract struct {
	ModelVersion, PromptVersion, ProbeVersion, ScorerVersion string
	SummarizerCapacity                                       int
}

// RebaseChunk records a complete source interpretation and its reduced data.
type RebaseChunk struct {
	SourceSeqs []int
	Summary    string
}

// RebaseResult retains every interpretation version and hierarchical chunk.
type RebaseResult struct {
	Contract RebaseContract
	Chunks   []RebaseChunk
}

// RebaseSummarizer reduces one bounded set of complete canonical units.
type RebaseSummarizer func(context.Context, []CanonicalSemanticUnit) (RebaseChunk, error)

// RebaseCanonical partitions canonical units into chunks no larger than 60% capacity.
func RebaseCanonical(ctx context.Context, units []CanonicalSemanticUnit, contract RebaseContract, summarize RebaseSummarizer) (RebaseResult, error) {
	if contract.ModelVersion == "" || contract.PromptVersion == "" || contract.ProbeVersion == "" || contract.ScorerVersion == "" || contract.SummarizerCapacity <= 0 || summarize == nil {
		return RebaseResult{}, fmt.Errorf("invalid frozen rebase contract")
	}
	limit := contract.SummarizerCapacity * 60 / 100
	result := RebaseResult{Contract: contract}
	for start := 0; start < len(units); {
		end, tokens := start, 0
		for end < len(units) && tokens+units[end].Tokens <= limit {
			tokens += units[end].Tokens
			end++
		}
		if end == start {
			return RebaseResult{}, ErrRebaseUnitTooLarge
		}
		chunk, err := summarize(ctx, append([]CanonicalSemanticUnit(nil), units[start:end]...))
		if err != nil {
			return RebaseResult{}, err
		}
		result.Chunks = append(result.Chunks, chunk)
		start = end
	}
	return result, nil
}

// RebaseState is the active/LKG control state changed only after successful validation.
type RebaseState struct {
	ActiveCheckpointID     string
	AutoGenerationsEnabled bool
	QuarantineReason       string
}

// ApplyRebase preserves the LKG and disables automation when rebase fails.
func ApplyRebase(prior RebaseState, result RebaseResult, err error) RebaseState {
	if err != nil {
		prior.AutoGenerationsEnabled = false
		prior.QuarantineReason = err.Error()
		return prior
	}
	prior.QuarantineReason = ""
	return prior
}
