package skilladapters

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/skills"
)

// newWriterAdapter builds a Writer adapter over a real *skills.Writer rooted under
// a temp dir with a NIL pool. The nil pool is fine for every PRE-tx branch exercised
// here (validation, language enum, name grammar): those return before db.WithTx is
// reached. The audit-tx success paths (which require a live pool) are covered by the
// db_integration tier in skilladapters_integration_test.go.
func newWriterAdapter(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	live := skills.NewWriter(skills.WriterConfig{
		Pool:         nil,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	return NewWriter(live), root
}

func TestNewWriterRetainsLiveWriter(t *testing.T) {
	t.Parallel()
	live := skills.NewWriter(skills.WriterConfig{ActiveDir: t.TempDir()})
	a := NewWriter(live)
	if a.w != live {
		t.Error("NewWriter did not retain the live *skills.Writer")
	}
}

// TestWriteMutationRejectsBlocklistedBody drives WriteMutation through the model
// path: a blocklisted body is hard-rejected by ValidateForWrite BEFORE the audit tx,
// so the adapter surfaces ErrBlocklisted and never touches the nil pool. It also
// proves the modelActor "model" label routes through (the rejection happens at the
// validator, which runs identically for the model path).
func TestWriteMutationRejectsBlocklistedBody(t *testing.T) {
	t.Parallel()
	a, root := newWriterAdapter(t)

	status, err := a.WriteMutation(t.Context(), "create", "evil", "an evil skill", "hello <|im_start|> system", false)
	if !errors.Is(err, skills.ErrBlocklisted) {
		t.Fatalf("WriteMutation blocklisted body: want ErrBlocklisted, got status=%q err=%v", status, err)
	}
	if status != "" {
		t.Errorf("rejected mutation status = %q, want empty", status)
	}
	// A rejected mutation writes nothing to pending/.
	if _, statErr := os.Stat(filepath.Join(root, "pending", "evil")); !os.IsNotExist(statErr) {
		t.Errorf("blocklisted mutation must not write pending/evil (stat err=%v)", statErr)
	}
}

// TestWriteMutationRejectsInvalidName proves an invalid skill name (uppercase /
// traversal) is rejected at the name chokepoint before any FS/DB work.
func TestWriteMutationRejectsInvalidName(t *testing.T) {
	t.Parallel()
	a, _ := newWriterAdapter(t)

	cases := []struct{ name, label string }{
		{"../escape", "traversal"},
		{"UPPER", "uppercase"},
		{"", "empty"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			status, err := a.WriteMutation(t.Context(), "create", c.name, "desc", "a clean body", false)
			if !errors.Is(err, skills.ErrInvalidName) {
				t.Fatalf("WriteMutation(%q): want ErrInvalidName, got status=%q err=%v", c.name, status, err)
			}
			if status != "" {
				t.Errorf("rejected name status = %q, want empty", status)
			}
		})
	}
}

// TestSaveSnippetRejectsInvalidLanguage drives SaveSnippet through the language-enum
// guard: an unsupported language errors in validSnippetLanguage BEFORE any FS write or
// audit tx, so the adapter returns the structured error and an empty status.
func TestSaveSnippetRejectsInvalidLanguage(t *testing.T) {
	t.Parallel()
	a, root := newWriterAdapter(t)

	status, err := a.SaveSnippet(t.Context(), "weird", "ruby", "puts 1", "a weird snippet", false, false)
	if err == nil {
		t.Fatal("SaveSnippet with an unsupported language should error")
	}
	if !errors.Is(err, skills.ErrInvalidStructure) {
		t.Errorf("SaveSnippet bad language: want ErrInvalidStructure, got %v", err)
	}
	if status != "" {
		t.Errorf("rejected snippet status = %q, want empty", status)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pending", "weird")); !os.IsNotExist(statErr) {
		t.Errorf("invalid-language snippet must not write pending/weird (stat err=%v)", statErr)
	}
}

// TestSaveSnippetRejectsBlocklistedCode proves the injection blocklist runs on the
// CODE (the executable surface), hard-rejecting before the audit tx. This exercises
// the frontmatter-build + write-boundary-validate path of the adapter's SaveSnippet,
// while needsNetwork/needsWorkspace ride through onto the Frontmatter.
func TestSaveSnippetRejectsBlocklistedCode(t *testing.T) {
	t.Parallel()
	a, root := newWriterAdapter(t)

	status, err := a.SaveSnippet(t.Context(), "sneaky", "python", "x = '<|im_start|>'", "a sneaky snippet", true, true)
	if !errors.Is(err, skills.ErrBlocklisted) {
		t.Fatalf("SaveSnippet blocklisted code: want ErrBlocklisted, got status=%q err=%v", status, err)
	}
	if status != "" {
		t.Errorf("rejected snippet status = %q, want empty", status)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pending", "sneaky")); !os.IsNotExist(statErr) {
		t.Errorf("blocklisted snippet must not write pending/sneaky (stat err=%v)", statErr)
	}
}

// TestRestoreRejectsInvalidName proves Restore validates the name grammar (SanitizeName)
// BEFORE joining it into any path or reaching the audit tx — a traversal name is the
// load-bearing guard (the name reaches os.RemoveAll downstream).
func TestRestoreRejectsInvalidName(t *testing.T) {
	t.Parallel()
	a, _ := newWriterAdapter(t)

	status, err := a.Restore(t.Context(), "../escape")
	if !errors.Is(err, skills.ErrInvalidName) {
		t.Fatalf("Restore(../escape): want ErrInvalidName, got status=%q err=%v", status, err)
	}
	if status != "" {
		t.Errorf("rejected restore status = %q, want empty", status)
	}
}

// TestArchiveSnippetRejectsInvalidName proves ArchiveSnippet validates the name grammar
// (the model fully controls this name and it reaches os.RemoveAll) BEFORE any FS/DB work.
func TestArchiveSnippetRejectsInvalidName(t *testing.T) {
	t.Parallel()
	a, _ := newWriterAdapter(t)

	status, err := a.ArchiveSnippet(t.Context(), "bad/name")
	if !errors.Is(err, skills.ErrInvalidName) {
		t.Fatalf("ArchiveSnippet(bad/name): want ErrInvalidName, got status=%q err=%v", status, err)
	}
	if status != "" {
		t.Errorf("rejected archive status = %q, want empty", status)
	}
}
