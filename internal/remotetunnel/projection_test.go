package remotetunnel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestProjectionIsPrivateAndRemovedWhenDisabled(t *testing.T) {
	p := NewFileProjection(t.TempDir(), -1, -1)
	if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Generation: 7, Token: "synthetic-secret"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.TokenPath())
	if err != nil || string(b) != "synthetic-secret" {
		t.Fatalf("token projection: %v", err)
	}
	info, err := os.Stat(p.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	b, err = os.ReadFile(filepath.Join(p.root, "desired.json"))
	if err != nil {
		t.Fatal(err)
	}
	var desired map[string]any
	if err := json.Unmarshal(b, &desired); err != nil {
		t.Fatal(err)
	}
	if len(desired) != 2 || desired["enabled"] != true || desired["generation"] != float64(7) {
		t.Fatalf("unexpected desired fields: %v", desired)
	}
	if err := p.Apply(context.Background(), ProjectionState{Generation: 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.TokenPath()); !os.IsNotExist(err) {
		t.Fatalf("disabled token exists: %v", err)
	}
}

func TestProjectionFailuresAreRedactedAndLeaveNoTemporaryFiles(t *testing.T) {
	for _, obstruction := range []string{"root", "token", "desired.json", "disable"} {
		t.Run(obstruction, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "projection")
			if obstruction == "root" {
				if err := os.WriteFile(root, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				name := obstruction
				if name == "disable" {
					name = "token"
				}
				if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name, "child"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := NewFileProjection(root, -1, -1)
			err := p.Apply(context.Background(), ProjectionState{Enabled: obstruction != "disable", Generation: 2, Token: "synthetic-secret"})
			if err == nil {
				t.Fatal("invalid projection succeeded")
			}
			if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("projection error leaked private detail")
			}
			files, _ := filepath.Glob(filepath.Join(root, ".projection-*"))
			if len(files) != 0 {
				t.Fatal("temporary token survived failed publication")
			}
		})
	}
	p := NewFileProjection(filepath.Join(t.TempDir(), "missing"), -1, -1)
	if err := p.write("token", []byte("synthetic-secret"), 0o600); err == nil {
		t.Fatal("missing root accepted")
	}
	if runtime.GOOS != "windows" && p.syncDir() == nil {
		t.Fatal("missing root synced")
	}
}

func TestProjectionConcurrentAppliesNeverPublishPartialToken(t *testing.T) {
	p := NewFileProjection(t.TempDir(), -1, -1)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Generation: int64(i), Token: "synthetic-secret"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	b, err := os.ReadFile(p.TokenPath())
	if err != nil || string(b) != "synthetic-secret" {
		t.Fatalf("incomplete token: %v", err)
	}
}

func TestProjectionRetryDoesNotInvalidateStartingCandidate(t *testing.T) {
	p := NewFileProjection(t.TempDir(), -1, -1)
	state := ProjectionState{Enabled: true, Generation: 1, Token: "synthetic-secret"}
	if err := p.Apply(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(p.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("identical retry replaced the token under a starting connector")
	}
}

func TestProjectionRejectsMissingTokenAndCancelledWrites(t *testing.T) {
	p := NewFileProjection(t.TempDir(), -1, -1)
	if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Generation: 1}); err == nil {
		t.Fatal("accepted empty token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Apply(ctx, ProjectionState{Generation: 1}); err == nil {
		t.Fatal("accepted cancelled write")
	}
}
