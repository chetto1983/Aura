package mcptools

import (
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"alnum kept", "create_issue", "create_issue"},
		{"hyphen kept", "list-files", "list-files"}, // Kills the '-'→'_' mutant (D-07 keeps '-', unlike Codex).
		{"slash replaced", "a/b", "a_b"},
		{"dot replaced", "a.b", "a_b"},
		{"space replaced", "a b", "a_b"},
		{"unicode replaced", "héllo", "h_llo"},
		{"mixed kept", "Tool_9-x", "Tool_9-x"},
		{"all invalid", "...", "___"},
		{"empty to underscore", "", "_"}, // Kills the empty→"" mutant.
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeName(tc.in); got != tc.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNamespacedName(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		tool      string
		want      string
	}{
		{"plain join", "github", "create_issue", "github__create_issue"}, // Kills the single-'_' delimiter mutant.
		{"sanitize both sides", "git/hub", "create.issue", "git_hub__create_issue"},
		{"hyphen survives join", "my-server", "do-thing", "my-server__do-thing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := namespacedName(tc.namespace, tc.tool); got != tc.want {
				t.Errorf("namespacedName(%q,%q) = %q, want %q", tc.namespace, tc.tool, got, tc.want)
			}
		})
	}
}

func TestNameLengthCap(t *testing.T) {
	// A joined sanitized name well over 64 bytes must be capped to <=64 and end in a
	// deterministic "_<12hex>" suffix; the same inputs yield the same capped name.
	ns := "server"
	tool := strings.Repeat("x", 100)
	got := namespacedName(ns, tool)
	if len(got) > maxToolNameLen {
		t.Fatalf("namespacedName length = %d, want <= %d", len(got), maxToolNameLen)
	}
	if !strings.HasPrefix(got, "server__") {
		t.Errorf("capped name should keep the namespace prefix, got %q", got) // Kills the truncate-the-namespace mutant.
	}
	suffix := got[len(got)-(hashLen+1):]
	if suffix[0] != '_' {
		t.Errorf("capped name should end in a '_'-prefixed hash, got suffix %q", suffix)
	}
	if !isHex(suffix[1:]) {
		t.Errorf("capped name suffix should be hex, got %q", suffix[1:])
	}
	if again := namespacedName(ns, tool); again != got {
		t.Errorf("namespacedName not deterministic: %q != %q", again, got)
	}

	// A name exactly at the 64-byte boundary is NOT hashed (no suffix added).
	exact := namespacedName("ns", strings.Repeat("a", maxToolNameLen-len("ns__")))
	if len(exact) != maxToolNameLen {
		t.Fatalf("boundary name length = %d, want exactly %d", len(exact), maxToolNameLen)
	}
	if strings.Contains(exact, "_"+strings.Repeat("0", 0)) && len(exact) > maxToolNameLen {
		t.Errorf("a name exactly 64 bytes must not be hashed/truncated: %q", exact)
	}
	// One byte over the boundary IS hashed (proves the > vs >= boundary).
	over := namespacedName("ns", strings.Repeat("a", maxToolNameLen-len("ns__")+1))
	if len(over) > maxToolNameLen {
		t.Fatalf("over-boundary name length = %d, want <= %d", len(over), maxToolNameLen)
	}
	if over[len(over)-(hashLen+1)] != '_' {
		t.Errorf("a 65-byte name must be hashed: %q", over) // Kills the >= vs > boundary mutant.
	}
}

// TestNamespacedName_LongNamespaceCap covers WR-01: a sanitized namespace whose
// prefix (ns + "__") alone exceeds budget (maxToolNameLen - 13) AND whose joined base
// overflows the 64-byte cap (55-byte ns + 30-byte tool yields 70 today) must still
// yield len <= maxToolNameLen, with the hash suffix surviving so distinct long inputs
// stay distinguishable.
func TestNamespacedName_LongNamespaceCap(t *testing.T) {
	ns := strings.Repeat("n", 55)
	tool := strings.Repeat("t", 30)
	got := namespacedName(ns, tool)
	if len(got) > maxToolNameLen {
		t.Fatalf("namespacedName length = %d, want <= %d (WR-01)", len(got), maxToolNameLen)
	}
	suffix := got[len(got)-(hashLen+1):]
	if suffix[0] != '_' {
		t.Errorf("long-namespace result should end in a '_'-prefixed hash, got suffix %q", suffix)
	}
	if !isHex(suffix[1:]) {
		t.Errorf("long-namespace suffix should be hex, got %q", suffix[1:])
	}
}

