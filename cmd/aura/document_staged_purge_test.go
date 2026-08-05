package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// A purger with no router is the CLI and pool-free composition, where there is no
// box to reach into. It must be a silent no-op, not a nil dereference in the middle
// of an operator's delete.
func TestStagedPurgerWithoutARouterIsANoOp(t *testing.T) {
	t.Parallel()
	if err := (runtimeStagedDocumentPurger{}).PurgeStagedDocument(context.Background(), "doc-1"); err != nil {
		t.Fatalf("purge without a router: %v", err)
	}
}

// A name the staging rules reject was never written under that name, so there is
// nothing to remove. Returning an error here would make every delete of such a
// document log a scary line about readable bytes that do not exist.
func TestStagedPurgerWithoutRouterDoesNotValidate(t *testing.T) {
	t.Parallel()
	purger := runtimeStagedDocumentPurger{}
	if err := purger.PurgeStagedDocument(context.Background(), " "); err != nil {
		t.Fatalf("nil router remains a no-op: %v", err)
	}
}

// The delete and document_open must resolve the SAME path. They do it through one
// exported function precisely so a staging directory that moved could not leave the
// delete confidently removing a path nothing was ever written to — with the
// operator's document still sitting where the agent can read it.
func TestStagedDocumentPathIsTheOneDocumentOpenWrites(t *testing.T) {
	t.Parallel()
	got, err := tools.StagedDocumentPath("doc-1", "Clienti.xlsx")
	if err != nil {
		t.Fatalf("StagedDocumentPath: %v", err)
	}
	dir, err := tools.StagedDocumentDirectory("doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != dir+"/Clienti.xlsx" {
		t.Fatalf("staged path = %q, want deterministic document directory %q", got, dir)
	}
	// The rejections are the write path's own rules, reused rather than restated.
	for _, bad := range []string{"", "  ", "../escape.xlsx", "a/b.xlsx", ".dotfile"} {
		if _, err := tools.StagedDocumentPath("doc-1", bad); err == nil {
			t.Errorf("StagedDocumentPath(%q) was accepted; it would name something outside the documents directory", bad)
		}
	}
}

// The path is single-quoted for the shell it is handed to. It is belt to the
// validator's braces — the name is already a validated bare file name — but the
// remove runs as a shell command, and a quoting bug there deletes the wrong thing.
func TestShellQuoteSingleClosesAndReopens(t *testing.T) {
	t.Parallel()
	if got, want := shellQuoteSingle("/workspace/documents/a.xlsx"), "'/workspace/documents/a.xlsx'"; got != want {
		t.Errorf("shellQuoteSingle = %q, want %q", got, want)
	}
	// close-escape-reopen: the inner quote leaves the quoted string, is escaped, and
	// the string reopens. The total number of quotes is ODD by construction, so any
	// balance heuristic here would be wrong — the exact bytes are the assertion.
	if got, want := shellQuoteSingle("it's.xlsx"), `'it'\''s.xlsx'`; got != want {
		t.Errorf("shellQuoteSingle = %q, want %q", got, want)
	}
}
