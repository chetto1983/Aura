package steer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestInboxPushDrainFIFO(t *testing.T) {
	t.Run("push then drain returns exactly one message", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		if err := inbox.Push("conv-1", "cockpit", "redirect to X"); err != nil {
			t.Fatalf("Push: %v", err)
		}
		got := inbox.Drain("conv-1")
		if len(got) != 1 {
			t.Fatalf("Drain len = %d, want 1", len(got))
		}
		if got[0].Text != "redirect to X" {
			t.Errorf("Text = %q, want %q", got[0].Text, "redirect to X")
		}
		if got[0].Source != "cockpit" {
			t.Errorf("Source = %q, want %q", got[0].Source, "cockpit")
		}
		if got[0].ID == "" {
			t.Error("ID must not be empty")
		}
		if again := inbox.Drain("conv-1"); len(again) != 0 {
			t.Errorf("second Drain len = %d, want 0", len(again))
		}
	})

	t.Run("three pushes drain in FIFO order", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		for _, text := range []string{"first", "second", "third"} {
			if err := inbox.Push("conv-2", "telegram", text); err != nil {
				t.Fatalf("Push(%q): %v", text, err)
			}
		}
		got := inbox.Drain("conv-2")
		if len(got) != 3 {
			t.Fatalf("Drain len = %d, want 3", len(got))
		}
		want := []string{"first", "second", "third"}
		for idx, w := range want {
			if got[idx].Text != w {
				t.Errorf("got[%d].Text = %q, want %q", idx, got[idx].Text, w)
			}
		}
		if again := inbox.Drain("conv-2"); len(again) != 0 {
			t.Errorf("Drain after full drain = %d, want 0", len(again))
		}
	})

	t.Run("drain on unknown conversation returns empty and leaves no residue", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		got := inbox.Drain("never-pushed")
		if len(got) != 0 {
			t.Errorf("Drain len = %d, want 0", len(got))
		}
		inbox.mu.Lock()
		_, exists := inbox.byConv["never-pushed"]
		inbox.mu.Unlock()
		if exists {
			t.Error("Drain on an unpushed conversation must not leave a map entry")
		}
	})

	t.Run("two conversations are independent", func(t *testing.T) {
		inbox := New(Config{Max: 2, MaxBytes: 64})
		if err := inbox.Push("conv-a", "cockpit", "a1"); err != nil {
			t.Fatalf("Push conv-a: %v", err)
		}
		if err := inbox.Push("conv-a", "cockpit", "a2"); err != nil {
			t.Fatalf("Push conv-a: %v", err)
		}
		if err := inbox.Push("conv-b", "cockpit", "b1"); err != nil {
			t.Fatalf("Push conv-b: %v", err)
		}
		if err := inbox.Push("conv-a", "cockpit", "a3"); err != ErrQueueFull {
			t.Errorf("Push conv-a (already at Max) = %v, want ErrQueueFull", err)
		}
		gotB := inbox.Drain("conv-b")
		if len(gotB) != 1 || gotB[0].Text != "b1" {
			t.Errorf("Drain conv-b = %+v, want one message %q", gotB, "b1")
		}
		gotA := inbox.Drain("conv-a")
		if len(gotA) != 2 {
			t.Errorf("Drain conv-a len = %d, want 2 (draining conv-b must not affect conv-a)", len(gotA))
		}
	})

	t.Run("close keeps queued messages but refuses further pushes", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		if err := inbox.Push("conv-c", "cockpit", "queued before close"); err != nil {
			t.Fatalf("Push: %v", err)
		}
		inbox.Close()
		if err := inbox.Push("conv-c", "cockpit", "after close"); err != ErrClosed {
			t.Errorf("Push after Close = %v, want ErrClosed", err)
		}
		got := inbox.Drain("conv-c")
		if len(got) != 1 || got[0].Text != "queued before close" {
			t.Errorf("Drain after Close = %+v, want the pre-close message preserved", got)
		}
	})
}

func TestInboxCapsAtMax(t *testing.T) {
	const max = 3

	t.Run("Max", func(t *testing.T) {
		inbox := New(Config{Max: max, MaxBytes: 64})
		for n := range max {
			if err := inbox.Push("conv", "cockpit", "msg"); err != nil {
				t.Fatalf("push %d/%d: %v", n+1, max, err)
			}
		}
		got := inbox.Drain("conv")
		if len(got) != max {
			t.Fatalf("queued = %d, want %d", len(got), max)
		}
	})

	t.Run("Max+1", func(t *testing.T) {
		inbox := New(Config{Max: max, MaxBytes: 64})
		for n := range max {
			if err := inbox.Push("conv", "cockpit", "msg"); err != nil {
				t.Fatalf("push %d/%d: %v", n+1, max, err)
			}
		}
		if err := inbox.Push("conv", "cockpit", "one too many"); err != ErrQueueFull {
			t.Fatalf("Push past Max = %v, want ErrQueueFull", err)
		}
		got := inbox.Drain("conv")
		if len(got) != max {
			t.Fatalf("queue after a refused push = %d, want it to still hold exactly %d", len(got), max)
		}
	})
}

