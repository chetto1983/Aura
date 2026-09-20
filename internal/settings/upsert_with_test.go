package settings

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestUpsertWithRejectsBeforeTransaction(t *testing.T) {
	s, _ := NewStore(nil, "")
	called := false
	callback := func(sqlc.DBTX) error { called = true; return nil }
	if _, err := s.UpsertWith(t.Context(), "POSTGRES_PASSWORD", "bad", "", callback); err == nil {
		t.Fatal("unlisted setting accepted")
	}
	if _, err := s.UpsertWith(t.Context(), "CLOUDFLARE_API_TOKEN", "secret", "", callback); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("callback ran before secret validation")
	}
}
