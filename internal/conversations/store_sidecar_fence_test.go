// Unit tier (no build tag): the LOOP-05 / F-005 / D-08 sidecar fence. It drives
// BOTH loadTurns (via LoadHistory) and loadBranchTurns (via LoadBranchHistory)
// through the in-memory sqlc.DBTX fake over a real tmpfs runDir, proving the
// reconstruct-don't-trust reader (readTurnSidecar) never touches the DB
// content_sidecar_path column value: a poisoned absolute path, a `..` traversal,
// and a symlink swapped at the <seq>.content leaf are all rejected, while a valid
// reconstructed sidecar rehydrates. The DB column is a did-spill flag ONLY.
package conversations

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
)

// fenceLoader names one of the two sidecar-reading loaders so every fence case runs
// identically against both (they share readTurnSidecar — DRY).
type fenceLoader struct {
	name string
	load func(s *Store, convID string) ([]llm.Message, error)
}

func fenceLoaders() []fenceLoader {
	return []fenceLoader{
		{"loadTurns", func(s *Store, convID string) ([]llm.Message, error) {
			return s.LoadHistory(context.Background(), convID)
		}},
		{"loadBranchTurns", func(s *Store, convID string) ([]llm.Message, error) {
			return s.LoadBranchHistory(context.Background(), convID, 2)
		}},
	}
}

// fenceStore builds a Store over a fresh fake scripting exactly one spilled turn at
// seq 2 whose content_sidecar_path column is columnPath (the untrusted value under
// test). The fake is fresh per call so the two loaders never share fakeRows state.
func fenceStore(t *testing.T, runDir, columnPath string) (*Store, string) {
	t.Helper()
	convID, conv := mustUUID(t)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 2, "assistant", "", columnPath, "", nil),
	}}}
	return &Store{q: sqlc.New(fake), runDir: runDir, turnCapBytes: 65536}, convID
}

// writeReconstructed writes body at the exact path readTurnSidecar reconstructs:
// runDir/conversations/<convID>/<seq>.content.
func writeReconstructed(t *testing.T, runDir, convID string, seq int, body string) {
	t.Helper()
	p := filepath.Join(runDir, "conversations", convID, strconv.Itoa(seq)+".content")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir reconstructed dir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write reconstructed sidecar: %v", err)
	}
}

// writeSecret writes a sensitive file OUTSIDE runDir that a path-trusting reader
// could be tricked into disclosing.
func writeSecret(t *testing.T) string {
	t.Helper()
	secret := filepath.Join(t.TempDir(), "SECRET.content")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	return secret
}

// TestReadTurnSidecar_ValidReconstructedRehydrates: a spilled turn whose file lives
// at the reconstructed path rehydrates for BOTH loaders.
func TestReadTurnSidecar_ValidReconstructedRehydrates(t *testing.T) {
	t.Parallel()
	for _, ld := range fenceLoaders() {
		ld := ld
		t.Run(ld.name, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			s, convID := fenceStore(t, runDir, "did-spill")
			writeReconstructed(t, runDir, convID, 2, "legit body")

			msgs, err := ld.load(s, convID)
			if err != nil {
				t.Fatalf("%s: %v", ld.name, err)
			}
			if len(msgs) != 1 || msgs[0].Content != "legit body" {
				t.Errorf("%s: reconstructed sidecar must rehydrate: %+v", ld.name, msgs)
			}
		})
	}
}

