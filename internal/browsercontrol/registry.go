// Package browsercontrol records who holds each box browser session: the operator in the
// cockpit's live view, or nobody, in which case the agent may drive it (prd.md §12, "The
// operator takes the browser over", 2026-10-09).
//
// It also remembers which sessions the operator drove since the agent last read their
// references. agent-browser addresses elements by @eN references taken from a snapshot; a
// click or a navigation by the operator renumbers them in the page while the agent still
// holds the old ones in its context, so the first element action after a handoff would land
// on whatever now sits at a stale reference.
//
// A leaf package with no Aura imports: the live-view routes write it, the MCP bridge reads
// it, and neither may import the other.
package browsercontrol

import "sync"

// Holder is the opaque token of the viewer stream that took a session over. Only the holder
// releases its own hold, so a stale stream ending late cannot release a newer one's.
type Holder any

type key struct{ identity, session string }

// Registry is safe for concurrent use. The zero value is ready.
type Registry struct {
	mu    sync.Mutex
	held  map[key]Holder
	stale map[key]struct{}
}

// Hold gives the session to h, replacing a previous holder: the newest viewer that asks is
// the one whose hand is on the page.
func (r *Registry) Hold(identityID, session string, h Holder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held == nil {
		r.held = map[key]Holder{}
	}
	r.held[key{identityID, session}] = h
}

// Release ends h's hold and reports whether h was the holder. A released session is stale:
// the operator may have navigated with a single click.
func (r *Registry) Release(identityID, session string, h Holder) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key{identityID, session}
	if current, ok := r.held[k]; !ok || current != h {
		return false
	}
	delete(r.held, k)
	if r.stale == nil {
		r.stale = map[key]struct{}{}
	}
	r.stale[k] = struct{}{}
	return true
}

// Held reports whether an operator holds the session now.
func (r *Registry) Held(identityID, session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.held[key{identityID, session}]
	return ok
}

// HeldBy reports whether h is the current holder.
func (r *Registry) HeldBy(identityID, session string, h Holder) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.held[key{identityID, session}]
	return ok && current == h
}

// Stale reports whether the operator drove the session since the agent last refreshed it.
func (r *Registry) Stale(identityID, session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.stale[key{identityID, session}]
	return ok
}

// Refresh records that the agent holds fresh references for the session again.
func (r *Registry) Refresh(identityID, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stale, key{identityID, session})
}
