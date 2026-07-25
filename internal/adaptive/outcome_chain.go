package adaptive

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const maxCorrectionChainLength = 64

var (
	// ErrCorrectionTargetNotFound rejects links to absent or cross-scope facts.
	ErrCorrectionTargetNotFound = errors.New("adaptive correction target not found")
	// ErrCorrectionScopeMismatch rejects links across evaluator provenance.
	ErrCorrectionScopeMismatch = errors.New("adaptive correction target scope mismatch")
	// ErrCorrectionFork rejects a second correction for one effective leaf.
	ErrCorrectionFork = errors.New("adaptive correction chain forks")
	// ErrCorrectionCycle rejects cyclic correction history.
	ErrCorrectionCycle = errors.New("adaptive correction chain cycles")
	// ErrCorrectionChainTooLong bounds correction reconstruction work.
	ErrCorrectionChainTooLong = errors.New("adaptive correction chain is too long")
	// ErrCorrectionChainInvalid rejects malformed persisted correction history.
	ErrCorrectionChainInvalid = errors.New("adaptive correction chain is invalid")
)

type outcomeChainFact struct {
	id         uuid.UUID
	kind       EventKind
	evaluator  EvaluatorProvenance
	supersedes uuid.UUID
}

func validateCorrectionAppend(
	facts []outcomeChainFact,
	correctionEventID uuid.UUID,
	correction CorrectionObservation,
) error {
	byID := make(map[uuid.UUID]outcomeChainFact, len(facts))
	children := make(map[uuid.UUID]uuid.UUID, len(facts))
	for _, fact := range facts {
		if fact.id == uuid.Nil || (fact.kind != EventOutcome && fact.kind != EventCorrection) {
			return ErrCorrectionChainInvalid
		}
		if _, exists := byID[fact.id]; exists {
			return ErrCorrectionChainInvalid
		}
		byID[fact.id] = fact
	}
	for _, fact := range facts {
		if fact.kind != EventCorrection {
			continue
		}
		target, exists := byID[fact.supersedes]
		if !exists {
			return ErrCorrectionChainInvalid
		}
		if target.evaluator != fact.evaluator {
			return ErrCorrectionScopeMismatch
		}
		if _, forked := children[fact.supersedes]; forked {
			return ErrCorrectionFork
		}
		children[fact.supersedes] = fact.id
	}
	if err := validateCorrectionGraph(byID); err != nil {
		return err
	}
	target, exists := byID[correction.SupersedesEventID]
	if !exists {
		return ErrCorrectionTargetNotFound
	}
	if target.evaluator != correction.Evaluator {
		return ErrCorrectionScopeMismatch
	}
	if _, forked := children[correction.SupersedesEventID]; forked {
		return ErrCorrectionFork
	}
	if correctionEventID == correction.SupersedesEventID {
		return ErrCorrectionCycle
	}
	depth := 1
	cursor := target
	seen := map[uuid.UUID]struct{}{correctionEventID: {}}
	for {
		if _, exists := seen[cursor.id]; exists {
			return ErrCorrectionCycle
		}
		seen[cursor.id] = struct{}{}
		if cursor.kind == EventOutcome {
			break
		}
		parent, exists := byID[cursor.supersedes]
		if !exists {
			return ErrCorrectionChainInvalid
		}
		cursor = parent
		depth++
		if depth >= maxCorrectionChainLength {
			return ErrCorrectionChainTooLong
		}
	}
	return nil
}

func validateCorrectionGraph(facts map[uuid.UUID]outcomeChainFact) error {
	for id := range facts {
		seen := make(map[uuid.UUID]struct{}, maxCorrectionChainLength)
		cursor := facts[id]
		for cursor.kind == EventCorrection {
			if _, exists := seen[cursor.id]; exists {
				return ErrCorrectionCycle
			}
			seen[cursor.id] = struct{}{}
			if len(seen) > maxCorrectionChainLength {
				return ErrCorrectionChainTooLong
			}
			parent, exists := facts[cursor.supersedes]
			if !exists {
				return fmt.Errorf("%w: correction %s", ErrCorrectionChainInvalid, cursor.id)
			}
			cursor = parent
		}
	}
	return nil
}

func foldOutcomeChain(
	facts []outcomeChainFact,
	rootID uuid.UUID,
) (outcomeChainFact, error) {
	byID := make(map[uuid.UUID]outcomeChainFact, len(facts))
	children := make(map[uuid.UUID]uuid.UUID, len(facts))
	for _, fact := range facts {
		if fact.id == uuid.Nil || (fact.kind != EventOutcome && fact.kind != EventCorrection) {
			return outcomeChainFact{}, ErrCorrectionChainInvalid
		}
		if _, exists := byID[fact.id]; exists {
			return outcomeChainFact{}, ErrCorrectionChainInvalid
		}
		byID[fact.id] = fact
		if fact.kind == EventCorrection {
			if _, forked := children[fact.supersedes]; forked {
				return outcomeChainFact{}, ErrCorrectionFork
			}
			children[fact.supersedes] = fact.id
		}
	}
	if err := validateCorrectionGraph(byID); err != nil {
		return outcomeChainFact{}, err
	}
	cursor, exists := byID[rootID]
	if !exists || cursor.kind != EventOutcome {
		return outcomeChainFact{}, ErrCorrectionTargetNotFound
	}
	for depth := 0; ; depth++ {
		if depth >= maxCorrectionChainLength {
			return outcomeChainFact{}, ErrCorrectionChainTooLong
		}
		childID, exists := children[cursor.id]
		if !exists {
			return cursor, nil
		}
		cursor = byID[childID]
	}
}
