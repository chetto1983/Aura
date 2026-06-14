package profile

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AddFact inserts fact under Agent.md's Identity section and appends the changelog.
func (s *Store) AddFact(identity, fact string) (bool, error) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return false, fmt.Errorf("add fact: fact cannot be empty")
	}
	current, err := s.ReadProfile(identity)
	if err != nil && !errors.Is(err, ErrProfileNotFound) {
		return false, err
	}
	now := s.now().Format(time.RFC3339)
	p := Profile{
		AgentMD: RenderAgentMD(AgentContent{}),
		Metadata: Metadata{
			Version:       1,
			SchemaVersion: 1,
			GeneratedAt:   now,
			LastUpdatedAt: now,
		},
	}
	if err == nil {
		p.AgentMD = current.AgentMD
		p.Preferences = current.Preferences
		p.Metadata = current.Metadata
		p.Metadata.Version++
		p.Metadata.LastUpdatedAt = now
	}
	updated, added, err := AddFact(p.AgentMD, fact)
	if err != nil {
		return false, err
	}
	if !added {
		return false, nil
	}
	p.AgentMD = updated
	p.Change = "added fact: " + fact
	return true, s.WriteProfile(identity, p)
}
