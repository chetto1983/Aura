package adaptive

import "slices"

var memoryRecallActionCatalog = []string{
	StaticActionID,
	"long_term_top_4",
	"long_term_top_8",
}

func validActionCatalogOrder(domain Domain, actions []string) bool {
	if domain != DomainMemoryRecall {
		return slices.IsSorted(actions)
	}
	return len(actions) > 0 &&
		len(actions) <= len(memoryRecallActionCatalog) &&
		slices.Equal(actions, memoryRecallActionCatalog[:len(actions)])
}
