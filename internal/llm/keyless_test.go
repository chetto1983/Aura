package llm

import "testing"

func TestIsKeylessLocalBaseURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://openrouter.ai/api/v1":         false,
		"https://api.example.com/v1":           false,
		"http://localhost:8080/v1":             true,
		"http://127.0.0.1:9000":                true,
		"http://host.docker.internal:11434/v1": true,
		"http://aura-llm:8084/v1":              true,
		"http://192.168.1.20:11434/v1":         true,
		"http://nas.local:11434/v1":            true,
		"":                                     false,
		"://not-a-url":                         false,
	} {
		if got := IsKeylessLocalBaseURL(raw); got != want {
			t.Errorf("IsKeylessLocalBaseURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
