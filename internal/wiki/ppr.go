package wiki

import "sort"

// PersonalizedPageRank runs power-iteration PPR on the wiki GraphIndex.
//
// Only page-level nodes (no subnodes) participate in the random walk.
// Algorithm: r_{t+1} = (1-alpha) * M * r_t + alpha * s
// where M is the column-normalised undirected adjacency and s is the
// restart vector (uniform over valid seeds; uniform over all nodes when
// seeds is empty or none of the seeds are in the index).
//
// Typical call: PersonalizedPageRank(g, []string{"hub-page"}, 0.15, 30)
func PersonalizedPageRank(g *GraphIndex, seeds []string, alpha float64, iter int) map[string]float64 {
	if g == nil {
		return nil
	}
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.15
	}
	if iter <= 0 {
		iter = 30
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Collect only page-level nodes (skip subnode entries).
	nodes := make([]string, 0, len(g.meta))
	for slug := range g.meta {
		if !isSubnodeID(slug) {
			nodes = append(nodes, slug)
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	sort.Strings(nodes) // deterministic ordering

	n := len(nodes)
	idx := make(map[string]int, n)
	for i, slug := range nodes {
		idx[slug] = i
	}

	// Build restart vector: concentrate on valid seeds; fall back to uniform.
	s := make([]float64, n)
	validSeeds := 0
	for _, seed := range seeds {
		if i, ok := idx[seed]; ok {
			s[i] += 1.0
			validSeeds++
		}
	}
	if validSeeds == 0 {
		for i := range s {
			s[i] = 1.0 / float64(n)
		}
	} else {
		for i := range s {
			s[i] /= float64(validSeeds)
		}
	}

	// Pre-compute undirected adjacency (union of outbound and inbound, page-only).
	neighbors := make([][]int, n)
	for i, slug := range nodes {
		seen := make(map[int]bool)
		for t := range g.outbound[slug] {
			if !isSubnodeID(t) {
				if j, ok := idx[t]; ok {
					seen[j] = true
				}
			}
		}
		for t := range g.inbound[slug] {
			if !isSubnodeID(t) {
				if j, ok := idx[t]; ok {
					seen[j] = true
				}
			}
		}
		neighbors[i] = make([]int, 0, len(seen))
		for j := range seen {
			neighbors[i] = append(neighbors[i], j)
		}
	}

	// Initialise r to the restart vector and iterate.
	r := make([]float64, n)
	copy(r, s)
	rNew := make([]float64, n)

	for it := 0; it < iter; it++ {
		for i := range rNew {
			rNew[i] = 0
		}
		for j := range nodes {
			deg := len(neighbors[j])
			if deg == 0 || r[j] == 0 {
				continue
			}
			spread := (1 - alpha) * r[j] / float64(deg)
			for _, nb := range neighbors[j] {
				rNew[nb] += spread
			}
		}
		for i := range rNew {
			rNew[i] += alpha * s[i]
		}
		r, rNew = rNew, r
	}

	result := make(map[string]float64, n)
	for i, slug := range nodes {
		result[slug] = r[i]
	}
	return result
}
