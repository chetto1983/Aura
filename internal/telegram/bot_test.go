package telegram

import "testing"

func TestToolActivityMessageDoesNotExposeArgs(t *testing.T) {
	got := toolActivityMessage("write_wiki")
	if got != "Running: write_wiki" {
		t.Fatalf("toolActivityMessage() = %q, want %q", got, "Running: write_wiki")
	}
}

func TestToolActivityMessageFallback(t *testing.T) {
	got := toolActivityMessage(" ")
	if got != "Running tool" {
		t.Fatalf("toolActivityMessage() = %q, want %q", got, "Running tool")
	}
}
