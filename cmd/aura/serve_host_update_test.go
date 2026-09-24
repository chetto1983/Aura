package main

import (
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

func TestHostUpdateWriterRunsOnlyWhereTheUpdaterSharesADirectory(t *testing.T) {
	pool := unreachablePool(t)
	t.Cleanup(pool.Close)
	ids := identity.New(pool)
	for name, tc := range map[string]struct {
		dir  string
		want bool
	}{
		"no directory configured": {"", false},
		"directory absent":        {filepath.Join(t.TempDir(), "absent"), false},
		"directory shared":        {t.TempDir(), true},
	} {
		t.Run(name, func(t *testing.T) {
			w := buildHostUpdateWriter(tc.dir, pool, ids)
			if (w != nil) != tc.want {
				t.Fatalf("writer built = %v, want %v", w != nil, tc.want)
			}
		})
	}
	if buildHostUpdateWriter(t.TempDir(), nil, ids) != nil {
		t.Fatal("a writer was built with no pool to measure activity from")
	}
}
