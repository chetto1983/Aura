package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

const (
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var testRef = packs.Ref{Source: "o/r", Directory: "p"}

// pluginFiles is plugin "p" at one commit: two skills, a stdio connector and a remote one.
func pluginFiles(version string) map[string]string {
	return map[string]string{
		"p/.claude-plugin/plugin.json": `{"name":"p","version":"` + version + `"}`,
		"p/skills/s1/SKILL.md":         skillMD("s1"),
		"p/skills/s2/SKILL.md":         skillMD("s2"),
		"p/.mcp.json": `{"mcpServers":{"local":{"command":"uvx","args":["srv"]},
			"remote":{"type":"http","url":"https://r.example/mcp"}}}`,
	}
}

// world is every fake the backend talks to, each recording its calls.
type world struct {
	t        *testing.T
	mu       sync.Mutex
	commits  map[string]map[string]string
	head     string
	afterCO  func()
	library  *fakeLibrary
	registry *fakeRegistry
	verified []string
	failVer  map[string]bool
	onVerify func()
	mounted  []string
	unmounts []string
	probed   []string
	failProb map[string]bool
	purged   []string
	order    []string
}

func newWorld(t *testing.T) (*world, *Backend) {
	t.Helper()
	w := &world{
		t:        t,
		commits:  map[string]map[string]string{commitA: pluginFiles("1.0.0")},
		head:     commitA,
		library:  &fakeLibrary{active: map[string]string{}, failInstall: map[string]bool{}, failArchive: map[string]bool{}},
		registry: &fakeRegistry{doc: mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{}}},
		failVer:  map[string]bool{},
		failProb: map[string]bool{},
	}
	b := New(Deps{
		Store:    Store{Root: filepath.Join(t.TempDir(), "plugins")},
		Checkout: w.checkout,
		Skills:   w.library,
		Registry: w.registry,
		Verify: func(_ context.Context, name string, s mcp.ManagedServer) (mcp.ManagedServer, error) {
			w.mu.Lock()
			w.verified = append(w.verified, name)
			w.order = append(w.order, "verify "+name)
			hook := w.onVerify
			fail := w.failVer[name]
			w.mu.Unlock()
			if hook != nil {
				hook()
			}
			if fail {
				return mcp.ManagedServer{}, errors.New("handshake failed")
			}
			return s, nil
		},
		Mount: func(_ context.Context, name string, _ mcp.ManagedServer) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.mounted = append(w.mounted, name)
		},
		Unmount: func(name string) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.unmounts = append(w.unmounts, name)
		},
		Probe: func(_ context.Context, name string, _ mcp.ManagedServer) mcp.ProbeResult {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.probed = append(w.probed, name)
			if w.failProb[name] {
				return mcp.ProbeResult{Name: name, Err: "401 unauthorized"}
			}
			return mcp.ProbeResult{Name: name, OK: true}
		},
		PurgeData: func(_ context.Context, dataDir string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.purged = append(w.purged, dataDir)
			return nil
		},
		Admins: func(_ context.Context, id string) (bool, error) { return id == "admin", nil },
		Taken: func() Taken {
			return Taken{Skill: w.library.Exists, Server: w.registry.source}
		},
		BodyCapBytes: 32768,
		Now:          func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) },
	})
	return w, b
}

// checkout writes the requested commit's files into a fresh directory: the backend closes,
// and so removes, every checkout it is handed.
func (w *world) checkout(_ context.Context, ref packs.Ref, commit string) (packs.Checkout, error) {
	w.mu.Lock()
	if commit == "" {
		commit = w.head
	}
	files, ok := w.commits[commit]
	after := w.afterCO
	w.mu.Unlock()
	if !ok {
		return packs.Checkout{}, fmt.Errorf("no commit %s", commit)
	}
	root, err := os.MkdirTemp(w.t.TempDir(), "co-*")
	if err != nil {
		return packs.Checkout{}, err
	}
	for path, content := range files {
		writeFile(w.t, filepath.Join(root, filepath.FromSlash(path)), content, 0o644)
	}
	pk, err := packs.ReadTree(root, ref)
	if err != nil {
		return packs.Checkout{}, err
	}
	if after != nil {
		after()
	}
	return packs.Checkout{Root: root, Commit: commit, Packs: pk}, nil
}

