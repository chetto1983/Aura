package plugins

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRemoveFindsConnectorsBySourceAndRecord(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.put("extra", "pack:o/r/p")
	rep, err := b.Remove(context.Background(), "admin", "p", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, name := range []string{"local", "remote", "extra"} {
		if _, still := w.registry.server(name); still {
			t.Errorf("%s is still in the registry", name)
		}
		if s := stepOf(rep.Connectors, name); s.Outcome != OutcomeOK {
			t.Errorf("%s step = %+v", name, s)
		}
	}
	slices.Sort(w.unmounts)
	if !slices.Equal(w.unmounts, []string{"extra", "local", "remote"}) {
		t.Errorf("unmounted %v", w.unmounts)
	}
	if got := w.registry.doc.ProfileServerNames(""); len(got) != 0 {
		t.Errorf("the active profile still lists %v", got)
	}
	if _, ok, _ := b.Get("p"); ok {
		t.Error("the record survived the removal")
	}
	if !slices.Equal(w.registry.updates[1:], []string{"plugin-remove p plugin remove o/r/p@" + commitA}) {
		t.Errorf("updates = %q", w.registry.updates)
	}
}

func TestRemoveSkipsAChangedSkillAndAForeignConnector(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.library.active["s1"] = "sha256:edited"
	w.put("remote", "custom")
	rep, err := b.Remove(context.Background(), "admin", "p", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s := stepOf(rep.Skills, "s1"); s.Outcome != OutcomeSkipped || s.Reason != "changed since install" {
		t.Errorf("s1 step = %+v", s)
	}
	if !slices.Equal(w.library.deleted, []string{"s2"}) {
		t.Errorf("deleted %v, want only s2", w.library.deleted)
	}
	if s := stepOf(rep.Connectors, "remote"); s.Outcome != OutcomeSkipped {
		t.Errorf("remote step = %+v", s)
	}
	if _, kept := w.registry.server("remote"); !kept {
		t.Error("a connector from another source was removed")
	}
}

func TestRemoveKeepsDataUnlessAsked(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	rep, err := b.Remove(context.Background(), "admin", "p", false)
	if err != nil || rep.DataDeleted || len(w.purged) != 0 {
		t.Fatalf("Remove keeping data = (%+v, %v), purged %v", rep, err, w.purged)
	}
	reviewAndInstall(t, b)
	rep, err = b.Remove(context.Background(), "admin", "p", true)
	if err != nil || !rep.DataDeleted || !slices.Equal(w.purged, []string{DataDir("p", testRef)}) {
		t.Fatalf("Remove deleting data = (%+v, %v), purged %v", rep, err, w.purged)
	}
}

func TestRemoveFailureLeavesARecordOfWhatRemains(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.library.failDelete["s2"] = true
	if _, err := b.Remove(context.Background(), "admin", "p", false); err == nil {
		t.Fatal("Remove succeeded with a failed delete")
	}
	rec, ok, _ := b.Get("p")
	if !ok || rec.Phase != PhaseRemoving || len(rec.Skills) != 1 || rec.Skills[0].Name != "s2" || len(rec.Connectors) != 0 {
		t.Fatalf("record = %+v (found %v), want only s2 left, removing", rec, ok)
	}

	w.registry.fail = errors.New("database is unreachable")
	w.library.failDelete["s2"] = false
	if _, err := b.Remove(context.Background(), "admin", "p", false); err == nil {
		t.Fatal("Remove succeeded with a failed registry update")
	}
	if rec, _, _ := b.Get("p"); len(rec.Skills) != 1 {
		t.Errorf("a failed update changed what remains: %+v", rec)
	}
}

// A new version is a removal and a reinstall (spec §2), so a removal leaves nothing the next
// review counts as taken.
func TestRemoveThenReinstallLandsTheSkillsAgain(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	reviewAndInstall(t, b)
	if _, err := b.Remove(context.Background(), "admin", "p", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	rep := reviewAndInstall(t, b)
	for _, name := range []string{"s1", "s2"} {
		if s := stepOf(rep.Skills, name); s.Outcome != OutcomeOK {
			t.Errorf("%s step on the reinstall = %+v", name, s)
		}
	}
}

func TestRemoveOfAnUnknownNameIsNotInstalled(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	if _, err := b.Remove(context.Background(), "admin", "ghost", false); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Remove = %v, want ErrNotInstalled", err)
	}
}

// A removal that waits behind an install acts on the record the install finished with, not the
// one it found while waiting: s2 never landed, so the removal has nothing to say about it. Not
// parallel: the wait is read from the goroutine dump, where another test's Remove would match.
func TestRemoveWaitingOnAnInstallActsOnItsFinalRecord(t *testing.T) {
	w, b := newWorld(t)
	w.library.failInstall["s2"] = true
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	w.library.onInstall = func() { once.Do(func() { close(entered); <-release }) }
	installed := make(chan error, 1)
	go func() {
		_, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest})
		installed <- err
	}()
	<-entered
	type result struct {
		rep RemoveReport
		err error
	}
	removed := make(chan result, 1)
	go func() {
		rep, err := b.Remove(context.Background(), "admin", "p", false)
		removed <- result{rep, err}
	}()
	waitParkedOnALock(t, "plugins.(*Backend).Remove(")
	close(release)
	if err := <-installed; err != nil {
		t.Fatalf("Install: %v", err)
	}
	res := <-removed
	if res.err != nil {
		t.Fatalf("Remove: %v", res.err)
	}
	if len(res.rep.Skills) != 1 || stepOf(res.rep.Skills, "s1").Outcome != OutcomeOK {
		t.Errorf("removal skills = %+v, want s1 deleted and nothing else", res.rep.Skills)
	}
}

// waitParkedOnALock waits until a goroutine in fn is parked on a sync.Mutex. testing/synctest
// cannot tell, since a mutex wait is not durably blocking; the goroutine dump can.
func waitParkedOnALock(t *testing.T, fn string) {
	t.Helper()
	buf := make([]byte, 1<<20)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		for g := range strings.SplitSeq(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
			if strings.Contains(g, "[sync.Mutex.Lock") && strings.Contains(g, fn) {
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatalf("no goroutine in %s parked on a lock", fn)
}
