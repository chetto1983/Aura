package tools

import (
	"strings"
	"testing"
)

// AG-014/D-05: the size cap is enforced on the BYTES the box returns (the read is a bounded
// `head -c <cap+1>`), so an over-cap file is refused without ever materializing the whole thing.

func TestFSReadOverCapReturnsPagingHint(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "64")
	be := boxRespondingWith(strings.Repeat("a", 65)) // cap+1: the sentinel byte head -c allows
	ctx := ctxWith(t, "sess-cap", "call-cap")

	_, err := (&FSRead{Router: routerWith(be)}).Execute(ctx, mustJSON(t, fsReadArgs{Path: "/workspace/big.txt"}))
	if err == nil {
		t.Fatal("fs_read over cap err = nil, want clean over-cap error")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "offset") || !strings.Contains(low, "limit") {
		t.Fatalf("over-cap error must suggest offset/limit paging: %v", err)
	}
	// The cap must reach the box as the read bound, not just be checked afterwards.
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "head -c 65") {
		t.Fatalf("read must be bounded at cap+1 inside the box: %#v", be.execs)
	}
}

// Under-cap reads are unaffected.
func TestFSReadUnderCapUnaffected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "1048576")
	be := boxRespondingWith("hello\nworld\n")
	ctx := ctxWith(t, "sess-cap2", "call-cap2")
	res, err := (&FSRead{Router: routerWith(be)}).Execute(ctx, mustJSON(t, fsReadArgs{Path: "/workspace/small.txt"}))
	if err != nil {
		t.Fatalf("under-cap read: %v", err)
	}
	if !strings.Contains(res.Preview, "hello") {
		t.Fatalf("under-cap read content lost: %q", res.Preview)
	}
}

// AG-014: fs_write over the cap is rejected BEFORE the route, so an oversized write reads as the
// model's own error rather than a sandbox outage — and nothing is copied into the box.
func TestFSWriteOverCapRejected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "16")
	be := &fakeBox{}
	ctx := ctxWith(t, "sess-cap-w", "call-cap-w")
	_, err := (&FSWrite{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsWriteArgs{Path: "/workspace/out.txt", Content: strings.Repeat("x", 64)}))
	if err == nil {
		t.Fatal("fs_write over cap err = nil, want rejection")
	}
	if len(be.written) != 0 {
		t.Fatalf("fs_write over cap still copied into the box: %#v", be.written)
	}
}

// AG-014: fs_edit on a file over the cap is rejected before the edit is applied or written back.
func TestFSEditOverCapRejected(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "32")
	be := boxRespondingWith(strings.Repeat("abcd", 100))
	ctx := ctxWith(t, "sess-cap-e", "call-cap-e")
	_, err := (&FSEdit{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsEditArgs{Path: "/workspace/big.txt", OldString: "abcd", NewString: "wxyz"}))
	if err == nil {
		t.Fatal("fs_edit over cap err = nil, want rejection")
	}
	if len(be.written) != 0 {
		t.Fatalf("an over-cap edit must not write back: %#v", be.written)
	}
}

func TestFSMaxReadBytesDefault(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "")
	if got := fsMaxReadBytes(); got != defaultFSMaxReadBytes {
		t.Fatalf("default fs cap = %d, want %d (10 MiB)", got, defaultFSMaxReadBytes)
	}
}
