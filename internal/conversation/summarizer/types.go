package summarizer

// Action is the dedup decision for a candidate fact.
type Action string

const (
	ActionSkip  Action = "skip"  // already covered in wiki (sim > patchThreshold)
	ActionPatch Action = "patch" // exists but worth augmenting (newThreshold <= sim <= patchThreshold)
	ActionNew   Action = "new"   // genuinely new (sim < newThreshold)

	ActionSkillCreate Action = "skill_create" // proposed procedural memory addition
	ActionSkillUpdate Action = "skill_update" // proposed procedural memory patch
	ActionSkillDelete Action = "skill_delete" // proposed procedural memory removal
)

func (a Action) String() string { return string(a) }

// IsWikiAction reports whether action can be applied by the wiki AutoApplier.
func IsWikiAction(action string) bool {
	switch Action(action) {
	case ActionNew, ActionPatch, ActionSkip:
		return true
	default:
		return false
	}
}

// IsSkillAction reports whether action represents a procedural-memory skill
// proposal. Generic review approval marks these reviewed only; installation
// and smoke tests stay in an explicit admin skill workflow.
func IsSkillAction(action string) bool {
	switch Action(action) {
	case ActionSkillCreate, ActionSkillUpdate, ActionSkillDelete:
		return true
	default:
		return false
	}
}

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
