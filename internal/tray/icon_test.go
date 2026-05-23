//go:build windows

package tray

import "testing"

func TestChooseTrayIconPrefersPrimary(t *testing.T) {
	got := chooseTrayIcon([]byte("primary"), []byte("fallback"))
	if string(got) != "primary" {
		t.Fatalf("icon = %q, want primary", got)
	}
}

func TestChooseTrayIconFallsBackWhenPrimaryMissing(t *testing.T) {
	got := chooseTrayIcon(nil, []byte("fallback"))
	if string(got) != "fallback" {
		t.Fatalf("icon = %q, want fallback", got)
	}
}
