package agui

import (
	"errors"
	"strings"
	"testing"
)

// TestSanitizeErr is a focused unit test on the redaction helper across DB DSN schemes,
// non-DSN URL userinfo, bearer/api-key/token shapes (WR-03), and the nil error.
func TestSanitizeErr(t *testing.T) {
	cases := map[string]struct {
		in        error
		wantNoSub string
		want      string
	}{
		"nil":      {in: nil, want: ""},
		"postgres": {in: errors.New("dial postgresql://u:p@h/db failed"), wantNoSub: "p@h"},
		"redis":    {in: errors.New("redis://x:y@z:6379 timeout"), wantNoSub: "y@z"},
		"plain":    {in: errors.New("plain message"), want: "plain message"},
		"https userinfo": {in: errors.New("GET https://alice:s3cret@mcp.example.com/v1 failed"),
			wantNoSub: "s3cret"},
		"http userinfo": {in: errors.New("proxy http://admin:hunter2@127.0.0.1:8080 refused"),
			wantNoSub: "hunter2"},
		"bearer":  {in: errors.New("auth rejected: Bearer sk-abc123XYZ token expired"), wantNoSub: "sk-abc123XYZ"},
		"api_key": {in: errors.New("call failed url=https://h/v1?api_key=secretval123&x=1"), wantNoSub: "secretval123"},
		"token":   {in: errors.New("webhook token=tok-deadbeef rejected"), wantNoSub: "tok-deadbeef"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeErr(tc.in)
			if tc.want != "" || tc.in == nil {
				if got != tc.want {
					t.Fatalf("sanitizeErr = %q, want %q", got, tc.want)
				}
				return
			}
			if strings.Contains(got, tc.wantNoSub) {
				t.Errorf("sanitizeErr leaked %q in %q", tc.wantNoSub, got)
			}
		})
	}
}
