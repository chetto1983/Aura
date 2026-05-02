package settings

import (
	"context"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "settings.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreSetGetRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "FOO", "bar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(ctx, "FOO")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "bar" {
		t.Errorf("Get(FOO) = %q, want bar", got)
	}
}

func TestStoreGetMissingReturnsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Get(context.Background(), "DOES_NOT_EXIST"); err != ErrNotFound {
		t.Errorf("Get missing: err = %v, want ErrNotFound", err)
	}
}

func TestStoreSetUpsertsExistingKey(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "K", "v1"); err != nil {
		t.Fatalf("Set v1: %v", err)
	}
	if err := s.Set(ctx, "K", "v2"); err != nil {
		t.Fatalf("Set v2: %v", err)
	}
	got, _ := s.Get(ctx, "K")
	if got != "v2" {
		t.Errorf("Get(K) = %q, want v2", got)
	}
}

func TestStoreGetStringFallback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if got := s.GetString(ctx, "MISSING", "fallback"); got != "fallback" {
		t.Errorf("GetString missing = %q, want fallback", got)
	}

	_ = s.Set(ctx, "BLANK", "   ")
	if got := s.GetString(ctx, "BLANK", "fallback"); got != "fallback" {
		t.Errorf("GetString blank = %q, want fallback (whitespace-only treated as missing)", got)
	}

	_ = s.Set(ctx, "REAL", "value")
	if got := s.GetString(ctx, "REAL", "fallback"); got != "value" {
		t.Errorf("GetString = %q, want value", got)
	}
}

func TestStoreGetIntFallback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if got := s.GetInt(ctx, "MISSING", 42); got != 42 {
		t.Errorf("GetInt missing = %d, want 42", got)
	}

	_ = s.Set(ctx, "GARBAGE", "not-a-number")
	if got := s.GetInt(ctx, "GARBAGE", 42); got != 42 {
		t.Errorf("GetInt garbage = %d, want 42 (fail-soft)", got)
	}

	_ = s.Set(ctx, "REAL", "100")
	if got := s.GetInt(ctx, "REAL", 42); got != 100 {
		t.Errorf("GetInt = %d, want 100", got)
	}
}

func TestStoreGetFloatFallback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if got := s.GetFloat(ctx, "MISSING", 1.5); got != 1.5 {
		t.Errorf("GetFloat missing = %v, want 1.5", got)
	}

	_ = s.Set(ctx, "GARBAGE", "x")
	if got := s.GetFloat(ctx, "GARBAGE", 1.5); got != 1.5 {
		t.Errorf("GetFloat garbage = %v, want 1.5", got)
	}

	_ = s.Set(ctx, "REAL", "3.14")
	if got := s.GetFloat(ctx, "REAL", 1.5); got != 3.14 {
		t.Errorf("GetFloat = %v, want 3.14", got)
	}
}

func TestStoreGetBoolFallback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if got := s.GetBool(ctx, "MISSING", true); !got {
		t.Errorf("GetBool missing = false, want true (fallback)")
	}

	_ = s.Set(ctx, "GARBAGE", "maybe")
	if got := s.GetBool(ctx, "GARBAGE", true); !got {
		t.Errorf("GetBool garbage = false, want true (fail-soft)")
	}

	_ = s.Set(ctx, "REAL", "false")
	if got := s.GetBool(ctx, "REAL", true); got {
		t.Errorf("GetBool = true, want false")
	}
}

func TestStoreDeleteIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_ = s.Set(ctx, "K", "v")
	if err := s.Delete(ctx, "K"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "K"); err != ErrNotFound {
		t.Errorf("after delete, err = %v, want ErrNotFound", err)
	}
	// Second delete must not error.
	if err := s.Delete(ctx, "K"); err != nil {
		t.Errorf("second Delete: %v, want nil", err)
	}
}

func TestStoreAllReturnsEverything(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_ = s.Set(ctx, "A", "1")
	_ = s.Set(ctx, "B", "2")

	all, err := s.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 || all["A"] != "1" || all["B"] != "2" {
		t.Errorf("All() = %v, want {A:1, B:2}", all)
	}
}

func TestStoreOpenRequiresPath(t *testing.T) {
	if _, err := OpenStore(""); err == nil {
		t.Errorf("OpenStore('') = nil, want error")
	}
	if _, err := OpenStore("   "); err == nil {
		t.Errorf("OpenStore('   ') = nil, want error")
	}
}

func TestNilStoreBehavior(t *testing.T) {
	// Empty store / nil receiver methods shouldn't panic — Applier-level
	// callers can pass nil when the DB couldn't open.
	var s *Store
	if _, err := s.Get(context.Background(), "X"); err != ErrNotFound {
		t.Errorf("nil Get: err = %v, want ErrNotFound", err)
	}
	if got := s.GetString(context.Background(), "X", "fb"); got != "fb" {
		t.Errorf("nil GetString = %q, want fb", got)
	}
	if got := s.GetInt(context.Background(), "X", 7); got != 7 {
		t.Errorf("nil GetInt = %d, want 7", got)
	}
	if got := s.GetFloat(context.Background(), "X", 1.0); got != 1.0 {
		t.Errorf("nil GetFloat = %v, want 1.0", got)
	}
	if !s.GetBool(context.Background(), "X", true) {
		t.Errorf("nil GetBool = false, want true")
	}
	if err := s.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
	if err := s.Delete(context.Background(), "X"); err != nil {
		t.Errorf("nil Delete: %v", err)
	}
	all, err := s.All(context.Background())
	if err != nil || len(all) != 0 {
		t.Errorf("nil All: %v, %v", all, err)
	}
}
