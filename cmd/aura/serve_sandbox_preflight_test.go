package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// serve_sandbox_preflight_test.go covers sandboxImagePreflight and logMultiUserAvailability
// (D-03/D-05): the boot-time counterpart to the lazy first-tool-call image pull. These are
// daemon-free unit tests over a fake usersandbox.Backend — no Docker daemon required.

// fakePreflightBackend implements usersandbox.Backend (the 5-verb core seam every backend
// must satisfy) plus EnsureImage, so it can be routed through a real *usersandbox.SandboxRouter
// exactly as DockerBackend is in production — the preflight calls chat.sandboxRouter.EnsureImage,
// never the backend directly.
type fakePreflightBackend struct {
	ensureImageErr   error
	ensureImageCalls int
}

func (f *fakePreflightBackend) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{}, nil
}
func (f *fakePreflightBackend) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{}, nil
}
func (f *fakePreflightBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (f *fakePreflightBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (f *fakePreflightBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func (f *fakePreflightBackend) EnsureImage(context.Context) error {
	f.ensureImageCalls++
	return f.ensureImageErr
}

// preflightChatEnv builds the minimal *chatEnv sandboxImagePreflight/logMultiUserAvailability
// need: a config and a real router over the fake backend above.
func preflightChatEnv(profile config.RuntimeProfile, isolation bool, be *fakePreflightBackend) *chatEnv {
	cfg := &config.Config{Profile: profile, MUSRIsolation: isolation}
	cfg.Sandbox.Image = "aura-sandbox:latest"
	return &chatEnv{
		cfg:           cfg,
		sandboxRouter: usersandbox.NewSandboxRouter(be, profile, cfg.Sandbox),
	}
}

// TestSandboxPreflightRefusesStrictWithoutImage proves a strict, isolation-on deployment
// whose image is neither present nor pullable refuses to boot, naming a command that
// actually works: the configured image ref, the repo build target, and a docker pull form.
func TestSandboxPreflightRefusesStrictWithoutImage(t *testing.T) {
	be := &fakePreflightBackend{ensureImageErr: errors.New("no such image")}
	chat := preflightChatEnv(config.ProfileSingleUserHardened, true, be)

	err := sandboxImagePreflight(context.Background(), chat)
	if err == nil {
		t.Fatal("sandboxImagePreflight err = nil, want a boot refusal")
	}
	for _, want := range []string{"aura-sandbox:latest", "sandbox-images", "docker pull"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
	if be.ensureImageCalls != 1 {
		t.Errorf("EnsureImage called %d times, want 1", be.ensureImageCalls)
	}
}

// TestSandboxPreflightPassesWhenImageResolvable proves the same strict + isolation-on
// combination boots clean when the image is present/pullable.
func TestSandboxPreflightPassesWhenImageResolvable(t *testing.T) {
	be := &fakePreflightBackend{}
	chat := preflightChatEnv(config.ProfileSingleUserHardened, true, be)

	if err := sandboxImagePreflight(context.Background(), chat); err != nil {
		t.Fatalf("sandboxImagePreflight err = %v, want nil", err)
	}
	if be.ensureImageCalls != 1 {
		t.Errorf("EnsureImage called %d times, want 1", be.ensureImageCalls)
	}
}

// TestSandboxPreflightSkipsNonStrict proves a non-strict profile never reaches the router at
// all, whether isolation is on (a combination gateMultiUserRequiresStrictProfile already
// refuses at config-validate time) or off.
func TestSandboxPreflightSkipsNonStrict(t *testing.T) {
	for _, isolation := range []bool{true, false} {
		be := &fakePreflightBackend{ensureImageErr: errors.New("must not be called")}
		chat := preflightChatEnv(config.ProfileDev, isolation, be)

		if err := sandboxImagePreflight(context.Background(), chat); err != nil {
			t.Errorf("isolation=%v: sandboxImagePreflight err = %v, want nil", isolation, err)
		}
		if be.ensureImageCalls != 0 {
			t.Errorf("isolation=%v: EnsureImage called %d times, want 0 (non-strict must never reach the router)", isolation, be.ensureImageCalls)
		}
	}
}

// TestSandboxPreflightSkipsIsolationOff proves a strict deployment with isolation off never
// reaches the router either — a hardened single-identity deployment must not be blocked by an
// image it will never need.
func TestSandboxPreflightSkipsIsolationOff(t *testing.T) {
	be := &fakePreflightBackend{ensureImageErr: errors.New("must not be called")}
	chat := preflightChatEnv(config.ProfileSingleUserHardened, false, be)

	if err := sandboxImagePreflight(context.Background(), chat); err != nil {
		t.Errorf("sandboxImagePreflight err = %v, want nil", err)
	}
	if be.ensureImageCalls != 0 {
		t.Errorf("EnsureImage called %d times, want 0 (isolation off must never reach the router)", be.ensureImageCalls)
	}
}

// captureLog installs a text slog handler over buf for the duration of the test and restores
// the previous default logger on cleanup.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestBootInfoLineOnlyWhenNonStrict proves the D-05 multi-identity availability line is
// emitted exactly once for a non-strict, isolation-off deployment, and not at all for a
// strict profile or when isolation is already on.
func TestBootInfoLineOnlyWhenNonStrict(t *testing.T) {
	const marker = "docs/runbooks/musr-rollout.md"

	t.Run("non-strict isolation off logs once", func(t *testing.T) {
		buf := captureLog(t)
		logMultiUserAvailability(&config.Config{Profile: config.ProfileDev, MUSRIsolation: false})
		if n := strings.Count(buf.String(), marker); n != 1 {
			t.Fatalf("log mentions %q %d times, want exactly 1; log:\n%s", marker, n, buf.String())
		}
	})

	t.Run("strict profile logs nothing", func(t *testing.T) {
		buf := captureLog(t)
		logMultiUserAvailability(&config.Config{Profile: config.ProfileSingleUserHardened, MUSRIsolation: false})
		if n := strings.Count(buf.String(), marker); n != 0 {
			t.Fatalf("log mentions %q %d times under a strict profile, want 0; log:\n%s", marker, n, buf.String())
		}
	})

	t.Run("isolation already on logs nothing", func(t *testing.T) {
		buf := captureLog(t)
		logMultiUserAvailability(&config.Config{Profile: config.ProfileDev, MUSRIsolation: true})
		if n := strings.Count(buf.String(), marker); n != 0 {
			t.Fatalf("log mentions %q %d times with isolation already on, want 0; log:\n%s", marker, n, buf.String())
		}
	})
}
