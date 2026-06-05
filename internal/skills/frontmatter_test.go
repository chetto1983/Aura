package skills

import (
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	t.Parallel()

	// The spike-006 corpus edge case: a CRLF-terminated SKILL.md whose description
	// is a DOUBLE-QUOTED YAML scalar with escaped inner quotes. A naive key:value
	// splitter (picobot) mis-parses this — goccy is the mandated parser (D-30).
	crlfQuoted := "---\r\n" +
		"name: xlsx\r\n" +
		"description: \"Read, edit, and create \\\"Excel\\\" .xlsx workbooks: set cells and formulas.\"\r\n" +
		"license: Proprietary\r\n" +
		"compatibility: any\r\n" +
		"allowed-tools:\r\n" +
		"  - sandbox_exec\r\n" +
		"metadata:\r\n" +
		"  short-description: Excel toolkit\r\n" +
		"---\r\n" +
		"# Excel\r\n\r\nBody text.\r\n"

	tests := []struct {
		name       string
		raw        string
		wantName   string
		wantDesc   string
		wantType   string
		wantBody   string
		wantAlways bool
		wantErr    bool
	}{
		{
			name:     "crlf double-quoted escaped-quotes description with tolerated optional fields",
			raw:      crlfQuoted,
			wantName: "xlsx",
			wantDesc: `Read, edit, and create "Excel" .xlsx workbooks: set cells and formulas.`,
			wantType: TypeInstruction,
			wantBody: "# Excel\n\nBody text.\n",
		},
		{
			name:     "minimal instruction skill defaults type",
			raw:      "---\nname: greeter\ndescription: Greets the operator.\n---\nHello.\n",
			wantName: "greeter",
			wantDesc: "Greets the operator.",
			wantType: TypeInstruction,
			wantBody: "Hello.\n",
		},
		{
			name:       "always-true snippet skill with snippet docs fields",
			raw:        "---\nname: csv-stats\ndescription: CSV stats.\nalways: true\ntype: snippet\nlanguage: python\nneeds_workspace: true\ndeps:\n  - pandas\n---\nprint('hi')\n",
			wantName:   "csv-stats",
			wantDesc:   "CSV stats.",
			wantType:   TypeSnippet,
			wantBody:   "print('hi')\n",
			wantAlways: true,
		},
		{
			name:    "missing leading fence is an error",
			raw:     "name: x\ndescription: y\n",
			wantErr: true,
		},
		{
			name:    "missing closing fence is an error",
			raw:     "---\nname: x\ndescription: y\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fm, body, err := parseFrontmatter([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fm.Name != tc.wantName {
				t.Errorf("name = %q, want %q", fm.Name, tc.wantName)
			}
			if fm.Description != tc.wantDesc {
				t.Errorf("description = %q, want %q", fm.Description, tc.wantDesc)
			}
			if fm.Type != tc.wantType {
				t.Errorf("type = %q, want %q", fm.Type, tc.wantType)
			}
			if fm.Always != tc.wantAlways {
				t.Errorf("always = %v, want %v", fm.Always, tc.wantAlways)
			}
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

// TestParseFrontmatterNFKC asserts a fullwidth-codepoint name folds to its ASCII
// canonical form under NFKC before any downstream regex/dir-name check (D-27).
func TestParseFrontmatterNFKC(t *testing.T) {
	t.Parallel()
	// "ｓｋｉｌｌ" is fullwidth Latin; NFKC collapses it to ASCII "skill".
	raw := "---\nname: ｓｋｉｌｌ\ndescription: Fullwidth name test.\n---\nbody\n"
	fm, _, err := parseFrontmatter([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm.Name != "skill" {
		t.Fatalf("NFKC name = %q, want %q", fm.Name, "skill")
	}
	if strings.ContainsRune(fm.Name, 'ｓ') {
		t.Fatalf("name still contains a fullwidth codepoint: %q", fm.Name)
	}
}
