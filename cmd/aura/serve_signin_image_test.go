package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type signInImageTestBackend struct {
	signInPreflightBackend
	present atomic.Bool
	pulls   atomic.Int32
	started chan context.Context
	release chan struct{}
	pullErr error
}

func newSignInImageTestBackend(df string) *signInImageTestBackend {
	b := &signInImageTestBackend{started: make(chan context.Context, 2), release: make(chan struct{})}
	b.df = df
	return b
}

func (b *signInImageTestBackend) SignInImageState(context.Context, string) (bool, error) {
	return b.present.Load(), nil
}
func (b *signInImageTestBackend) PullSignInImage(ctx context.Context, _ string, progress func(int64, int64)) error {
	b.pulls.Add(1)
	progress(50, 100)
	b.started <- ctx
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		if b.pullErr != nil {
			return b.pullErr
		}
		b.present.Store(true)
		return nil
	}
}

func TestClosingSignInRuntimeCancelsAnAcceptedDownload(t *testing.T) {
	root, stop := context.WithCancel(t.Context())
	b := newSignInImageTestBackend("Avail\n9000000000\n")
	r := &signInRuntime{root: root, stop: stop, browser: &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:fixture"}}
	ctx := identityctx.WithIdentityID(t.Context(), "alice")
	if _, err := r.PullImage(ctx); err != nil {
		t.Fatal(err)
	}
	pulling := <-b.started
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if pulling.Err() == nil {
		t.Fatal("download survived shutdown")
	}
	if _, err := r.PullImage(ctx); err == nil {
		t.Fatal("shutdown accepted another download")
	}
}

func TestSignInImageDownloadFailureCanBeRetried(t *testing.T) {
	root, stop := context.WithCancel(t.Context())
	b := newSignInImageTestBackend("Avail\n9000000000\n")
	b.pullErr = errors.New("synthetic registry failure")
	r := &signInRuntime{root: root, stop: stop, browser: &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:fixture"}}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := identityctx.WithIdentityID(t.Context(), "alice")
	if _, err := r.PullImage(ctx); err != nil {
		t.Fatal(err)
	}
	close(b.release)
	r.pullWorkers.Wait()
	state, err := r.ImageState(ctx)
	if err != nil || state.Present || state.Pulling || state.Error == "" {
		t.Fatalf("failed pull=%+v %v", state, err)
	}
	b.pullErr = nil
	if _, err = r.PullImage(ctx); err != nil {
		t.Fatal(err)
	}
	r.pullWorkers.Wait()
	state, err = r.ImageState(ctx)
	if err != nil || !state.Present || state.Error != "" || b.pulls.Load() != 2 {
		t.Fatalf("retried pull=%+v %v", state, err)
	}
}

func TestSignInImageLowDiskDoesNotDownload(t *testing.T) {
	root, stop := context.WithCancel(t.Context())
	defer stop()
	b := newSignInImageTestBackend("Avail\n100\n")
	r := &signInRuntime{root: root, stop: stop, browser: &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:fixture"}}
	state, err := r.PullImage(identityctx.WithIdentityID(t.Context(), "alice"))
	if err != nil || !strings.Contains(state.Error, "8 GiB") || b.pulls.Load() != 0 {
		t.Fatalf("low disk pull=%+v %v", state, err)
	}
}

func TestSignInImagePullIsExplicitSharedAndOutlivesItsRequest(t *testing.T) {
	root, stop := context.WithCancel(t.Context())
	defer stop()
	b := newSignInImageTestBackend("Avail\n9000000000\n")
	runtime := &signInRuntime{root: root, stop: stop, browser: &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(b, config.SandboxConfig{}), image: "desktop:fixture"}}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(identityctx.WithIdentityID(t.Context(), "alice"))
	defer cancel()
	image, err := runtime.ImageState(ctx)
	if err != nil || image.Present || image.Pulling || b.pulls.Load() != 0 {
		t.Fatalf("unexpected download: %+v %v", image, err)
	}
	if _, err = runtime.PullImage(context.Background()); err == nil {
		t.Fatal("anonymous request started download")
	}
	if _, err = runtime.PullImage(ctx); err != nil {
		t.Fatal(err)
	}
	pullContext := <-b.started
	cancel()
	if pullContext.Err() != nil {
		t.Fatal("request cancellation stopped the accepted download")
	}
	ctx = identityctx.WithIdentityID(t.Context(), "bob")
	image, err = runtime.PullImage(ctx)
	if err != nil || !image.Pulling || image.Done != 50 || image.Total != 100 || b.pulls.Load() != 1 {
		t.Fatalf("shared progress=%+v err=%v pulls=%d", image, err, b.pulls.Load())
	}
	close(b.release)
	runtime.pullWorkers.Wait()
	image, err = runtime.ImageState(ctx)
	if err != nil || !image.Present || image.Pulling || image.Error != "" {
		t.Fatalf("completed image=%+v err=%v", image, err)
	}
}
