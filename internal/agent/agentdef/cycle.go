package agentdef

func (r *Registry) DetectCycle(rootID string) []string {
	if r == nil {
		return nil
	}
	refs := make(map[string][]string, len(r.defs))
	for id, def := range r.defs {
		for _, sub := range def.Subagents {
			if sub.ID != "" {
				refs[id] = append(refs[id], sub.ID)
			}
		}
	}
	return detectCycle(refs, rootID)
}
