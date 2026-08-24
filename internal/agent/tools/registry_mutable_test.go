package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// registry_mutable_test.go covers the registry becoming writable after boot.
//
// It stopped being immutable because a server behind OAuth cannot be mounted at process
// start: boot has no browser and no identity, so the mount is refused and the server is
// dropped. It is mounted when the human authorizes it, which means tools arrive while turns
// are already reading the manifest.

type fakeTool struct{ name string }

func (f fakeTool) Spec() Spec { return Spec{Name: f.name, Description: "fake"} }

func (f fakeTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// A remount registers the same tool names the previous mount did, so Adopt must replace
// rather than reject. Register still panics on a duplicate — that path is boot wiring,
// where a collision is a bug.
func TestAdoptReplacesAnEarlierMount(t *testing.T) {
	reg := NewRegistry()
	reg.Adopt([]Tool{fakeTool{name: "linear__create_issue"}})
	reg.Adopt([]Tool{fakeTool{name: "linear__create_issue"}, fakeTool{name: "linear__search"}})

	if _, ok := reg.Get("linear__create_issue"); !ok {
		t.Fatal("the re-adopted tool is missing")
	}
	if got := len(reg.All()); got != 2 {
		t.Fatalf("registry holds %d tools, want 2 — a remount duplicated instead of replacing", got)
	}
}

// Forget is the unmount half: a server the operator removed must stop being offered to the
// model without waiting for a restart. It matches on the <server>__ namespace, so a server
// whose name merely PREFIXES another's must not take its tools down with it.
func TestForgetDropsOnlyTheNamedServer(t *testing.T) {
	reg := NewRegistry()
	reg.Adopt([]Tool{
		fakeTool{name: "linear__search"},
		fakeTool{name: "linear__create"},
		fakeTool{name: "linear_extra__search"},
		fakeTool{name: "notion__search"},
	})

	if dropped := reg.Forget("linear__"); dropped != 2 {
		t.Fatalf("Forget dropped %d tools, want 2", dropped)
	}
	for _, survivor := range []string{"linear_extra__search", "notion__search"} {
		if _, ok := reg.Get(survivor); !ok {
			t.Fatalf("Forget took %q with it", survivor)
		}
	}
	if _, ok := reg.Get("linear__search"); ok {
		t.Fatal("a forgotten tool is still registered")
	}
}

// The reason the mutex exists. Mounting a newly authorized server writes the map while
// in-flight turns render the manifest from it; under -race this fails without the lock.
func TestRegistryToleratesMountDuringReads(t *testing.T) {
	reg := NewRegistry()
	reg.Register(fakeTool{name: "text_response"})

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			reg.Adopt([]Tool{fakeTool{name: fmt.Sprintf("linear__tool_%d", i)}})
		}()
		go func() {
			defer wg.Done()
			_ = reg.Render()
			_ = reg.All()
			_, _ = reg.Get("text_response")
		}()
	}
	wg.Wait()

	if _, ok := reg.Get("text_response"); !ok {
		t.Fatal("the boot-registered tool did not survive concurrent mounts")
	}
}
