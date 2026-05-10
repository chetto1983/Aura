package telegram

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestUserRequestedRawOutput(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "explicit italian raw", text: "Esegui pwd e mostrami l'output grezzo", want: true},
		{name: "explicit command", text: "esegui il comando pwd", want: true},
		{name: "backtick command", text: "cosa stampa `pwd`?", want: true},
		{name: "status natural", text: "Sei operativo?", want: false},
		{name: "network capability", text: "Puoi scansionare cartelle di rete?", want: false},
		{name: "summarized diagnostic", text: "Fai diagnostica del container e riassumi", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userRequestedRawOutput(tt.text); got != tt.want {
				t.Fatalf("userRequestedRawOutput(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestLatestUserText(t *testing.T) {
	got := latestUserText([]llm.Message{
		{Role: "user", Content: "prima"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "ultima"},
	})
	if got != "ultima" {
		t.Fatalf("latestUserText() = %q, want ultima", got)
	}
}

func TestRuntimeToolAllowedForUserIntent(t *testing.T) {
	tests := []struct {
		name string
		tool string
		text string
		want bool
	}{
		{name: "non runtime tool", tool: "search_memory", text: "Puoi scansionare?", want: true},
		{name: "broad network capability blocks code", tool: "execute_code", text: "Puoi anche scansionare le cartelle di rete?", want: false},
		{name: "status blocks shell", tool: "execute_shell", text: "Sei operativo?", want: false},
		{name: "explicit diagnostic allows code", tool: "execute_code", text: "Fai diagnostica del container e riassumi", want: true},
		{name: "explicit raw command allows shell", tool: "execute_shell", text: "esegui il comando pwd e mostrami l'output grezzo", want: true},
		{name: "calculation allows code", tool: "execute_code", text: "calcola sum(range(101))", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeToolAllowedForUserIntent(tt.tool, tt.text); got != tt.want {
				t.Fatalf("runtimeToolAllowedForUserIntent(%q, %q) = %v, want %v", tt.tool, tt.text, got, tt.want)
			}
		})
	}
}
