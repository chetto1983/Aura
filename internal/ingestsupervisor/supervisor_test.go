package ingestsupervisor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

type fakeLister struct {
	items []identity.Identity
}

func TestReconcileDoesNotRepeatUnprovisionedWarningEveryPoll(t *testing.T) {
	identityID := "5ebd218c-d85e-4593-bf05-925b776d43bf"
	var logs bytes.Buffer
	supervisor := New(
		&fakeLister{items: []identity.Identity{{ID: identityID, Kind: "user"}}},
		&fakeResolver{credentials: map[string]objectstore.Credentials{}},
		&fakeRoutes{route: testRoute},
		&fakeLauncher{},
		Options{PollInterval: time.Second, Logger: slog.New(slog.NewTextHandler(&logs, nil))},
	)
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if got := strings.Count(logs.String(), "no usable Garage binding"); got != 1 {
		t.Fatalf("warning count = %d, want one state-change warning; logs=%q", got, logs.String())
	}
}

func (f *fakeLister) ListIdentities(context.Context) ([]identity.Identity, error) {
	return append([]identity.Identity(nil), f.items...), nil
}

type fakeResolver struct {
	credentials map[string]objectstore.Credentials
}

func (f *fakeResolver) Resolve(ctx context.Context) (objectstore.Credentials, error) {
	credentials, ok := f.credentials[identityctx.IdentityID(ctx)]
	if !ok {
		return objectstore.Credentials{}, errors.New("not provisioned")
	}
	return credentials, nil
}

type fakeRoutes struct {
	route EmbedRoute
	err   error
}

func (f *fakeRoutes) Resolve(context.Context) (EmbedRoute, error) { return f.route, f.err }

var testRoute = EmbedRoute{
	BaseURL: "http://aura-llama-embed:8081", Space: "es1-0123456789abcdef", Dimensions: 768,
	TokenizerURL: "http://aura-llama-embed:8081",
}

type fakeProcess struct {
	done    chan error
	stopped bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan error, 1)}
}

func (p *fakeProcess) Done() <-chan error { return p.done }

func (p *fakeProcess) Stop(context.Context) error {
	p.stopped = true
	return nil
}

type fakeLauncher struct {
	specs     []ProcessSpec
	processes []*fakeProcess
}

func (f *fakeLauncher) Start(_ context.Context, spec ProcessSpec) (Process, error) {
	process := newFakeProcess()
	f.specs = append(f.specs, spec)
	f.processes = append(f.processes, process)
	return process, nil
}

func TestReconcileStartsOnlyProvisionedActiveUsersWithIsolatedState(t *testing.T) {
	activeID := "a696df2b-b7bc-4ee7-870b-15d2cced1839"
	lister := &fakeLister{items: []identity.Identity{
		{ID: activeID, Kind: "user"},
		{ID: "5ebd218c-d85e-4593-bf05-925b776d43bf", Kind: "user", Deactivated: true},
		{ID: identityctx.CLIServiceIdentity, Kind: "service"},
		{ID: "76db7481-0175-49f4-8c55-aab4d26e14ae", Kind: "user"},
	}}
	resolver := &fakeResolver{credentials: map[string]objectstore.Credentials{
		activeID: {Bucket: "aura-active", AccessKey: "active-key", SecretKey: "active-secret"},
	}}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, &fakeRoutes{route: testRoute}, launcher, Options{
		PollInterval: time.Second,
		StateRoot:    "/state",
		S3Endpoint:   "http://garage:3900",
		S3Region:     "garage",
	})

	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(launcher.specs) != 1 {
		t.Fatalf("started %d processes, want one provisioned active user", len(launcher.specs))
	}
	spec := launcher.specs[0]
	if spec.IdentityID != activeID || spec.Bucket != "aura-active" {
		t.Fatalf("spec identity/bucket = %q/%q", spec.IdentityID, spec.Bucket)
	}
	if got, want := spec.StateDB, filepath.Join("/state", activeID, "coco.db"); got != want {
		t.Fatalf("StateDB = %q, want %q", got, want)
	}
	if spec.AccessKey != "active-key" || spec.SecretKey != "active-secret" {
		t.Fatal("resolved credentials were not passed to the identity child")
	}
	if spec.S3Endpoint != "http://garage:3900" || spec.S3Region != "garage" {
		t.Fatalf("S3 route = %q/%q", spec.S3Endpoint, spec.S3Region)
	}
	if spec.Embed != testRoute {
		t.Fatalf("Embed = %+v, want the resolved route %+v", spec.Embed, testRoute)
	}
}

