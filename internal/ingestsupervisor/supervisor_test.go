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
	supervisor := New(lister, resolver, launcher, Options{
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
}

func TestReconcileStopsRemovedIdentityAndRestartsExitedChild(t *testing.T) {
	identityID := "a696df2b-b7bc-4ee7-870b-15d2cced1839"
	lister := &fakeLister{items: []identity.Identity{{ID: identityID, Kind: "user"}}}
	resolver := &fakeResolver{credentials: map[string]objectstore.Credentials{
		identityID: {Bucket: "aura-user", AccessKey: "key", SecretKey: "secret"},
	}}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})

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
	}
	env := spec.Environment([]string{
		"PATH=/usr/local/bin:/usr/bin",
		"AURA_INGEST_IDENTITY_ID=foreign",
		"AURA_INGEST_S3_SECRET_ACCESS_KEY=foreign-secret",
	})

	for key, want := range map[string]string{
		"AURA_INGEST_IDENTITY_ID":          spec.IdentityID,
		"AURA_INGEST_S3_BUCKET":            spec.Bucket,
		"AURA_INGEST_S3_ACCESS_KEY_ID":     spec.AccessKey,
		"AURA_INGEST_S3_SECRET_ACCESS_KEY": spec.SecretKey,
		"AURA_INGEST_S3_ENDPOINT":          spec.S3Endpoint,
		"AURA_INGEST_S3_REGION":            spec.S3Region,
		"COCOINDEX_DB":                     spec.StateDB,
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
}

func TestSupervisorRunRejectsMissingDependencies(t *testing.T) {
	supervisor := New(nil, nil, nil, Options{})

	err := supervisor.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "requires identity, credential, and process dependencies") {
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
