package usersandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestWriteBoxFilesFailsClosedWithoutADaemon covers the branches that decide before any Docker
// call: no source wired is a no-op, a failing source fails Resolve, and a file aimed at the
// tmpfs scratch mount is refused instead of silently lost.
func TestWriteBoxFilesFailsClosedWithoutADaemon(t *testing.T) {
	t.Parallel()
	h := BoxHandle{ContainerID: "box-1", IdentityID: "id-1"}

	if err := NewDockerBackend(nil, "img", Resources{}).writeBoxFiles(context.Background(), h); err != nil {
		t.Fatalf("no source wired: err = %v, want nil", err)
	}

	boom := errors.New("secret unavailable")
	failing := NewDockerBackend(nil, "img", Resources{}, WithBoxFiles(func(id string) ([]BoxFile, error) {
		if id != "id-1" {
			t.Errorf("source got identity %q, want id-1", id)
		}
		return nil, boom
	}))
	if err := failing.writeBoxFiles(context.Background(), h); !errors.Is(err, boom) {
		t.Fatalf("failing source: err = %v, want it wrapped", err)
	}

	scratch := NewDockerBackend(nil, "img", Resources{}, WithBoxFiles(func(string) ([]BoxFile, error) {
		return []BoxFile{{Path: scratchTarget + "/key", Content: []byte("k"), Mode: 0o600}}, nil
	}))
	if err := scratch.writeBoxFiles(context.Background(), h); err == nil || !strings.Contains(err.Error(), "scratch mount") {
		t.Fatalf("scratch target: err = %v, want the scratch-mount refusal", err)
	}
}
