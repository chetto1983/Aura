package documents

const minimumRerankCandidates = 8

func retrievalCandidateLimit(limit int) int {
	candidates := effectiveLimit(limit) + 2
	return min(seedCandidateLimit, max(minimumRerankCandidates, candidates))
}
