package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/objectstore"
)

// The boot moves old Studio projects only when the daemon has rows to read them from; an
// unwired daemon gets nil, which the boot skips, never a pass that panics on a nil store.
func TestBuildStudioProjectRekey(t *testing.T) {
	if rekey := buildStudioProjectRekey(&chatEnv{}, objectstore.NewFake()); rekey != nil {
		t.Fatalf("an unwired daemon built %#v", rekey)
	}
	pool := newLazyPool(t)
	svc := &assets.Service{}
	rekey := buildStudioProjectRekey(&chatEnv{
		cfg: &config.Config{ObjectStoreBucket: "aura-assets"}, pool: pool, assets: svc, identity: identity.New(pool),
	}, objectstore.NewFake())
	if rekey == nil || rekey.Assets != svc || rekey.Files == nil || rekey.Files.Rows == nil || rekey.Identities == nil {
		t.Fatalf("rekey = %#v, want the daemon's asset service, a file manager with rows, every identity", rekey)
	}
	if rekey.Files.SharedBucket != "aura-assets" {
		t.Fatalf("SharedBucket = %q, want the configured bucket", rekey.Files.SharedBucket)
	}
}

type rekeyIdentities struct {
	ids []string
	err error
}

func (r rekeyIdentities) IdentityIDs(context.Context) ([]string, error) { return r.ids, r.err }

// stalledIdentities is an identity store that never answers: it returns only when its context ends.
type stalledIdentities struct{}

func (stalledIdentities) IdentityIDs(ctx context.Context) ([]string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// The boot logs what the pass did, and a failure is a warning, never a stopped boot.
func TestRekeyStudioProjectsLogsAndNeverFails(t *testing.T) {
	logs := captureLog(t)

	rekeyStudioProjects(context.Background(), nil, time.Minute)
	rekeyStudioProjects(context.Background(), &assets.StudioProjectRekey{Identities: rekeyIdentities{}}, time.Minute)
	rekeyStudioProjects(context.Background(), &assets.StudioProjectRekey{
		Identities: rekeyIdentities{err: errors.New("identity store down")},
	}, time.Minute)

	for _, want := range []string{
		`level=INFO msg="aura serve: studio project re-key done" moved=0`,
		`level=WARN msg="aura serve: studio project re-key left projects in the index" moved=0`,
		"identity store down",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}
}

// The pass runs before HTTP serves, so a store that never answers must not hold the boot: at its
// deadline the pass gives up with a warning and the boot goes on.
func TestRekeyStudioProjectsGivesUpAtItsDeadline(t *testing.T) {
	logs := captureLog(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		rekeyStudioProjects(context.Background(), &assets.StudioProjectRekey{Identities: stalledIdentities{}}, 20*time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pass still held the boot 5s past a 20ms deadline")
	}

	for _, want := range []string{
		`level=WARN msg="aura serve: studio project re-key left projects in the index" moved=0`,
		"context deadline exceeded",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}
}
