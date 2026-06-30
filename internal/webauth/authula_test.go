package webauth

import (
	"net/url"
	"testing"
)

// TestEnsureAuthulaSearchPath pins the H1 DSN hardening: every Authula DSN must
// carry search_path=authula so the framework's bare tables land in the isolated
// schema, EXCEPT when the operator already pinned a search_path (then it is
// respected verbatim, idempotently). Empty/whitespace and malformed DSNs are
// rejected. The result is parsed and asserted BY VALUE (not substring) so a
// double-append (two search_path params) or a clobbered sibling param is caught.
//
// Source-conflict note: ROADMAP/REQUIREMENTS scope this gap to Phase 32 while audit
// §E routes it to Phase 34. Resolved IN Phase 32 — ensureAuthulaSearchPath is a pure
// existing string helper that needs no Authula-cutover infrastructure (32-RESEARCH
// HIGH confidence). This table previously lived as a weaker substring check in
// webauth_test.go; it is relocated here (co-located with authula.go) and strengthened.
func TestEnsureAuthulaSearchPath(t *testing.T) {
	tests := []struct {
		name           string
		in             string
		wantErr        bool
		wantSearchPath string            // exact search_path value expected in the result
		wantParams     map[string]string // sibling query params that must survive untouched
	}{
		{name: "empty dsn errors", in: "", wantErr: true},
		{name: "whitespace-only dsn errors", in: "   ", wantErr: true},
		{name: "malformed dsn (no scheme) errors", in: "://%zz", wantErr: true},
		{name: "malformed dsn (bad port) errors", in: "postgres://h:notaport/db", wantErr: true},
		{
			name:           "no query appends authula",
			in:             "postgres://u:p@h:5432/db",
			wantSearchPath: "authula",
		},
		{
			name:           "no search_path appends authula and keeps siblings",
			in:             "postgres://aura_app:pw@127.0.0.1:5432/aura?sslmode=disable",
			wantSearchPath: "authula",
			wantParams:     map[string]string{"sslmode": "disable"},
		},
		{
			name:           "operator search_path respected (idempotent)",
			in:             "postgres://u:p@h:5432/db?search_path=custom",
			wantSearchPath: "custom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ensureAuthulaSearchPath(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ensureAuthulaSearchPath(%q) = %q, nil; want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ensureAuthulaSearchPath(%q) unexpected error: %v", tt.in, err)
			}
			u, perr := url.Parse(got)
			if perr != nil {
				t.Fatalf("result %q is not a valid URL: %v", got, perr)
			}
			q := u.Query()
			if sp := q["search_path"]; len(sp) != 1 || sp[0] != tt.wantSearchPath {
				t.Fatalf("search_path = %v, want exactly [%q] (result %q)", sp, tt.wantSearchPath, got)
			}
			for k, want := range tt.wantParams {
				if g := q.Get(k); g != want {
					t.Fatalf("param %q = %q, want %q (result %q)", k, g, want, got)
				}
			}
		})
	}
}
