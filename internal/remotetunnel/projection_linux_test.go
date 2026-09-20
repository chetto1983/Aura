package remotetunnel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

func TestProjectionSidecarOwnership(t *testing.T) {
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 {
		uid, gid = 65532, 65532
	}
	p := NewFileProjection(t.TempDir(), uid, gid)
	if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Generation: 1, Token: "synthetic-secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != uint32(uid) || stat.Gid != uint32(gid) || info.Mode().Perm() != 0o600 {
		t.Fatalf("wrong ownership or permissions: %d:%d %o", stat.Uid, stat.Gid, info.Mode().Perm())
	}
}

// os.Rename only promises atomic replacement on Unix; the sidecar runs on Linux.
func TestProjectionConcurrentAppliesNeverPublishPartialToken(t *testing.T) {
	p := NewFileProjection(t.TempDir(), -1, -1)
	values := make(map[string]bool)
	for i := range 10 {
		values[fmt.Sprintf("%d:%s", i, strings.Repeat(fmt.Sprint(i), 32*1024))] = true
	}
	initial := cloudflareapi.Secret(fmt.Sprintf("0:%s", strings.Repeat("0", 32*1024)))
	if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Token: initial}); err != nil {
		t.Fatal(err)
	}
	stop, started, readerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readerDone)
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(p.TokenPath())
			if err != nil || !values[string(b)] {
				t.Errorf("reader observed incomplete publication (bytes=%d, err=%v)", len(b), err)
				return
			}
		}
	}()
	<-started
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			token := cloudflareapi.Secret(fmt.Sprintf("%d:%s", i, strings.Repeat(fmt.Sprint(i), 32*1024)))
			if err := p.Apply(context.Background(), ProjectionState{Enabled: true, Generation: int64(i), Token: token}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(stop)
	<-readerDone
	b, err := os.ReadFile(p.TokenPath())
	if err != nil || !values[string(b)] {
		t.Fatalf("incomplete token: %v", err)
	}
}

func TestProjectionRecoveryDoesNotFollowSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	target := outside + "/keep"
	if err := os.WriteFile(target, []byte("outside-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := root + "/.projection-symlink"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	p := NewFileProjection(root, -1, -1)
	if err := p.Apply(context.Background(), ProjectionState{}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "outside-data" {
		t.Fatal("recovery followed symlink outside projection")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("recovery removed an unrelated link")
	}
}