// TestNamespacedName_DistinctLongNamespacesNoRecollision proves the hash suffix is
// never truncated away: two different overflowing (namespace,tool) pairs must produce
// different capped names. Kills the "drop-the-suffix" mutant.
func TestNamespacedName_DistinctLongNamespacesNoRecollision(t *testing.T) {
	a := namespacedName(strings.Repeat("n", 55), "alpha")
	b := namespacedName(strings.Repeat("n", 55), "beta")
	if len(a) > maxToolNameLen || len(b) > maxToolNameLen {
		t.Fatalf("both results must be capped: len(a)=%d len(b)=%d", len(a), len(b))
	}
	if a == b {
		t.Fatalf("distinct overflowing inputs must stay distinct, both = %q", a)
	}
}

// TestNamespacedName_ShortNamesByteStable proves the no-overflow fast path is
// byte-identical — the sha1->sha256 hashSuffix swap must NOT touch short names, which
// never reach hashSuffix (manifest/cache invariant).
func TestNamespacedName_ShortNamesByteStable(t *testing.T) {
	cases := []struct {
		ns, tool, want string
	}{
		{"github", "create_issue", "github__create_issue"},
		{"calculator", "calculate", "calculator__calculate"},
		{"sb", "sandbox_exec", "sb__sandbox_exec"},
	}
	for _, c := range cases {
		if got := namespacedName(c.ns, c.tool); got != c.want {
			t.Errorf("namespacedName(%q,%q) = %q, want byte-identical %q", c.ns, c.tool, got, c.want)
		}
	}
}

// TestNamespacedName_AdversarialLenSweep asserts the len <= maxToolNameLen invariant
// holds for ALL inputs: long namespaces, long tools, multibyte runes (each becomes a
// single '_' so byte and rune budgets agree), and near-collision pairs.
func TestNamespacedName_AdversarialLenSweep(t *testing.T) {
	cases := []struct {
		name, ns, tool string
	}{
		{"long ns short tool", strings.Repeat("a", 80), "x"},
		{"short ns long tool", "srv", strings.Repeat("z", 200)},
		{"long ns long tool", strings.Repeat("a", 80), strings.Repeat("z", 80)},
		{"multibyte ns", strings.Repeat("é", 60), "tool"},
		{"multibyte tool", "srv", strings.Repeat("漢", 60)},
		{"ns exactly at budget edge", strings.Repeat("a", 51), "tool"},
		{"ns one under cap", strings.Repeat("a", 60), "tool"},
		{"empty ns and tool", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := namespacedName(c.ns, c.tool)
			if len(got) > maxToolNameLen {
				t.Fatalf("namespacedName(%q,%q) len = %d, want <= %d", c.ns, c.tool, len(got), maxToolNameLen)
			}
		})
	}
}

func TestHashSuffixDeterministic(t *testing.T) {
	a := hashSuffix("github\x00create_issue")
	b := hashSuffix("github\x00create_issue")
	if a != b {
		t.Fatalf("hashSuffix not deterministic: %q != %q", a, b)
	}
	if len(a) != hashLen+1 {
		t.Fatalf("hashSuffix length = %d, want %d", len(a), hashLen+1)
	}
	if a[0] != '_' {
		t.Errorf("hashSuffix must be '_'-prefixed, got %q", a)
	}
	if !isHex(a[1:]) {
		t.Errorf("hashSuffix body must be hex, got %q", a[1:])
	}
	if c := hashSuffix("github\x00other_tool"); c == a {
		t.Errorf("distinct identities must hash differently: %q == %q", c, a) // Kills the constant-hash mutant.
	}
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return len(s) > 0
}