func TestInboxRejectsEmptyAndOversize(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		if err := inbox.Push("conv", "cockpit", ""); err != ErrEmpty {
			t.Fatalf(`Push("") = %v, want ErrEmpty`, err)
		}
		if got := inbox.Drain("conv"); len(got) != 0 {
			t.Fatalf("queue after a refused empty push = %d, want 0", len(got))
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 64})
		if err := inbox.Push("conv", "cockpit", "   \t\n  "); err != ErrEmpty {
			t.Fatalf("Push(whitespace-only) = %v, want ErrEmpty", err)
		}
		if got := inbox.Drain("conv"); len(got) != 0 {
			t.Fatalf("queue after a refused whitespace-only push = %d, want 0", len(got))
		}
	})

	t.Run("MaxBytes", func(t *testing.T) {
		// MaxBytes byte-length fixture: "é" (U+00E9) is a 2-byte UTF-8 rune. A
		// rune-counting implementation would also accept this fixture; it is the
		// MaxBytes+1 case below — built from the SAME multi-byte fixture — that
		// only a byte-counting cap refuses.
		const maxBytes = 10
		body := strings.Repeat("é", maxBytes/2) // 5 runes * 2 bytes = 10 bytes exactly
		if got := len([]byte(body)); got != maxBytes {
			t.Fatalf("fixture byte length = %d, want %d", got, maxBytes)
		}
		inbox := New(Config{Max: 4, MaxBytes: maxBytes})
		if err := inbox.Push("conv", "cockpit", body); err != nil {
			t.Fatalf("Push(exactly MaxBytes) = %v, want nil", err)
		}
	})

	t.Run("MaxBytes+1", func(t *testing.T) {
		// MaxBytes+1 byte-length fixture: same multi-byte "é" body, one ASCII byte over.
		const maxBytes = 10
		body := strings.Repeat("é", maxBytes/2) + "x" // 11 bytes, still carries a multi-byte rune
		if got := len([]byte(body)); got != maxBytes+1 {
			t.Fatalf("fixture byte length = %d, want %d", got, maxBytes+1)
		}
		inbox := New(Config{Max: 4, MaxBytes: maxBytes})
		if err := inbox.Push("conv", "cockpit", body); err != ErrTooLarge {
			t.Fatalf("Push(MaxBytes+1) = %v, want ErrTooLarge", err)
		}
	})
}

