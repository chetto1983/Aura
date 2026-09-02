package arcadedb

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
)

func cloneMemoryBatchState(state memoryBatchState) memoryBatchState {
	clone := memoryBatchState{Entities: map[string]string{}, Facts: map[string]memoryBatchFact{}}
	maps.Copy(clone.Entities, state.Entities)
	for key, fact := range state.Facts {
		fact.Sources = cloneFactSources(fact.Sources)
		clone.Facts[key] = fact
	}
	return clone
}

func applyCompiledMemoryBatch(
	state memoryBatchState,
	compiled CompiledMemoryBatch,
	now time.Time,
	limits MemoryLimits,
	embeddings map[string][]float64,
) ([]MemoryBatchOperationResult, error) {
	results := make([]MemoryBatchOperationResult, 0, len(compiled.Operations))
	for index, operation := range compiled.Operations {
		result, err := applyMemoryBatchOperation(state, operation, index, now, embeddings)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := validateMemoryBatchFinalState(state, limits, now); err != nil {
		return nil, memoryBatchOperationError(
			len(compiled.Operations)-1, "invalid_final_state", err)
	}
	return results, nil
}

func applyMemoryBatchOperation(
	state memoryBatchState,
	operation MemoryBatchOperation,
	index int,
	now time.Time,
	embeddings map[string][]float64,
) (MemoryBatchOperationResult, error) {
	switch operation.Type {
	case MemoryBatchUpsertFact, MemoryBatchSupersedeFact:
		return applyMemoryBatchFact(state, operation, index, now, embeddings)
	case MemoryBatchMergeEntities:
		return applyMemoryBatchMerge(state, operation, index, now)
	case MemoryBatchForget:
		return applyMemoryBatchForget(state, operation, index)
	default:
		return MemoryBatchOperationResult{}, memoryBatchOperationError(
			index, "malformed_operation", fmt.Errorf("unknown operation type %q", operation.Type))
	}
}

func applyMemoryBatchFact(
	state memoryBatchState,
	operation MemoryBatchOperation,
	index int,
	now time.Time,
	embeddings map[string][]float64,
) (MemoryBatchOperationResult, error) {
	fact := *operation.Fact
	validFrom := fact.ValidFrom
	if validFrom.IsZero() {
		validFrom = now
	}
	identity := factIdentity(fact)
	if existingKey, existing, ok := findActiveMemoryBatchFact(state, identity, now); ok {
		existing.Sources = mergeFactSources(existing.Sources, fact.Source)
		existing.Fact.Source = existing.Sources[0]
		state.Facts[existingKey] = existing
		return MemoryBatchOperationResult{Type: operation.Type, FactKey: existing.FactKey}, nil
	}

	result := MemoryBatchOperationResult{Type: operation.Type}
	if operation.Type == MemoryBatchSupersedeFact {
		targets := resolveMemoryBatchSupersedeTargets(state, fact, identity, now)
		switch len(targets) {
		case 0:
			return MemoryBatchOperationResult{}, memoryBatchOperationError(
				index, "target_not_found", fmt.Errorf("no still-valid fact matches the supersede target"))
		case 1:
			// Exactly one target is the only authorized correction.
		default:
			return MemoryBatchOperationResult{}, memoryBatchOperationError(
				index, "target_ambiguous", fmt.Errorf("%d still-valid facts match the supersede target", len(targets)))
		}
		target := state.Facts[targets[0]]
		target.ValidTo = validFrom
		target.ExpiredAt = now
		target.Fact.ValidTo = validFrom
		target.FactKey = ""
		state.Facts[targets[0]] = target
		result.Superseded = 1
	}

	fact.Supersedes = false
	fact.TargetFactKey = ""
	fact.ValidFrom = validFrom
	stored := memoryBatchFact{
		Fact: fact, Sources: []FactSource{fact.Source}, ValidFrom: validFrom,
		ValidTo: fact.ValidTo, CreatedAt: now, FactKey: stringActiveFactKey(identity, fact.ValidTo, now),
	}
	// Attach the vector to the CREATE. Without it the fact reaches the graph with no
	// embedding and stays invisible to the dense retrieval leg until the
	// memory_embed_backfill sweep runs, which is minutes -- an eternity for a fact the
	// user just asked Aura to remember. A missing key is the fail-soft case and leaves
	// Embedding nil, exactly as before, for the sweep to pick up.
	if vector, ok := embeddings[fact.Statement]; ok {
		stored.Embedding = vector
	}
	key := fmt.Sprintf("new:%06d:%s", index, identity)
	for suffix := 1; ; suffix++ {
		if _, exists := state.Facts[key]; !exists {
			break
		}
		key = fmt.Sprintf("new:%06d:%s:%d", index, identity, suffix)
	}
	state.Facts[key] = stored
	state.Entities[fact.Subject] = preferEntityKind(state.Entities[fact.Subject], fact.SubjectKind)
	state.Entities[fact.Object] = preferEntityKind(state.Entities[fact.Object], fact.ObjectKind)
	result.FactKey = stored.FactKey
	return result, nil
}

func findActiveMemoryBatchFact(
	state memoryBatchState,
	factKey string,
	now time.Time,
) (string, memoryBatchFact, bool) {
	for _, key := range sortedMemoryBatchFactKeys(state) {
		fact := state.Facts[key]
		if memoryBatchFactActive(fact, now) && fact.FactKey == factKey {
			return key, fact, true
		}
	}
	return "", memoryBatchFact{}, false
}

func resolveMemoryBatchSupersedeTargets(
	state memoryBatchState,
	fact Fact,
	newFactKey string,
	now time.Time,
) []string {
	targets := []string{}
	for _, key := range sortedMemoryBatchFactKeys(state) {
		candidate := state.Facts[key]
		if !memoryBatchFactActive(candidate, now) || candidate.FactKey == newFactKey {
			continue
		}
		if fact.TargetFactKey != "" {
			if candidate.FactKey == fact.TargetFactKey {
				targets = append(targets, key)
			}
			continue
		}
		if candidate.Fact.Subject == fact.Subject && candidate.Fact.Predicate == fact.Predicate {
			targets = append(targets, key)
		}
	}
	return targets
}

func applyMemoryBatchMerge(
	state memoryBatchState,
	operation MemoryBatchOperation,
	index int,
	now time.Time,
) (MemoryBatchOperationResult, error) {
	source := operation.Merge.Source
	target := operation.Merge.Target
	sourceKind, exists := state.Entities[source]
	if !exists {
		return MemoryBatchOperationResult{}, memoryBatchOperationError(
			index, "target_not_found", fmt.Errorf("merge source %q does not exist", source))
	}
	state.Entities[target] = preferEntityKind(state.Entities[target], sourceKind)
	moved := 0
	dropped := 0
	for _, key := range sortedMemoryBatchFactKeys(state) {
		fact := state.Facts[key]
		touchesSource := fact.Fact.Subject == source || fact.Fact.Object == source
		if !touchesSource {
			continue
		}
		if (fact.Fact.Subject == source && fact.Fact.Object == target) ||
			(fact.Fact.Subject == target && fact.Fact.Object == source) {
			delete(state.Facts, key)
			dropped++
			continue
		}
		if fact.Fact.Subject == source {
			fact.Fact.Subject = target
			fact.Fact.SubjectKind = preferEntityKind(fact.Fact.SubjectKind, state.Entities[target])
		}
		if fact.Fact.Object == source {
			fact.Fact.Object = target
			fact.Fact.ObjectKind = preferEntityKind(fact.Fact.ObjectKind, state.Entities[target])
		}
		fact.Fact.Statement = strings.ReplaceAll(fact.Fact.Statement, source, target)
		fact.Embedding = nil
		if memoryBatchFactActive(fact, now) {
			fact.FactKey = factIdentity(fact.Fact)
		}
		state.Facts[key] = fact
		moved++
	}
	delete(state.Entities, source)
	dropped += deduplicateMemoryBatchFacts(state, now)
	return MemoryBatchOperationResult{
		Type: operation.Type, Moved: moved, Dropped: dropped,
	}, nil
}

func deduplicateMemoryBatchFacts(state memoryBatchState, now time.Time) int {
	seen := map[string]string{}
	dropped := 0
	for _, key := range sortedMemoryBatchFactKeys(state) {
		fact := state.Facts[key]
		if !memoryBatchFactActive(fact, now) || fact.FactKey == "" {
			continue
		}
		canonical, exists := seen[fact.FactKey]
		if !exists {
			seen[fact.FactKey] = key
			continue
		}
		kept := state.Facts[canonical]
		kept.Sources = mergeFactSources(kept.Sources, fact.Sources...)
		if len(kept.Sources) > 0 {
			kept.Fact.Source = kept.Sources[0]
		}
		state.Facts[canonical] = kept
		delete(state.Facts, key)
		dropped++
	}
	return dropped
}

func applyMemoryBatchForget(
	state memoryBatchState,
	operation MemoryBatchOperation,
	index int,
) (MemoryBatchOperationResult, error) {
	filter := *operation.Forget
	matches := []string{}
	for _, key := range sortedMemoryBatchFactKeys(state) {
		if filter.matchesMemoryBatchFact(state.Facts[key]) {
			matches = append(matches, key)
		}
	}
	if len(matches) == 0 {
		return MemoryBatchOperationResult{}, memoryBatchOperationError(
			index, "target_not_found", fmt.Errorf("forget filter matched no facts"))
	}
	endpoints := []string{}
	for _, key := range matches {
		fact := state.Facts[key]
		endpoints = appendUnique(endpoints, fact.Fact.Subject)
		endpoints = appendUnique(endpoints, fact.Fact.Object)
		if filter.SourceRunID != "" {
			fact.Sources = removeFactSource(fact.Sources, filter.SourceRunID)
			if len(fact.Sources) > 0 {
				fact.Fact.Source = fact.Sources[0]
				state.Facts[key] = fact
				continue
			}
		}
		delete(state.Facts, key)
	}
	if filter.Entity != "" {
		endpoints = appendUnique(endpoints, filter.Entity)
	}
	removedEntities := 0
	if !filter.KeepOrphans {
		for _, entity := range endpoints {
			if _, exists := state.Entities[entity]; exists && !memoryBatchEntityReferenced(state, entity) {
				delete(state.Entities, entity)
				removedEntities++
			}
		}
	}
	return MemoryBatchOperationResult{
		Type: operation.Type, Facts: len(matches), Entities: removedEntities,
	}, nil
}

func validateMemoryBatchFinalState(state memoryBatchState, limits MemoryLimits, now time.Time) error {
	active := map[string]string{}
	for _, key := range sortedMemoryBatchFactKeys(state) {
		fact := state.Facts[key]
		if strings.TrimSpace(fact.Fact.Subject) == "" || strings.TrimSpace(fact.Fact.Object) == "" ||
			strings.TrimSpace(fact.Fact.Predicate) == "" || strings.TrimSpace(fact.Fact.Statement) == "" {
			return fmt.Errorf("fact %q has an empty required field", key)
		}
		for _, field := range []struct {
			name  string
			value string
			limit int
		}{
			{"subject", fact.Fact.Subject, limits.EntityRunes},
			{"predicate", fact.Fact.Predicate, limits.PredicateRunes},
			{"object", fact.Fact.Object, limits.EntityRunes},
			{"statement", fact.Fact.Statement, limits.StatementRunes},
		} {
			if err := validateRuneLimit(field.name, field.value, field.limit); err != nil {
				return err
			}
		}
		if !fact.ValidFrom.IsZero() && !fact.ValidTo.IsZero() && !fact.ValidTo.After(fact.ValidFrom) {
			return fmt.Errorf("fact %q valid_to must be after valid_from", key)
		}
		if !memoryBatchFactActive(fact, now) {
			continue
		}
		identity := factIdentity(fact.Fact)
		if fact.FactKey != identity {
			return fmt.Errorf("fact %q has a stale active identity", key)
		}
		if prior, exists := active[identity]; exists {
			return fmt.Errorf("facts %q and %q have duplicate active identity", prior, key)
		}
		active[identity] = key
	}
	return nil
}

func sortedMemoryBatchFactKeys(state memoryBatchState) []string {
	keys := make([]string, 0, len(state.Facts))
	for key := range state.Facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func memoryBatchFactActive(fact memoryBatchFact, now time.Time) bool {
	return fact.ExpiredAt.IsZero() && (fact.ValidTo.IsZero() || fact.ValidTo.After(now))
}

func memoryBatchEntityReferenced(state memoryBatchState, entity string) bool {
	for _, fact := range state.Facts {
		if fact.Fact.Subject == entity || fact.Fact.Object == entity {
			return true
		}
	}
	return false
}

func stringActiveFactKey(key string, validTo, now time.Time) string {
	if activeFactKey(key, validTo, now) == nil {
		return ""
	}
	return key
}

func preferEntityKind(existing, candidate string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	return candidate
}
