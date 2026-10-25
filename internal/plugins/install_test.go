package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
)

func TestInstallOfTheReviewedCommitAfterTheBranchMoved(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	w.mu.Lock()
	w.commits[commitB] = pluginFiles("2.0.0")
	w.head = commitB
	w.mu.Unlock()
	if _, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	rec, ok, err := b.Get("p")
	if err != nil || !ok || rec.Commit != commitA || rec.Version != "1.0.0" {
		t.Fatalf("record = (%+v, %v, %v), want the reviewed commit", rec, ok, err)
	}
}

func TestInstallRefusesAChangedDigestAndWritesNothing(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	w.put("local", "custom")
	_, err = b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest})
	if !errors.Is(err, ErrDigestChanged) || !strings.Contains(err.Error(), "did not move") {
		t.Fatalf("Install = %v, want ErrDigestChanged saying the commit did not move", err)
	}
	if recs, _ := b.List(); len(recs) != 0 {
		t.Errorf("records = %+v", recs)
	}
	if entries, _ := os.ReadDir(b.d.Store.Root); len(entries) != 0 {
		t.Errorf("the store holds %d entries", len(entries))
	}
	if len(w.library.installed) != 0 || len(w.registry.updates) != 0 || len(w.verified) != 0 || len(w.mounted) != 0 {
		t.Errorf("calls were made: skills %v, updates %v, verify %v, mounts %v",
			w.library.installed, w.registry.updates, w.verified, w.mounted)
	}
}

func TestInstallLandsSkillsFromTheKeptTree(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	rep := reviewAndInstall(t, b)
	want := []string{filepath.Join(b.d.Store.TreeDir("p"), "skills", "s1"), filepath.Join(b.d.Store.TreeDir("p"), "skills", "s2")}
	if !slices.Equal(w.library.installed, want) {
		t.Errorf("installed from %v, want %v", w.library.installed, want)
	}
	if !slices.Equal(w.library.sources, []string{"pack:o/r/p", "pack:o/r/p"}) {
		t.Errorf("sources = %v", w.library.sources)
	}
	if s := stepOf(rep.Skills, "s2"); s.Outcome != OutcomeOK {
		t.Errorf("s2 step = %+v", s)
	}
}

func TestInstallVerifiesStdioBeforeTheSaveAndSkipsAFailure(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	w.failVer["local"] = true
	rep := reviewAndInstall(t, b)
	if !slices.Equal(w.verified, []string{"local"}) {
		t.Errorf("verified %v, want only the stdio connector", w.verified)
	}
	if s := stepOf(rep.Connectors, "local"); s.Outcome != OutcomeFailed || !strings.Contains(s.Reason, "handshake failed") {
		t.Errorf("local step = %+v", s)
	}
	if _, saved := w.registry.server("local"); saved {
		t.Error("a connector that failed its verification was saved")
	}
	if _, saved := w.registry.server("remote"); !saved {
		t.Error("the remote connector was not saved")
	}
}

func TestInstallUpdatesTheRegistryOnceAndMountsEach(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	if want := []string{"plugin-install p plugin install o/r/p@" + commitA}; !slices.Equal(w.registry.updates, want) {
		t.Errorf("updates = %q, want %q", w.registry.updates, want)
	}
	if !slices.Equal(w.mounted, []string{"local", "remote"}) {
		t.Errorf("mounted %v", w.mounted)
	}
	if got := w.registry.doc.ProfileServerNames(""); !slices.Equal(got, []string{"local", "remote"}) {
		t.Errorf("active profile = %v", got)
	}
}

func TestInstallTrustsByTheConfirmation(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	local, _ := w.registry.server("local")
	if local.Trust.Class != mcp.TrustSandboxedLocal || local.Trust.ApprovedBy != "admin" ||
		local.Trust.Reason != "plugin install o/r/p@"+commitA || local.Trust.ApprovedAt != "2026-10-25T12:00:00Z" {
		t.Errorf("local trust = %+v", local.Trust)
	}
	if local.Source != "pack:o/r/p" || local.Runtime.Kind != mcp.RuntimeKindBox {
		t.Errorf("local = %+v", local)
	}
	if remote, _ := w.registry.server("remote"); remote.Trust.Class != mcp.TrustRemoteHTTP {
		t.Errorf("remote trust = %+v", remote.Trust)
	}
}

func TestInstallReportsAFailedRemoteProbeAsAttention(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	w.failProb["remote"] = true
	rep := reviewAndInstall(t, b)
	s := stepOf(rep.Connectors, "remote")
	if s.Outcome != OutcomeAttention || !strings.Contains(s.Reason, "401") {
		t.Errorf("remote step = %+v", s)
	}
	if !slices.Equal(w.probed, []string{"remote"}) {
		t.Errorf("probed %v, want only the remote connector", w.probed)
	}
	if rec, _, _ := b.Get("p"); !slices.Contains(rec.Connectors, "remote") {
		t.Errorf("a saved connector needing attention left the record: %+v", rec)
	}
}

func TestInstallRecordExistsBeforeVerifyAndNamesWhatLanded(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	w.failVer["local"] = true
	var during Record
	w.onVerify = func() { during, _, _ = b.Get("p") }
	rep := reviewAndInstall(t, b)
	if during.Phase != PhaseInstalling || !slices.Equal(during.Connectors, []string{"local", "remote"}) {
		t.Errorf("record during verify = %+v", during)
	}
	rec, _, _ := b.Get("p")
	if rec.Phase != PhaseInstalled || !slices.Equal(rec.Connectors, []string{"remote"}) || len(rec.Skills) != 2 ||
		rec.Skills[0].Hash != rep.Review.Skills[0].Hash || rec.InstalledBy != "admin" || rec.Digest == "" {
		t.Errorf("final record = %+v", rec)
	}
}

