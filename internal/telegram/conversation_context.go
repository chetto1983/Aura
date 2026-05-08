package telegram

// turnRetrievalCapsule is kept as the runtime handoff shape for explicit
// retrieval capsules. Aura no longer routes user text with keyword lists in the
// hot path; compact memory is available through search_memory.
type turnRetrievalCapsule struct {
	Text                 string
	HasEvidence          bool
	SuppressSearchMemory bool
}
