package browsercontrol

import (
	"sync"
	"testing"
	"testing/quick"
)

const (
	alice = "11111111-1111-1111-1111-111111111111"
	bob   = "22222222-2222-2222-2222-222222222222"
)

func TestHoldReleaseMarksStale(t *testing.T) {
	var r Registry
	viewer := new(int)
	if r.Held(alice, "portal") || r.Stale(alice, "portal") {
		t.Fatal("a fresh registry holds nothing and nothing is stale")
	}
	r.Hold(alice, "portal", viewer)
	if !r.Held(alice, "portal") || !r.HeldBy(alice, "portal", viewer) {
		t.Fatal("hold did not take")
	}
	if r.Stale(alice, "portal") {
		t.Fatal("a session is stale on release, not on hold")
	}
	if !r.Release(alice, "portal", viewer) {
		t.Fatal("the holder could not release")
	}
	if r.Held(alice, "portal") || !r.Stale(alice, "portal") {
		t.Fatal("release must free the session and mark it stale")
	}
	r.Refresh(alice, "portal")
	if r.Stale(alice, "portal") {
		t.Fatal("refresh must clear stale")
	}
}

func TestOnlyTheHolderReleases(t *testing.T) {
	var r Registry
	first, second := new(int), new(int)
	r.Hold(alice, "portal", first)
	r.Hold(alice, "portal", second)
	if r.HeldBy(alice, "portal", first) {
		t.Fatal("a newer hold must replace the older one")
	}
	if r.Release(alice, "portal", first) {
		t.Fatal("a replaced holder must not release the newer hold")
	}
	if !r.Held(alice, "portal") || r.Stale(alice, "portal") {
		t.Fatal("a failed release changed the session")
	}
	if r.Release(alice, "unknown", first) {
		t.Fatal("releasing a session nobody holds reports false")
	}
}

func TestSessionsAreIdentityScoped(t *testing.T) {
	var r Registry
	r.Hold(alice, "portal", new(int))
	if r.Held(bob, "portal") {
		t.Fatal("bob's portal is not alice's")
	}
	if r.Held(alice, "bank") {
		t.Fatal("another session of the same identity is not held")
	}
}

// Property: after any sequence of operations, a session the agent just refreshed is never
// stale, and a session is never both held by h and released by h in the same breath.
func TestRefreshAlwaysClearsStale(t *testing.T) {
	f := func(ops []uint8) bool {
		var r Registry
		holders := []Holder{new(int), new(int)}
		for _, op := range ops {
			h := holders[int(op)%2]
			switch op % 4 {
			case 0:
				r.Hold(alice, "s", h)
			case 1:
				r.Release(alice, "s", h)
			case 2:
				r.Refresh(alice, "s")
			case 3:
				_ = r.Stale(alice, "s")
			}
		}
		r.Refresh(alice, "s")
		return !r.Stale(alice, "s")
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryIsSafeForConcurrentUse(t *testing.T) {
	var r Registry
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			h := new(int)
			r.Hold(alice, "s", h)
			_ = r.Held(alice, "s")
			_ = r.Stale(alice, "s")
			r.Release(alice, "s", h)
			if i%2 == 0 {
				r.Refresh(alice, "s")
			}
		})
	}
	wg.Wait()
}