type fakeLibrary struct {
	mu          sync.Mutex
	active      map[string]string
	installed   []string
	sources     []string
	archived    []string
	failInstall map[string]bool
	failArchive map[string]bool
	onInstall   func()
}

func (l *fakeLibrary) Exists(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.active[name]
	return ok || slices.Contains(l.archived, name)
}

func (l *fakeLibrary) InstallDir(_ context.Context, dir, source string, _ skills.AuditActor) (string, error) {
	l.mu.Lock()
	hook := l.onInstall
	l.mu.Unlock()
	if hook != nil {
		hook()
	}
	name := filepath.Base(dir)
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failInstall[name] {
		return "", errors.New("blocklisted")
	}
	l.installed = append(l.installed, dir)
	l.sources = append(l.sources, source)
	l.active[name] = "sha256:landed-" + name
	return l.active[name], nil
}

func (l *fakeLibrary) ActiveHash(name string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.active[name]
	return h, ok
}

func (l *fakeLibrary) Archive(_ context.Context, name string, _ skills.AuditActor) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failArchive[name] {
		return errors.New("archive failed")
	}
	delete(l.active, name)
	l.archived = append(l.archived, name)
	return nil
}

type fakeRegistry struct {
	mu      sync.Mutex
	doc     mcp.ManagedConfig
	updates []string
	fail    error
}

// Update runs fn on a copy of the document and keeps it only when fn succeeds, as a save does.
func (r *fakeRegistry) Update(_ context.Context, _, action, subject, reason string, fn func(*mcp.ManagedConfig) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	raw, err := json.Marshal(r.doc)
	if err != nil {
		return err
	}
	var doc mcp.ManagedConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if err := fn(&doc); err != nil {
		return err
	}
	r.doc = doc
	r.updates = append(r.updates, action+" "+subject+" "+reason)
	return nil
}

func (r *fakeRegistry) source(name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.doc.MCPServers[name]
	return s.Source, ok
}

func (r *fakeRegistry) server(name string) (mcp.ManagedServer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.doc.MCPServers[name]
	return s, ok
}

func (w *world) put(name, source string) {
	w.registry.mu.Lock()
	defer w.registry.mu.Unlock()
	w.registry.doc.MCPServers[name] = mcp.ManagedServer{Command: "uvx", Source: source}
}

func reviewAndInstall(t *testing.T, b *Backend) Report {
	t.Helper()
	r, err := b.Review(context.Background(), "o/r/p")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	rep, err := b.Install(context.Background(), "admin", InstallRequest{Ref: "o/r/p", Commit: r.Commit, Digest: r.Digest})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	return rep
}

func stepOf(steps []Step, name string) Step {
	for _, s := range steps {
		if s.Name == name {
			return s
		}
	}
	return Step{}
}

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
	if s.Outcome != OutcomeAttention || s.Link != "/governance/mcp?server=remote" || !strings.Contains(s.Reason, "401") {
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
	reviewAndInstall(t, b)
	if during.Phase != PhaseInstalling || !slices.Equal(during.Connectors, []string{"local", "remote"}) {
		t.Errorf("record during verify = %+v", during)
	}
	rec, _, _ := b.Get("p")
	if rec.Phase != PhaseInstalled || !slices.Equal(rec.Connectors, []string{"remote"}) || len(rec.Skills) != 2 ||
		rec.Skills[0].Hash != "sha256:landed-s1" || rec.InstalledBy != "admin" || rec.Digest == "" {
		t.Errorf("final record = %+v", rec)
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
		if _, err := b.Install(context.Background(), "admin", req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if ok, err := b.MayAdminister(context.Background(), "admin"); !ok || err != nil {
		t.Errorf("MayAdminister(admin) = (%v, %v)", ok, err)
	}
}
