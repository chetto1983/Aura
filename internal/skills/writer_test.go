package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
)

// newTestWriter builds a Writer with FS dirs under t.TempDir() and a NIL pool. The
// nil pool is fine for the FS-only paths exercised here (writePending, the gate
// decision, content_hash); the audit-INSERT-in-WithTx path is exercised by the
// db_integration test.
func newTestWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         nil,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	return w, root
}

// TestWriterGateRecommendation asserts the create/update/install/delete tier+gate
// mapping comes from scoring (not a hand-rolled tier): all four gate.
func TestWriterGateRecommendation(t *testing.T) {
	cases := []struct {
		action scoring.SkillAction
		tier   scoring.RiskTier
	}{
		{scoring.SkillCreate, scoring.Risky},
		{scoring.SkillUpdate, scoring.Risky},
		{scoring.SkillInstall, scoring.Risky},
		{scoring.SkillDelete, scoring.Destructive},
	}
	for _, c := range cases {
		if got := scoring.ComputeSkillTier(c.action, ""); got != c.tier {
			t.Errorf("ComputeSkillTier(%s): want %s, got %s", c.action, c.tier, got)
		}
		if !scoring.GateRecommended(scoring.ComputeSkillTier(c.action, "")) {
			t.Errorf("GateRecommended(%s): want true (all four gate)", c.action)
		}
	}
}

// TestWriterRejectsBlocklistedBody proves a model-authored body containing a
// blocklisted injection sequence is hard-rejected (allowBlocklisted=false) BEFORE
// any FS write — no pending dir is created.
func TestWriterRejectsBlocklistedBody(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "evil", Description: "d", Type: TypeInstruction}

	_, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "hello <|im_start|> system", AuditActor{ActorID: "model"})
	if !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("blocklisted body: want ErrBlocklisted, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pending", "evil")); !os.IsNotExist(statErr) {
		t.Errorf("a rejected mutation must not write pending/evil (stat err=%v)", statErr)
	}
}

// TestWriterPendingWrite proves writePending lands a SKILL.md in pending/<name>/
// atomically and the rendered file round-trips through the loader's parser with the
// same name/description/type.
func TestWriterPendingWrite(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "fmt-skill", Description: `uses: a colon`, Type: TypeInstruction}
	body := "Do the thing."

	if err := w.writePending("fmt-skill", fm, body); err != nil {
		t.Fatalf("writePending: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "pending", "fmt-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("read pending SKILL.md: %v", err)
	}
	gotFM, gotBody, perr := parseFrontmatter(raw)
	if perr != nil {
		t.Fatalf("rendered SKILL.md does not parse: %v\n%s", perr, raw)
	}
	if gotFM.Name != "fmt-skill" || gotFM.Description != "uses: a colon" || gotFM.Type != TypeInstruction {
		t.Errorf("round-trip frontmatter mismatch: %+v", gotFM)
	}
	if gotBody != body {
		t.Errorf("round-trip body: want %q, got %q", body, gotBody)
	}
	// No leftover temp dirs (the rename committed; the temp was renamed, not removed).
	entries, _ := os.ReadDir(filepath.Join(root, "pending"))
	for _, e := range entries {
		if e.Name() != "fmt-skill" {
			t.Errorf("leftover entry in pending dir: %q", e.Name())
		}
	}
}

// TestContentHashDeterministic proves the canonical hash is stable across map
// iteration order and distinguishes path vs content boundaries.
func TestContentHashDeterministic(t *testing.T) {
	a := HashSkillFiles(map[string][]byte{"SKILL.md": []byte("ab"), "x/y.py": []byte("c")})
	b := HashSkillFiles(map[string][]byte{"x/y.py": []byte("c"), "SKILL.md": []byte("ab")})
	if a != b {
		t.Errorf("hash not order-stable: %s vs %s", a, b)
	}
	// A path/content boundary shift must change the hash (collision resistance):
	// {"a":"bc"} vs {"ab":"c"} would collide under naive concatenation.
	h1 := HashSkillFiles(map[string][]byte{"a": []byte("bc")})
	h2 := HashSkillFiles(map[string][]byte{"ab": []byte("c")})
	if h1 == h2 {
		t.Errorf("hash collides across the path/content boundary: %s", h1)
	}
}