// A removal after a crash mid-install reads the record written at the claim, so that record carries
// the hash each skill lands with: an always:true skill lands without the flag.
func TestInstallClaimsWithTheHashEachSkillLandsWith(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	w.commits[commitA]["p/skills/s1/SKILL.md"] = "---\nname: s1\ndescription: d\nalways: true\n---\nbody"
	var claimed Record
	w.library.onInstall = func() {
		if claimed.Name == "" {
			claimed, _, _ = b.Get("p")
		}
	}
	reviewAndInstall(t, b)
	rec, _, _ := b.Get("p")
	if len(rec.Skills) != 2 || !slices.Equal(claimed.Skills, rec.Skills) {
		t.Errorf("claimed with %+v, landed %+v", claimed.Skills, rec.Skills)
	}
}

func TestInstallSurvivesACancelledRequest(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.afterCO = cancel
	if _, err := b.Install(ctx, "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if rec, _, _ := b.Get("p"); rec.Phase != PhaseInstalled {
		t.Errorf("phase = %q, want installed", rec.Phase)
	}
}

func TestInstallFailurePoints(t *testing.T) {
	t.Parallel()
	t.Run("after Claim the record names everything reviewed", func(t *testing.T) {
		t.Parallel()
		w, b := newWorld(t)
		var first Record
		var once sync.Once
		w.library.onInstall = func() { once.Do(func() { first, _, _ = b.Get("p") }) }
		reviewAndInstall(t, b)
		if first.Phase != PhaseInstalling || len(first.Skills) != 2 || !slices.Equal(first.Connectors, []string{"local", "remote"}) {
			t.Errorf("record at the first skill = %+v", first)
		}
	})
	t.Run("a failed registry update keeps the landed skills", func(t *testing.T) {
		t.Parallel()
		w, b := newWorld(t)
		w.registry.fail = errors.New("database is unreachable")
		r, _ := b.Review(context.Background(), "o/r/p")
		_, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest})
		if err == nil || !strings.Contains(err.Error(), "database is unreachable") {
			t.Fatalf("Install = %v, want the update's error", err)
		}
		rec, ok, _ := b.Get("p")
		if !ok || len(rec.Skills) != 2 || len(rec.Connectors) != 0 || len(w.mounted) != 0 {
			t.Errorf("record = %+v (found %v), mounts %v", rec, ok, w.mounted)
		}
	})
	t.Run("nothing landed leaves no directory", func(t *testing.T) {
		t.Parallel()
		w, b := newWorld(t)
		w.library.failInstall["s1"], w.library.failInstall["s2"] = true, true
		w.registry.fail = errors.New("database is unreachable")
		r, _ := b.Review(context.Background(), "o/r/p")
		if _, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest}); err == nil {
			t.Fatal("Install succeeded")
		}
		if _, err := os.Stat(filepath.Join(b.d.Store.Root, "p")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the plugin directory is still there: %v", err)
		}
	})
}

func TestConcurrentInstallsOfOneNameOneWins(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest})
		})
	}
	wg.Wait()
	refused := 0
	for _, err := range errs {
		switch {
		case errors.Is(err, ErrAlreadyInstalled):
			refused++
		case err != nil:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if refused != 1 {
		t.Errorf("%d installs refused, want exactly 1: %v", refused, errs)
	}
}

func TestInstallOfAnInstalledNameIsRefused(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	reviewAndInstall(t, b)
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if _, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: commitA, Digest: r.Digest}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second Install = %v, want ErrAlreadyInstalled", err)
	}
}

func TestInstallRefusesAMalformedRequest(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	for name, req := range map[string]InstallRequest{
		"bad ref":    {Ref: "o", Commit: commitA, Digest: "d"},
		"bad commit": {Ref: "o/r/p", Commit: "main", Digest: "d"},
		"no digest":  {Ref: "o/r/p", Commit: commitA},
	} {
		if _, err := b.Install(context.Background(), "admin", req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: %v, want ErrInvalidRequest", name, err)
		}
	}
	if _, err := b.Review(context.Background(), "o"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("review of a bad ref: %v, want ErrInvalidRequest", err)
	}
	if _, err := b.Remove(context.Background(), "admin", "../x", false); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("removal of a bad name: %v, want ErrInvalidRequest", err)
	}
	if ok, err := b.MayAdminister(context.Background(), "admin"); !ok || err != nil {
		t.Errorf("MayAdminister(admin) = (%v, %v)", ok, err)
	}
}

func TestAnUnreadableRegistryFailsTheReview(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	b.d.Taken = func() (Taken, error) { return Taken{}, errors.New("database is unreachable") }
	if _, err := b.Review(context.Background(), "o/r/p"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("Review = %v, want the registry's error", err)
	}
}

// The cockpit's review route is classified write-free on this account.
func TestReviewWritesNothing(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	var root string
	prev := b.d.Checkout
	b.d.Checkout = func(ctx context.Context, ref packs.Ref, commit string) (packs.Checkout, error) {
		c, err := prev(ctx, ref, commit)
		root = c.Root
		return c, err
	}
	if _, err := b.Review(context.Background(), "o/r/p"); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if _, err := os.Stat(b.d.Store.Root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the review created the plugins dir: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the review left its checkout behind: %v", err)
	}
	if len(w.library.installed) != 0 || len(w.registry.updates) != 0 || len(w.verified) != 0 || len(w.mounted) != 0 {
		t.Errorf("the review made calls: %v %v %v %v", w.library.installed, w.registry.updates, w.verified, w.mounted)
	}
}