func TestReconcileStopsRemovedIdentityAndRestartsExitedChild(t *testing.T) {
	identityID := "a696df2b-b7bc-4ee7-870b-15d2cced1839"
	lister := &fakeLister{items: []identity.Identity{{ID: identityID, Kind: "user"}}}
	resolver := &fakeResolver{credentials: map[string]objectstore.Credentials{
		identityID: {Bucket: "aura-user", AccessKey: "key", SecretKey: "secret"},
	}}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, &fakeRoutes{route: testRoute}, launcher,
		Options{PollInterval: time.Second, StateRoot: "/state"})

	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	launcher.processes[0].done <- errors.New("crashed")
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("restart Reconcile: %v", err)
	}
	if len(launcher.processes) != 2 {
		t.Fatalf("process starts = %d, want restart after exit", len(launcher.processes))
	}

	lister.items = nil
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("removal Reconcile: %v", err)
	}
	if !launcher.processes[1].stopped {
		t.Fatal("active child was not stopped after its identity disappeared")
	}
}

func TestProcessSpecEnvironmentContainsOnlyItsIdentityBinding(t *testing.T) {
	spec := ProcessSpec{
		IdentityID: "a696df2b-b7bc-4ee7-870b-15d2cced1839",
		Bucket:     "aura-user",
		AccessKey:  "user-key",
		SecretKey:  "user-secret",
		S3Endpoint: "http://garage:3900",
		S3Region:   "garage",
		StateDB:    "/state/user/coco.db",
		Embed: EmbedRoute{
			BaseURL: "https://openrouter.ai/api", Model: "vendor/embed", APIKey: "sk-user",
			Space: "es1-0123456789abcdef", Dimensions: 768, InputLimit: 8192,
			TokenizerURL: "http://aura-llama-embed:8081",
		},
	}
	env := spec.Environment([]string{
		"PATH=/usr/local/bin:/usr/bin",
		"AURA_INGEST_IDENTITY_ID=foreign",
		"AURA_INGEST_S3_SECRET_ACCESS_KEY=foreign-secret",
		"AURA_EMBED_BASE_URL=http://compose-default:8081",
	})

	for key, want := range map[string]string{
		"AURA_INGEST_IDENTITY_ID":          spec.IdentityID,
		"AURA_INGEST_S3_BUCKET":            spec.Bucket,
		"AURA_INGEST_S3_ACCESS_KEY_ID":     spec.AccessKey,
		"AURA_INGEST_S3_SECRET_ACCESS_KEY": spec.SecretKey,
		"AURA_INGEST_S3_ENDPOINT":          spec.S3Endpoint,
		"AURA_INGEST_S3_REGION":            spec.S3Region,
		"COCOINDEX_DB":                     spec.StateDB,
		"AURA_EMBED_BASE_URL":              "https://openrouter.ai/api",
		"AURA_EMBED_MODEL":                 "vendor/embed",
		"AURA_EMBED_API_KEY":               "sk-user",
		"AURA_EMBED_SPACE":                 "es1-0123456789abcdef",
		"AURA_EMBED_DIMENSIONS":            "768",
		"AURA_EMBED_INPUT_LIMIT":           "8192",
		"AURA_EMBED_TOKENIZER_URL":         "http://aura-llama-embed:8081",
	} {
		if got := environmentValue(env, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := environmentCount(env, "AURA_INGEST_IDENTITY_ID"); got != 1 {
		t.Errorf("identity env count = %d, want one override", got)
	}
	if got := environmentCount(env, "AURA_INGEST_S3_SECRET_ACCESS_KEY"); got != 1 {
		t.Errorf("secret env count = %d, want one override", got)
	}
	if got := environmentCount(env, "AURA_EMBED_BASE_URL"); got != 1 {
		t.Errorf("embed base env count = %d, want the route to replace compose's fallback", got)
	}
}

func TestSupervisorRunRejectsMissingDependencies(t *testing.T) {
	supervisor := New(nil, nil, nil, nil, Options{})

	err := supervisor.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "requires identity, credential, embedding route, and process dependencies") {
		t.Fatalf("Run error = %v, want missing-dependency refusal", err)
	}
}

func TestSupervisorRunStopsChildrenOnCancellation(t *testing.T) {
	identityID := "a696df2b-b7bc-4ee7-870b-15d2cced1839"
	launcher := &fakeLauncher{}
	supervisor := New(
		&fakeLister{items: []identity.Identity{{ID: identityID, Kind: "user"}}},
		&fakeResolver{credentials: map[string]objectstore.Credentials{
			identityID: {Bucket: "aura-user", AccessKey: "key", SecretKey: "secret"},
		}},
		&fakeRoutes{route: testRoute},
		launcher,
		Options{PollInterval: time.Hour, StateRoot: t.TempDir()},
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := supervisor.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
	if len(launcher.processes) != 1 || !launcher.processes[0].stopped {
		t.Fatal("Run did not stop its active identity child during shutdown")
	}
	if len(supervisor.active) != 0 {
		t.Fatalf("active children after shutdown = %d, want zero", len(supervisor.active))
	}
}

func TestExecLauncherStartsWithPrivateIdentityEnvironment(t *testing.T) {
	stateDB := filepath.Join(t.TempDir(), "nested", "coco.db")
	launcher := &ExecLauncher{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestExecLauncherHelperProcess$"},
		Env:     []string{"AURA_INGEST_HELPER_MODE=exit"},
	}
	process, err := launcher.Start(t.Context(), ProcessSpec{
		IdentityID: "a696df2b-b7bc-4ee7-870b-15d2cced1839",
		Bucket:     "aura-user",
		AccessKey:  "user-key",
		SecretKey:  "user-secret",
		S3Endpoint: "http://garage:3900",
		S3Region:   "garage",
		StateDB:    stateDB,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case err := <-process.Done():
		if err != nil {
			t.Fatalf("helper process: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit")
	}
}

func TestExecLauncherStopCancelsRunningChild(t *testing.T) {
	launcher := &ExecLauncher{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestExecLauncherHelperProcess$"},
		Env:     []string{"AURA_INGEST_HELPER_MODE=wait"},
	}
	process, err := launcher.Start(t.Context(), ProcessSpec{StateDB: filepath.Join(t.TempDir(), "coco.db")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := process.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestNewExecLauncherUsesProductionDefaults(t *testing.T) {
	launcher := NewExecLauncher()
	if launcher.Command != "python" || strings.Join(launcher.Args, " ") != "-m ingest.app" {
		t.Fatalf("launcher command = %q %q", launcher.Command, launcher.Args)
	}
	if launcher.Stdout != os.Stdout || launcher.Stderr != os.Stderr || len(launcher.Env) == 0 {
		t.Fatal("launcher did not inherit the production process environment and streams")
	}
}

func TestExecLauncherHelperProcess(t *testing.T) {
	mode := os.Getenv("AURA_INGEST_HELPER_MODE")
	if mode == "" {
		return
	}
	for key, want := range map[string]string{
		"AURA_INGEST_IDENTITY_ID":          "a696df2b-b7bc-4ee7-870b-15d2cced1839",
		"AURA_INGEST_S3_BUCKET":            "aura-user",
		"AURA_INGEST_S3_ACCESS_KEY_ID":     "user-key",
		"AURA_INGEST_S3_SECRET_ACCESS_KEY": "user-secret",
		"AURA_INGEST_S3_ENDPOINT":          "http://garage:3900",
		"AURA_INGEST_S3_REGION":            "garage",
	} {
		if mode == "exit" && os.Getenv(key) != want {
			t.Fatalf("%s = %q, want %q", key, os.Getenv(key), want)
		}
	}
	if stateDB := os.Getenv("COCOINDEX_DB"); stateDB == "" {
		t.Fatal("COCOINDEX_DB is empty")
	} else if _, err := os.Stat(filepath.Dir(stateDB)); err != nil {
		t.Fatalf("identity state directory: %v", err)
	}
	if mode == "wait" {
		time.Sleep(time.Hour)
	}
}

func twoProvisionedUsers() (*fakeLister, *fakeResolver, []string) {
	ids := []string{"a696df2b-b7bc-4ee7-870b-15d2cced1839", "76db7481-0175-49f4-8c55-aab4d26e14ae"}
	return &fakeLister{items: []identity.Identity{{ID: ids[0], Kind: "user"}, {ID: ids[1], Kind: "user"}}},
		&fakeResolver{credentials: map[string]objectstore.Credentials{
			ids[0]: {Bucket: "aura-a", AccessKey: "key-a", SecretKey: "secret-a"},
			ids[1]: {Bucket: "aura-b", AccessKey: "key-b", SecretKey: "secret-b"},
		}}, ids
}

func TestReconcileRestartsEveryChildWhenTheRouteChanges(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	routes := &fakeRoutes{route: testRoute}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	routes.route.Space = "es1-fedcba9876543210"
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile after the route change: %v", err)
	}
	if !launcher.processes[0].stopped || !launcher.processes[1].stopped {
		t.Fatal("a child kept embedding with the old route after one poll")
	}
	if len(launcher.specs) != 4 {
		t.Fatalf("starts = %d, want both children restarted once", len(launcher.specs))
	}
	for _, spec := range launcher.specs[2:] {
		if spec.Embed.Space != "es1-fedcba9876543210" {
			t.Fatalf("restarted child space = %q, want the new one", spec.Embed.Space)
		}
	}
}

func TestReconcileKeepsChildrenOnTheirRouteWhileItCannotBeRead(t *testing.T) {
	lister, resolver, ids := twoProvisionedUsers()
	lister.items = lister.items[:1]
	routes := &fakeRoutes{route: testRoute}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	routes.route, routes.err = EmbedRoute{}, errors.New("attest local embedder: connection refused")
	lister.items = append(lister.items, identity.Identity{ID: ids[1], Kind: "user"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile while the route is unreadable: %v", err)
	}
	if launcher.processes[0].stopped {
		t.Fatal("a running child was stopped because the route could not be read")
	}
	if len(launcher.specs) != 2 || launcher.specs[1].Embed != testRoute {
		t.Fatalf("specs = %+v, want the new identity started on the last route that resolved", launcher.specs)
	}

	routes.route, routes.err = testRoute, nil
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile after the route came back: %v", err)
	}
	if len(launcher.specs) != 2 || launcher.processes[0].stopped || launcher.processes[1].stopped {
		t.Fatal("the same route coming back restarted children: a sidecar blip would be a restart storm")
	}

	lister.items = lister.items[1:]
	routes.route, routes.err = EmbedRoute{}, errors.New("settings database unreachable")
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile removing an identity: %v", err)
	}
	if !launcher.processes[0].stopped {
		t.Fatal("a removed identity kept its child because the route could not be read")
	}
}

func TestReconcileStartsNothingBeforeARouteResolves(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	routes := &fakeRoutes{err: embeddings.ErrNoCredential}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})

	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile without a route: %v", err)
	}
	if len(launcher.specs) != 0 {
		t.Fatalf("started %d children with no route: they would embed with nothing", len(launcher.specs))
	}
	routes.route, routes.err = testRoute, nil
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile once the route resolves: %v", err)
	}
	if len(launcher.specs) != 2 {
		t.Fatalf("starts = %d, want both children once the route resolves", len(launcher.specs))
	}
}

func TestReconcileLogsAnUnresolvedRouteOncePerChange(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	var logs bytes.Buffer
	supervisor := New(lister, resolver, &fakeRoutes{err: embeddings.ErrNoCredential}, &fakeLauncher{},
		Options{PollInterval: time.Second, Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	for range 3 {
		if err := supervisor.Reconcile(t.Context()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
	}
	if got := strings.Count(logs.String(), "embedding route unresolved"); got != 1 {
		t.Fatalf("warning count = %d, want one per change of failure; logs=%q", got, logs.String())
	}
}

func TestProcessSpecFingerprintMovesWithTheRouteAndOnlyWithIt(t *testing.T) {
	base := ProcessSpec{
		IdentityID: "a696df2b-b7bc-4ee7-870b-15d2cced1839", Bucket: "aura-user", AccessKey: "k",
		SecretKey: "s", S3Endpoint: "http://garage:3900", S3Region: "garage", StateDB: "/state/u/coco.db",
		Embed: EmbedRoute{
			BaseURL: "http://embed:8081", Model: "vendor/embed", APIKey: "sk", Space: "es1-aaaaaaaaaaaaaaaa",
			Dimensions: 768, InputLimit: 8192, TokenizerURL: "http://tokenizer:8081",
		},
	}
	same := base
	if same.fingerprint() != base.fingerprint() {
		t.Fatal("an identical spec moved its fingerprint: every tick would restart the child")
	}
	for name, change := range map[string]func(*EmbedRoute){
		"base":       func(r *EmbedRoute) { r.BaseURL = "http://other:8081" },
		"model":      func(r *EmbedRoute) { r.Model = "vendor/other" },
		"key":        func(r *EmbedRoute) { r.APIKey = "sk-rotated" },
		"space":      func(r *EmbedRoute) { r.Space = "es1-bbbbbbbbbbbbbbbb" },
		"dimensions": func(r *EmbedRoute) { r.Dimensions = 1024 },
		"limit":      func(r *EmbedRoute) { r.InputLimit = 512 },
		"tokenizer":  func(r *EmbedRoute) { r.TokenizerURL = "http://elsewhere:8081" },
	} {
		changed := base
		change(&changed.Embed)
		if changed.fingerprint() == base.fingerprint() {
			t.Errorf("a %s change kept the fingerprint: the child would never learn it", name)
		}
	}
}
