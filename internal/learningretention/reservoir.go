// Package learningretention bounds and compacts Aura-owned learned examples.
package learningretention

import "time"

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
	return nil
}
