package agui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// orderLog records which reverse leg ran, in the order they ran.
type orderLog struct {
	mu   sync.Mutex
	seen []string
}

func (o *orderLog) add(step string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, step)
	return nil
}

func (o *orderLog) order() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.seen...)
}

type orderedSandbox struct {
	log *orderLog
	err error
}

func (s orderedSandbox) DestroySandbox(context.Context, string) error {
	if s.err != nil {
		return s.err
	}
	return s.log.add("sandbox")
}

type orderedConv struct{ log *orderLog }

func (c orderedConv) PurgeConversations(context.Context, string) error {
	return c.log.add("conversations")
}

type orderedMemory struct{ log *orderLog }

func (m orderedMemory) PurgeMemory(context.Context, string) error { return m.log.add("memory") }

type orderedFS struct{ log *orderLog }

func (f orderedFS) ProvisionIdentityDirs(context.Context, string) error   { return nil }
func (f orderedFS) DeprovisionIdentityDirs(context.Context, string) error { return f.log.add("dirs") }

func sandboxOrderDeps(log *orderLog, sandbox SandboxPurger) DeprovisionDeps {
	return DeprovisionDeps{
		Journal:       newFakeJournal(),
		Sandbox:       sandbox,
		Conversations: orderedConv{log: log},
		Memory:        orderedMemory{log: log},
		Filesystem:    orderedFS{log: log},
	}
}

// The box is the identity's only LIVE COMPUTE: a shell inside it can still write to the
// workspace volume and, through the materialized mounts, to the very filesystem roots the
// dirs leg removes. So it must be torn down before any plane is erased -- otherwise the
// purge races something that is still writing.
func TestDeprovisionDestroysTheBoxBeforeErasingAnyPlane(t *testing.T) {
	t.Parallel()
	log := &orderLog{}
	dep := NewDeprovisioner(sandboxOrderDeps(log, orderedSandbox{log: log}))

	if err := dep.Purge(context.Background(), DeprovisionTarget{IdentityID: testIdentityID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	got := log.order()
	if len(got) == 0 || got[0] != "sandbox" {
		t.Fatalf("legs ran %v; the box must be destroyed FIRST", got)
	}
	if strings.Join(got, ",") != "sandbox,conversations,memory,dirs" {
		t.Fatalf("legs ran %v, want sandbox,conversations,memory,dirs", got)
	}
}

// "We could not reach the daemon" must not be recorded as "there is no box". A step that
// swallowed the error would leave a container named after a deleted identity, which nothing
// else sweeps -- the idle reaper suspends and retains by design.
func TestDeprovisionStopsWhenTheBoxCannotBeDestroyed(t *testing.T) {
	t.Parallel()
	log := &orderLog{}
	dep := NewDeprovisioner(sandboxOrderDeps(log, orderedSandbox{log: log, err: errors.New("sandbox backend unavailable")}))

	if err := dep.Purge(context.Background(), DeprovisionTarget{IdentityID: testIdentityID}); err == nil {
		t.Fatal("Purge must fail when the box cannot be destroyed")
	}
	if got := log.order(); len(got) != 0 {
		t.Fatalf("legs %v ran after the box teardown failed; nothing may be erased", got)
	}
}

// A deployment with no sandbox router wired (no Docker at all) still deprovisions.
func TestDeprovisionSkipsTheBoxLegWhenUnwired(t *testing.T) {
	t.Parallel()
	log := &orderLog{}
	dep := NewDeprovisioner(sandboxOrderDeps(log, nil))

	if err := dep.Purge(context.Background(), DeprovisionTarget{IdentityID: testIdentityID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if strings.Join(log.order(), ",") != "conversations,memory,dirs" {
		t.Fatalf("legs ran %v", log.order())
	}
}
