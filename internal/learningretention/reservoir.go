// Package learningretention bounds and compacts Aura-owned learned examples.
package learningretention

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"time"
)

// Candidate is the content-free metadata needed to make a retention decision.
type Candidate struct {
	Hash          string
	Bucket        string
	UpdatedAt     time.Time
	Quality       float64
	Novelty       float64
	PolicyVersion string
}

// Select returns the candidates retained by the deterministic policy.
func Select(store, bucket, policy string, candidates []Candidate, capacity int) []Candidate {
	if capacity <= 0 || len(candidates) == 0 {
		return nil
	}
	pool := uniqueCandidates(candidates)
	if len(pool) <= capacity {
		sort.Slice(pool, func(i, j int) bool { return pool[i].Hash < pool[j].Hash })
		return pool
	}

	// The newest reservation is exactly ceil(capacity*25/100). The remaining
	// slots use an integer exponential-race approximation: the first 64 bits of
	// SHA-256(store\x00bucket\x00policy\x00hash), divided by the positive fixed-point
	// weight 1+3*quality_milli+2*novelty_milli. Lower priorities win. This keeps
	// the fixture stable across processes and avoids floating-point ranking.
	sort.Slice(pool, func(i, j int) bool {
		if pool[i].UpdatedAt.Equal(pool[j].UpdatedAt) {
			return pool[i].Hash < pool[j].Hash
		}
		return pool[i].UpdatedAt.After(pool[j].UpdatedAt)
	})
	newest := (capacity + 3) / 4
	if newest > len(pool) {
		newest = len(pool)
	}
	selected := append([]Candidate(nil), pool[:newest]...)

	type ranked struct {
		candidate Candidate
		priority  uint64
		random    uint64
	}
	rankedPool := make([]ranked, 0, len(pool)-newest)
	seedPrefix := store + "\x00" + bucket + "\x00" + policy + "\x00"
	for _, candidate := range pool[newest:] {
		digest := sha256.Sum256([]byte(seedPrefix + candidate.Hash))
		random := binary.BigEndian.Uint64(digest[:8])
		weight := uint64(1 + 3*fixedScore(candidate.Quality) + 2*fixedScore(candidate.Novelty))
		rankedPool = append(rankedPool, ranked{candidate: candidate, priority: random / weight, random: random})
	}
	sort.Slice(rankedPool, func(i, j int) bool {
		if rankedPool[i].priority != rankedPool[j].priority {
			return rankedPool[i].priority < rankedPool[j].priority
		}
		if rankedPool[i].random != rankedPool[j].random {
			return rankedPool[i].random < rankedPool[j].random
		}
		return rankedPool[i].candidate.Hash < rankedPool[j].candidate.Hash
	})
	remaining := capacity - len(selected)
	for i := 0; i < remaining && i < len(rankedPool); i++ {
		selected = append(selected, rankedPool[i].candidate)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Hash < selected[j].Hash })
	return selected
}

func fixedScore(value float64) int {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1000
	}
	return int(value*1000 + 0.5)
}

func uniqueCandidates(in []Candidate) []Candidate {
	byHash := make(map[string]Candidate, len(in))
	for _, candidate := range in {
		if candidate.Hash == "" {
			continue
		}
		current, ok := byHash[candidate.Hash]
		if !ok || candidate.UpdatedAt.After(current.UpdatedAt) ||
			(candidate.UpdatedAt.Equal(current.UpdatedAt) && candidate.PolicyVersion > current.PolicyVersion) {
			byHash[candidate.Hash] = candidate
		}
	}
	out := make([]Candidate, 0, len(byHash))
	for _, candidate := range byHash {
		out = append(out, candidate)
	}
	return out
}
