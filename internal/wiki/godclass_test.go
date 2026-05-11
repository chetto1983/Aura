package wiki

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const godClassLimit = 600

// godClassAllowlist holds pre-existing files that exceeded 600 LOC before
// Phase 2 introduced this guard. Each entry is a repo-root-relative path
// using forward slashes. NEW violations are never added here — the list is
// frozen at the Phase 2 close-out state. To remove an entry, factor the
// file below 600 lines and delete the entry.
var godClassAllowlist = map[string]bool{
	// memory_hygiene.go was 759 lines at Phase 2 close-out (Plan 08).
	// Tracked for future refactor; excluded here so the guard does not
	// permanently fail on pre-existing debt while still catching new ones.
	"internal/wiki/memory_hygiene.go": true,
}

// TestGodClass enforces CLAUDE.md "Behavioral Rules / GOD CLASS" — no .go
// file in internal/wiki/ may exceed 600 lines. Test files (*_test.go) are
// exempted because they often hold table-driven cases that legitimately
// grow large.
//
// Scope: internal/wiki/ only. Plan 03 (Phase 2) factored wiki/store.go and
// wiki/store_writes.go below the 600-line ceiling. This guard prevents
// regression in the wiki package. Pre-existing violations are tracked in
// godClassAllowlist above.
//
// Running this test from internal/wiki keeps it in the standard
// `go test ./internal/wiki/` flow so CI catches violations without a
// separate test target.
func TestGodClass(t *testing.T) {
	// Walk internal/wiki/ — the package where the Plan 03 factor happened.
	// The test file lives in internal/wiki/, so "." resolves to that directory.
	internalRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	// repoRoot is two levels up (internal/wiki -> internal -> repo root).
	repoRoot := filepath.Join(internalRoot, "..", "..")

	var violations []string
	err = filepath.WalkDir(internalRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		n, countErr := countLines(path)
		if countErr != nil {
			return countErr
		}
		if n > godClassLimit {
			rel, _ := filepath.Rel(repoRoot, path)
			// Normalise to forward-slash for cross-platform allowlist lookup.
			relFwd := strings.ReplaceAll(rel, string(filepath.Separator), "/")
			if godClassAllowlist[relFwd] {
				t.Logf("SKIP (allowlisted pre-existing violation): %s has %d lines", relFwd, n)
				return nil
			}
			violations = append(violations, relFwd+" has "+itoa(n)+" lines (limit "+itoa(godClassLimit)+")")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("god-class violations:\n  - %s", strings.Join(violations, "\n  - "))
	}
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for s.Scan() {
		n++
	}
	return n, s.Err()
}

func itoa(n int) string {
	// Avoid strconv import to keep the test file minimal.
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
