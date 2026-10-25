package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		library:  &fakeLibrary{active: map[string]string{}, failInstall: map[string]bool{}, failDelete: map[string]bool{}},
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
		Taken: func() (Taken, error) {
			return Taken{Skill: w.library.Exists, Server: w.registry.source}, nil
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
	deleted     []string
	failInstall map[string]bool
	failDelete  map[string]bool
	onInstall   func()
}

func (l *fakeLibrary) Exists(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.active[name]
	return ok
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
	hash, err := skills.LandingHash(dir)
	if err != nil {
		return "", err
	}
	l.installed = append(l.installed, dir)
	l.sources = append(l.sources, source)
	l.active[name] = hash
	return hash, nil
}

func (l *fakeLibrary) ActiveHash(name string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.active[name]
	return h, ok
}

func (l *fakeLibrary) Delete(_ context.Context, name string, _ skills.AuditActor) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failDelete[name] {
		return errors.New("delete failed")
	}
	delete(l.active, name)
	l.deleted = append(l.deleted, name)
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
