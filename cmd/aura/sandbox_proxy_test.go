package main

import (
	"bytes"
	"testing"
)

// TestSandboxProxy_ParseArgs covers the `aura sandbox proxy` flag contract: --listen
// is required (the gateway host:port the egress bridge routes to); --allow is
// optional (empty ⇒ egressless baseline-deny). A missing/blank --listen, an empty
// --listen=, or any unknown flag is a usage error (exit 64) with a diagnostic —
// never a silent bind to a wildcard address (the CI egress step depends on this
// command existing and refusing a bad listen addr loudly; #08-11 Gate-3).
func TestSandboxProxy_ParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		listen  string
		allow   string
		deny    string
	}{
		{"listen-and-allow", []string{"--listen", "172.20.0.1:18999", "--allow", "pypi.org,files.pythonhosted.org"}, false, "172.20.0.1:18999", "pypi.org,files.pythonhosted.org", ""},
		{"equals-form", []string{"--listen=127.0.0.1:9", "--allow=example.com"}, false, "127.0.0.1:9", "example.com", ""},
		{"with-deny", []string{"--listen", "127.0.0.1:9", "--allow", "pypi.org", "--deny", "evil.test"}, false, "127.0.0.1:9", "pypi.org", "evil.test"},
		{"listen-only-empty-allow-ok", []string{"--listen", "127.0.0.1:9"}, false, "127.0.0.1:9", "", ""},
		{"missing-listen", []string{"--allow", "pypi.org"}, true, "", "", ""},
		{"empty-listen", []string{"--listen="}, true, "", "", ""},
		{"blank-listen", []string{"--listen", "   "}, true, "", "", ""},
		{"unknown-flag", []string{"--listen", "x:1", "--bogus"}, true, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			pa, code := parseSandboxProxyArgs(tc.args, &errOut)
			if tc.wantErr {
				if code != exitUsage {
					t.Fatalf("args %v: want exit %d, got %d", tc.args, exitUsage, code)
				}
				if errOut.Len() == 0 {
					t.Fatalf("args %v: want a diagnostic on stderr", tc.args)
				}
				return
			}
			if code != 0 {
				t.Fatalf("args %v: want exit 0, got %d (stderr=%q)", tc.args, code, errOut.String())
			}
			if pa.listen != tc.listen || pa.allow != tc.allow || pa.deny != tc.deny {
				t.Fatalf("args %v: got %+v, want listen=%q allow=%q deny=%q", tc.args, pa, tc.listen, tc.allow, tc.deny)
			}
		})
	}
}
