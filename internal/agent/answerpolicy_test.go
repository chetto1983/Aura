package agent

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
			if got := UserRequestedRawOutput(tt.text); got != tt.want {
				t.Fatalf("UserRequestedRawOutput(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestLatestUserText(t *testing.T) {
	got := LatestUserText([]llm.Message{
		{Role: "user", Content: "prima"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "ultima"},
	})
	if got != "ultima" {
		t.Fatalf("LatestUserText() = %q, want ultima", got)
	}
}
