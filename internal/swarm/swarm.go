// Package swarm coordinates parallel agents that can spawn each other and
// talk via a shared message bus. First version is a stub: the real
// implementation lands in a follow-up slice (tier model, DM-by-ID, bus
// channel, MAX_SPAWN_DEPTH=3, payload summarizer).
package swarm

import (
	"context"
	"fmt"
)

// Spawn requests creation of a child agent. Tier is one of chat | reasoning
// | worker; Depth is propagated +1 from the parent.
type Spawn struct {
	Tier   string
	Depth  int
	Goal   string
	Parent string // parent agent ID, empty for root
}

// Coordinator is what the `swarm.spawn` and `swarm.talk` tools delegate
// against. Real implementation lives in the swarm slice.
type Coordinator interface {
	Spawn(ctx context.Context, req Spawn) (id string, err error)
	Talk(ctx context.Context, fromID, toID, message string) error
	Join(ctx context.Context, id string) (lastReport string, err error)
}

const MaxSpawnDepth = 3

// Stub returns "not yet implemented" for any call.
type Stub struct{}

func (Stub) Spawn(_ context.Context, _ Spawn) (string, error) {
	return "", fmt.Errorf("swarm.Coordinator not yet implemented — see swarm slice")
}
func (Stub) Talk(_ context.Context, _, _, _ string) error {
	return fmt.Errorf("swarm.Coordinator not yet implemented — see swarm slice")
}
func (Stub) Join(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("swarm.Coordinator not yet implemented — see swarm slice")
}
