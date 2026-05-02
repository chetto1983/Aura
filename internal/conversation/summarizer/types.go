package summarizer

// Action is the dedup decision for a candidate fact.
type Action string

const (
	ActionSkip  Action = "skip"  // already covered in wiki (sim > patchThreshold)
	ActionPatch Action = "patch" // exists but worth augmenting (newThreshold <= sim <= patchThreshold)
	ActionNew   Action = "new"   // genuinely new (sim < newThreshold)
)

func (a Action) String() string { return string(a) }

// Candidate is one extracted fact from a conversation turn batch.
type Candidate struct {
	Fact          string   `json:"fact"`
	Score         float64  `json:"score"`
	Category      string   `json:"category"` // person|project|preference|fact|todo
	RelatedSlugs  []string `json:"related_slugs"`
	SourceTurnIDs []int64  `json:"source_turn_ids"`
}

// Decision is what the deduper recommends for a Candidate.
type Decision struct {
	Candidate  Candidate
	Action     Action
	TargetSlug string  // non-empty when Action==ActionPatch
	Similarity float64 // top cosine similarity from wiki search
}
