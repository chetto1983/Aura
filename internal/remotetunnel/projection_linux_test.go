package remotetunnel

import (
	"context"
	"os"
	"syscall"
	"testing"
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
