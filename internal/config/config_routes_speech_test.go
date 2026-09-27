package config

import "testing"

// Found by code reading on 2026-09-27: the cloud speech clients were built on the chat base
// URL, so with the chat on Ollama a transcription would go to the Ollama server carrying the
// OpenRouter key.
func TestSpeechCloudRouteIgnoresTheChatBase(t *testing.T) {
	for name, chatBase := range map[string]string{
		"ollama":     "http://host.docker.internal:11434/v1",
		"llama.cpp":  "http://aura-llm:8084/v1",
		"openrouter": "https://openrouter.ai/api/v1",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{}
			cfg.LLM.BaseURL = chatBase
			cfg.LLM.APIKey = "sk-or-v1-services"

			base, key := cfg.SpeechCloudRoute()
			if base != "https://openrouter.ai/api/v1" || key != "sk-or-v1-services" {
				t.Fatalf("speech route = (%q, %q), want OpenRouter with the services key whatever the chat base", base, key)
			}
		})
	}
}