func TestInboxConcurrentPushDrain(t *testing.T) {
	inbox := New(Config{Max: 1000, MaxBytes: 64})
	const conv = "conv-race"
	const n = 200

	pushed := make([]string, n)
	for i := range pushed {
		pushed[i] = fmt.Sprintf("msg-%03d", i)
	}

	var mu sync.Mutex
	var drained []Message
	stop := make(chan struct{})
	drainerDone := make(chan struct{})
	go func() {
		defer close(drainerDone)
		for {
			select {
			case <-stop:
				return
			default:
				if got := inbox.Drain(conv); len(got) > 0 {
					mu.Lock()
					drained = append(drained, got...)
					mu.Unlock()
				}
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(n)
	for _, text := range pushed {
		go func(text string) {
			defer wg.Done()
			if err := inbox.Push(conv, "cockpit", text); err != nil {
				t.Errorf("Push(%q): %v", text, err)
			}
		}(text)
	}
	wg.Wait()
	close(stop)
	<-drainerDone
	// Catch anything pushed after the drainer's last iteration but before stop
	// was observed — wg.Wait() already guarantees every Push returned, so
	// whatever is left here is exactly what the drainer's loop missed.
	drained = append(drained, inbox.Drain(conv)...)

	if len(drained) != n {
		t.Fatalf("drained %d messages, want %d (lost or duplicated)", len(drained), n)
	}
	seen := make(map[string]bool, n)
	for _, m := range drained {
		if seen[m.Text] {
			t.Fatalf("message %q drained more than once", m.Text)
		}
		seen[m.Text] = true
	}
	for _, text := range pushed {
		if !seen[text] {
			t.Errorf("message %q was pushed but never drained", text)
		}
	}
}

// TestNewResolvesNonPositiveCapsToPackageDefaults pins New's <=0 -> default
// fallback (52-08 mutation spot-check: every existing test in this file passes
// an explicit non-zero Config, leaving New's own boundary branch and the exact
// defaultMax/defaultMaxBytes values unexercised — go-mutesting's live run
// against this file surfaced 12 surviving mutants, all inside this branch).
// Config{} (the Go zero value) exercises the <=0 path for both fields at once.
func TestNewResolvesNonPositiveCapsToPackageDefaults(t *testing.T) {
	t.Run("zero Config resolves Max to exactly defaultMax", func(t *testing.T) {
		inbox := New(Config{})
		for i := range defaultMax {
			if err := inbox.Push("c", "cockpit", fmt.Sprintf("m%d", i)); err != nil {
				t.Fatalf("push %d/%d: %v", i+1, defaultMax, err)
			}
		}
		if err := inbox.Push("c", "cockpit", "one too many"); !errors.Is(err, ErrQueueFull) {
			t.Errorf("push past defaultMax(%d) = %v, want ErrQueueFull", defaultMax, err)
		}
	})

	// literalDefaultMax/literalDefaultMaxBytes are HARDCODED, not read from the
	// package constants: a symbolic reference to defaultMax/defaultMaxBytes gets
	// mutated together with the production code under go-mutesting, so a test
	// that only reads the symbol cannot distinguish 32 from 31/33 (the mutant
	// changes both the production value and the test's own expectation in
	// lock-step). Pinning the literal is what actually asserts "the default is
	// 32", not merely "New is internally self-consistent".
	const literalDefaultMax = 32
	const literalDefaultMaxBytes = 32768

	t.Run("zero Config resolves Max to the literal value 32", func(t *testing.T) {
		inbox := New(Config{})
		for i := range literalDefaultMax {
			if err := inbox.Push("c", "cockpit", fmt.Sprintf("m%d", i)); err != nil {
				t.Fatalf("push %d/%d: %v", i+1, literalDefaultMax, err)
			}
		}
		if err := inbox.Push("c", "cockpit", "one too many"); !errors.Is(err, ErrQueueFull) {
			t.Errorf("push past literal default %d = %v, want ErrQueueFull", literalDefaultMax, err)
		}
	})

	t.Run("zero Config resolves MaxBytes to exactly defaultMaxBytes", func(t *testing.T) {
		atCap := New(Config{})
		if err := atCap.Push("c", "cockpit", strings.Repeat("a", defaultMaxBytes)); err != nil {
			t.Errorf("push of exactly defaultMaxBytes(%d) bytes = %v, want nil", defaultMaxBytes, err)
		}
		overCap := New(Config{})
		if err := overCap.Push("c", "cockpit", strings.Repeat("a", defaultMaxBytes+1)); !errors.Is(err, ErrTooLarge) {
			t.Errorf("push of defaultMaxBytes+1(%d) bytes = %v, want ErrTooLarge", defaultMaxBytes+1, err)
		}
	})

	t.Run("zero Config resolves MaxBytes to the literal value 32768", func(t *testing.T) {
		atCap := New(Config{})
		if err := atCap.Push("c", "cockpit", strings.Repeat("a", literalDefaultMaxBytes)); err != nil {
			t.Errorf("push of exactly %d literal bytes = %v, want nil", literalDefaultMaxBytes, err)
		}
		overCap := New(Config{})
		if err := overCap.Push("c", "cockpit", strings.Repeat("a", literalDefaultMaxBytes+1)); !errors.Is(err, ErrTooLarge) {
			t.Errorf("push of %d literal+1 bytes = %v, want ErrTooLarge", literalDefaultMaxBytes+1, err)
		}
	})

	t.Run("Max=1 is honoured verbatim, never silently widened to the default", func(t *testing.T) {
		inbox := New(Config{Max: 1, MaxBytes: 64})
		if err := inbox.Push("c", "cockpit", "first"); err != nil {
			t.Fatalf("first push with Max=1: %v", err)
		}
		if err := inbox.Push("c", "cockpit", "second"); !errors.Is(err, ErrQueueFull) {
			t.Errorf("second push with Max=1 = %v, want ErrQueueFull (Max must stay 1, not fall back to default)", err)
		}
	})

	t.Run("MaxBytes=1 is honoured verbatim, never silently widened to the default", func(t *testing.T) {
		inbox := New(Config{Max: 4, MaxBytes: 1})
		if err := inbox.Push("c", "cockpit", "a"); err != nil {
			t.Fatalf("1-byte push with MaxBytes=1: %v", err)
		}
		if err := inbox.Push("c", "cockpit", "ab"); !errors.Is(err, ErrTooLarge) {
			t.Errorf("2-byte push with MaxBytes=1 = %v, want ErrTooLarge (MaxBytes must stay 1, not fall back to default)", err)
		}
	})

	t.Run("negative caps also resolve to the package defaults, not merely <=0", func(t *testing.T) {
		inbox := New(Config{Max: -5, MaxBytes: -5})
		for i := range defaultMax {
			if err := inbox.Push("c", "cockpit", fmt.Sprintf("m%d", i)); err != nil {
				t.Fatalf("push %d/%d with negative Max: %v", i+1, defaultMax, err)
			}
		}
		if err := inbox.Push("c", "cockpit", "one too many"); !errors.Is(err, ErrQueueFull) {
			t.Errorf("push past defaultMax with negative Max input = %v, want ErrQueueFull", err)
		}
	})
}