// TestReadTurnSidecar_PoisonedColumnIgnored: an outside-root absolute path and a
// `..` traversal in the DB column are both IGNORED — with no file at the
// reconstructed path the read is a HARD error, proving the reader never fell back
// to the poisoned column target (which is readable and would otherwise leak).
func TestReadTurnSidecar_PoisonedColumnIgnored(t *testing.T) {
	t.Parallel()
	for _, ld := range fenceLoaders() {
		ld := ld
		t.Run(ld.name, func(t *testing.T) {
			t.Parallel()
			secret := writeSecret(t)
			poisons := map[string]string{
				"absolute-outside-root": secret,
				"parent-traversal":      "../../../../../../etc/passwd",
				"dot-dot-into-secret":   filepath.Join("..", "..", filepath.Base(filepath.Dir(secret)), "SECRET.content"),
			}
			for name, col := range poisons {
				name, col := name, col
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					runDir := t.TempDir() // no reconstructed file exists under here
					s, convID := fenceStore(t, runDir, col)
					if _, err := ld.load(s, convID); err == nil {
						t.Fatalf("poisoned column %q must be ignored and the absent reconstructed path must error", col)
					}
				})
			}
		})
	}
}

// TestReadTurnSidecar_ReadsReconstructedNotColumnTarget: when BOTH a poisoned column
// target and a reconstructed file exist, the reader returns the reconstructed
// content and never the poisoned target — for both loaders.
func TestReadTurnSidecar_ReadsReconstructedNotColumnTarget(t *testing.T) {
	t.Parallel()
	for _, ld := range fenceLoaders() {
		ld := ld
		t.Run(ld.name, func(t *testing.T) {
			t.Parallel()
			secret := writeSecret(t)
			runDir := t.TempDir()
			s, convID := fenceStore(t, runDir, secret) // column points at the SECRET
			writeReconstructed(t, runDir, convID, 2, "legit body")

			msgs, err := ld.load(s, convID)
			if err != nil {
				t.Fatalf("%s: %v", ld.name, err)
			}
			if len(msgs) != 1 {
				t.Fatalf("%s: want 1 turn, got %d", ld.name, len(msgs))
			}
			if msgs[0].Content == "TOP SECRET" {
				t.Fatalf("%s: reader disclosed the poisoned column target", ld.name)
			}
			if msgs[0].Content != "legit body" {
				t.Errorf("%s: reader must return the reconstructed content: %+v", ld.name, msgs)
			}
		})
	}
}

// TestReadTurnSidecar_SymlinkLeafRefused: a symlink swapped at the <seq>.content leaf
// pointing OUTSIDE runDir is refused by os.Root (the value-add over plain os.Open) —
// the read errors and the external secret is never disclosed, for both loaders.
func TestReadTurnSidecar_SymlinkLeafRefused(t *testing.T) {
	t.Parallel()
	for _, ld := range fenceLoaders() {
		ld := ld
		t.Run(ld.name, func(t *testing.T) {
			t.Parallel()
			secret := writeSecret(t)
			runDir := t.TempDir()
			s, convID := fenceStore(t, runDir, "did-spill")

			leafDir := filepath.Join(runDir, "conversations", convID)
			if err := os.MkdirAll(leafDir, 0o750); err != nil {
				t.Fatalf("mkdir leaf dir: %v", err)
			}
			leaf := filepath.Join(leafDir, "2.content")
			if err := os.Symlink(secret, leaf); err != nil {
				t.Skipf("symlinks unavailable on this platform: %v", err)
			}

			msgs, err := ld.load(s, convID)
			if err == nil {
				t.Fatalf("%s: an escaping symlink at the .content leaf must be refused (got %+v)", ld.name, msgs)
			}
		})
	}
}

// TestReadTurnSidecar_GuardsRunDirAndConvID exercises the two precondition guards
// directly: a relative runDir (os.Root needs an absolute root) and a traversal-shaped
// conversation id are both rejected before any filesystem access.
func TestReadTurnSidecar_GuardsRunDirAndConvID(t *testing.T) {
	t.Parallel()
	relative := &Store{runDir: filepath.Join("relative", "run")}
	if _, err := relative.readTurnSidecar("11111111-1111-1111-1111-111111111111", 1); err == nil {
		t.Error("relative runDir must be rejected (os.Root needs an absolute root)")
	}
	absolute := &Store{runDir: t.TempDir()}
	if _, err := absolute.readTurnSidecar("../escape", 1); err == nil {
		t.Error("traversal conversation id must be rejected before the join")
	}
}
