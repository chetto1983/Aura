package main

import (
	"encoding/hex"
	"testing"
)

func TestBrowserStateKeyFiles(t *testing.T) {
	if src := browserStateKeyFiles("  "); src != nil {
		t.Fatal("without AURA_AUTHULA_SECRET no key source may be wired: the box entry point must refuse, not run keyless")
	}

	src := browserStateKeyFiles("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	a, err := src("identity-a")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	again, _ := src("identity-a")
	b, _ := src("identity-b")
	if len(a) != 1 || a[0].Path != browserStateKeyPath || a[0].Mode != 0o600 {
		t.Fatalf("files = %+v, want exactly the 0600 key at %s", a, browserStateKeyPath)
	}
	if raw, err := hex.DecodeString(string(a[0].Content)); err != nil || len(raw) != 32 {
		t.Fatalf("content %q is not a 64-hex-char key (agent-browser's AGENT_BROWSER_ENCRYPTION_KEY format)", a[0].Content)
	}
	if string(again[0].Content) != string(a[0].Content) {
		t.Fatal("the same identity must rebuild the same key, or a recreated box cannot read its own state")
	}
	if string(b[0].Content) == string(a[0].Content) {
		t.Fatal("two identities must not share a browser state key")
	}

	if _, err := browserStateKeyFiles("not-hex-and-too-short")("identity-a"); err == nil {
		t.Fatal("an unusable secret must fail Resolve closed")
	}
}
