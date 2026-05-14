package api

import "strings"

func detectLocale(acceptLanguage string) string {
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.Split(part, ";")[0]))
		if tag == "" {
			continue
		}
		if tag == "it" || strings.HasPrefix(tag, "it-") {
			return "it"
		}
		if tag == "en" || strings.HasPrefix(tag, "en-") {
			return "en"
		}
	}
	return "en"
}

func setupText(locale string) map[string]string {
	if locale == "it" {
		return map[string]string{
			"title":             "Aura - configurazione iniziale",
			"brandSub":          "Second brain - setup",
			"heading":           "Benvenuto. Ti mettiamo in funzione.",
			"lede":              "Pochi valori e hai finito. Aura li salva in locale: token e chiavi non lasciano mai questa macchina.",
			"telegramTitle":     "1. Token del bot Telegram",
			"telegramHint":      "Lo ottieni in 30 secondi: apri Telegram, scrivi a @BotFather, invia /newbot e segui i passaggi. Ha un formato simile a 123456:ABC...",
			"modelTitle":        "2. Modello linguistico",
			"modelHint":         "Scegli un provider. Aura parla con API chat compatibili OpenAI; OpenRouter e una opzione.",
			"provider":          "Provider",
			"baseURL":           "URL base",
			"model":             "Modello",
			"apiKey":            "Chiave API",
			"apiKeyPlaceholder": "sk-...",
			"testConnection":    "Testa connessione",
			"optionalSummary":   "Opzionale: ricerca wiki (embedding) e OCR PDF",
			"optionalHint":      "Salta questa sezione se non sei sicuro. Puoi abilitarla piu tardi dalle Impostazioni della dashboard.",
			"embeddingKey":      "Chiave API embedding (il piano gratuito Mistral va bene per uso personale)",
			"ocrKey":            "Chiave Mistral OCR (ingestione PDF)",
			"save":              "Salva e avvia Aura",
			"footnote":          "Salva TELEGRAM_TOKEN in .env e il resto nella tabella settings di aura.db. Potrai modificare tutto piu tardi dalla dashboard.",
			"testing":           "Test in corso...",
			"connectedModels":   "Connesso. {count} modelli disponibili.",
			"connectedNoModels": "Connesso (lista modelli non esposta).",
			"saving":            "Salvataggio...",
			"saved":             "Salvato. Aura si sta avviando: apro la dashboard...",
			"failed":            "errore",
			"openaiDesc":        "Provider popolare a pagamento. Crea una chiave su platform.openai.com.",
			"openrouterDesc":    "Router compatibile OpenAI per molti modelli ospitati.",
			"mistralDesc":       "Provider europeo, spesso economico. Crea una chiave su console.mistral.ai.",
			"groqDesc":          "Inferenza veloce, con piano gratuito disponibile.",
			"deepseekDesc":      "Economico e compatibile OpenAI.",
			"togetherDesc":      "Hosting per modelli open.",
			"customDesc":        "Qualsiasi endpoint compatibile OpenAI.",
		}
	}
	return map[string]string{
		"title":             "Aura - first-run setup",
		"brandSub":          "Second brain - setup",
		"heading":           "Welcome. Let's get you running.",
		"lede":              "A few values and you're done. Aura saves them locally - your token and keys never leave this machine.",
		"telegramTitle":     "1. Telegram bot token",
		"telegramHint":      "Get one in 30 seconds: open Telegram, message @BotFather, send /newbot, follow the prompts. It looks like 123456:ABC...",
		"modelTitle":        "2. Language model",
		"modelHint":         "Pick a provider. Aura talks to OpenAI-compatible chat APIs; OpenRouter is one option.",
		"provider":          "Provider",
		"baseURL":           "Base URL",
		"model":             "Model",
		"apiKey":            "API key",
		"apiKeyPlaceholder": "sk-...",
		"testConnection":    "Test connection",
		"optionalSummary":   "Optional: wiki search (embeddings) and PDF OCR",
		"optionalHint":      "Skip these for now if you're not sure. You can enable them later from the dashboard's Settings page.",
		"embeddingKey":      "Embeddings API key (Mistral free tier works well)",
		"ocrKey":            "Mistral OCR key (PDF ingestion)",
		"save":              "Save and start Aura",
		"footnote":          "Saves TELEGRAM_TOKEN to .env and the rest to aura.db's settings table. You can edit anything later from the dashboard.",
		"testing":           "Testing...",
		"connectedModels":   "Connected. {count} models available.",
		"connectedNoModels": "Connected (model list not exposed).",
		"saving":            "Saving...",
		"saved":             "Saved. Aura is starting now - open the dashboard...",
		"failed":            "failed",
		"openaiDesc":        "Most popular, paid. Get a key at platform.openai.com.",
		"openrouterDesc":    "OpenAI-compatible router for many hosted models.",
		"mistralDesc":       "EU-based, cheaper. Get a key at console.mistral.ai.",
		"groqDesc":          "Fast inference, free tier available.",
		"deepseekDesc":      "Cheap, OpenAI-compatible.",
		"togetherDesc":      "Open-model hosting.",
		"customDesc":        "Any OpenAI-compatible endpoint.",
	}
}

func localizedPresets(locale string) []Preset {
	t := setupText(locale)
	out := make([]Preset, len(SetupLLMPresets))
	copy(out, SetupLLMPresets)
	for i := range out {
		if desc, ok := t[out[i].ID+"Desc"]; ok {
			out[i].Description = desc
		}
	}
	return out
}
