package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type failingResolveBackend struct {
	fakePreflightBackend
	err error
}

func (f *failingResolveBackend) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{}, f.err
}

// A box image its registry lacks is the one box failure retrying cannot fix; any other stays a
// transport failure the mount retries.
func TestBoxServerStartFailureIsRetriedUnlessTheImageIsUnavailable(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"image not in its registry", fmt.Errorf("resolve: create box: pull image %q: %w", "aura-sandbox:latest", usersandbox.ErrImageUnavailable), false},
		{"docker unreachable", errors.New("resolve: create box: Cannot connect to the Docker daemon"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			box := newSandboxMCPBox(usersandbox.NewSandboxRouter(&failingResolveBackend{err: tc.err}, config.SandboxConfig{}))
			_, err := box.StartStdio(context.Background(), "deckcraft", []string{"deckcraft-cli", "mcp"}, nil,
				io.NopCloser(strings.NewReader("")), io.Discard)
			if err == nil {
				t.Fatal("StartStdio err = nil, want the resolve failure")
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want it to keep the resolve failure %v", err, tc.err)
			}
			if got := mcp.IsTransportError(mcp.TransportErrorf("deckcraft", err)); got != tc.retryable {
				t.Fatalf("retryable = %v, want %v for %v", got, tc.retryable, err)
			}
			if errors.Is(err, mcp.ErrNoBox) {
				t.Fatalf("err = %v reads as no sandbox, so the MCP board would report the server as not probed instead of failing", err)
			}
		})
	}
}

func TestBoxStdioCommandQuotesArgvAndKeepsStderrOffTheProtocol(t *testing.T) {
	got := boxStdioCommand("my server/../x", []string{"agent-browser", "mcp", "it's"})
	want := `'agent-browser' 'mcp' 'it'\''s' 2>>'/tmp/aura-mcp-my_server____x.log'`
	if got != want {
		t.Fatalf("boxStdioCommand = %s\nwant             %s", got, want)
	}
}

func TestNoSandboxMeansNoBoxLauncher(t *testing.T) {
	if newSandboxMCPBox(nil) != nil {
		t.Fatal("a host with no sandbox router got a box launcher, so box servers would fail late instead of being refused")
	}
}

func TestBoxDiscoveryWithoutADatabaseUsesTheRoutersOwnIdentity(t *testing.T) {
	if got := boxDiscoveryIdentity(context.Background(), nil); got != identityctx.LocalOperatorIdentity {
		t.Fatalf("boxDiscoveryIdentity(nil pool) = %q", got)
	}
}
