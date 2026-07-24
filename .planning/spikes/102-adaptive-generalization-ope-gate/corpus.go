package main

import (
	"fmt"
	"strings"
)

// This corpus is a true family holdout: none of these prompts, entities, catalog
// descriptions, or expected answers occurs in spike 097's training surface.
func reasoningScenarios() []scenario {
	rows := []struct{ id, family, prompt, expected string }{
		{"hr01", "stable_fact", "Return only the city: what is the capital of Canada?", "ottawa"},
		{"hr02", "direct_arithmetic", "Return only the integer: 144 divided by 12.", "12"},
		{"hr03", "rate_reasoning", "Four ovens bake four loaves in eight minutes. At the same rate, how many minutes do twenty ovens need for twenty loaves? Return only the integer.", "8"},
		{"hr04", "conditional_probability", "A fair coin is tossed three times. Return only the probability of exactly two heads as a reduced fraction.", "3/8"},
		{"hr05", "calendar_arithmetic", "A maintenance task starts at 21:50 and lasts 205 minutes. Return only the end time in 24-hour HH:MM format.", "01:15"},
		{"hr06", "algebra", "The sum of four consecutive integers is 74. Return only the largest integer.", "20"},
		{"hr07", "logic", "Every zorp is a mip. Some mips are tars. Does it follow that some zorps are tars? Return only yes or no.", "no"},
		{"hr08", "percentage", "A quantity falls by 20% and then rises by 25%. Return only the final percentage of the original quantity.", "100"},
		{"hr09", "combinatorics", "How many distinct arrangements are there of the letters in LEVEL? Return only the integer.", "30"},
		{"hr10", "number_theory", "Return only the smallest positive integer divisible by 12, 15, and 18.", "180"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{
			ID: row.id, Domain: "reasoning", Prompt: row.prompt, Expected: row.expected,
			Meta: map[string]string{"family": row.family},
		})
	}
	return out
}

func toolCatalog() []catalogItem {
	return []catalogItem{
		{"weather_now", "Get the current weather or forecast for a location."},
		{"weather_history", "Look up historical observed weather for a past date."},
		{"send_email", "Send a new email message to a recipient."},
		{"email_search", "Search and read existing email messages."},
		{"document_search", "Search the user's already indexed private documents and manuals."},
		{"document_upload", "Upload or ingest a new document into the private document library."},
		{"memory_add_preference", "Remember a durable user preference for future conversations."},
		{"memory_search", "Recall previously stored user memories and preferences."},
		{"calendar_create_event", "Create a new event or appointment in the user's calendar."},
		{"calendar_list_events", "List existing calendar events in a time range."},
		{"finance_quote", "Get the latest market price for a stock or cryptocurrency."},
		{"finance_news", "Find recent financial news about a company or asset."},
		{"web_search", "Search the public internet for current information."},
		{"web_open", "Open and inspect a specific public web page URL."},
		{"shell_exec", "Execute a command or script in the user workspace."},
		{"shell_read_file", "Read a local workspace file without executing a command."},
	}
}

func toolScenarios() []scenario {
	rows := []struct{ id, family, prompt, expected string }{
		{"ht01", "near_duplicate_weather", "How warm was Milan on 5 January 2025?", "weather_history"},
		{"ht02", "near_duplicate_email", "Find the last email I received from Elena about the audit.", "email_search"},
		{"ht03", "near_duplicate_document", "Import the attached safety handbook into my private library.", "document_upload"},
		{"ht04", "near_duplicate_memory", "What temperature unit did I previously ask you to use?", "memory_search"},
		{"ht05", "near_duplicate_calendar", "Show my appointments between Monday and Wednesday.", "calendar_list_events"},
		{"ht06", "near_duplicate_finance", "Find today's news affecting ASML shares.", "finance_news"},
		{"ht07", "near_duplicate_web", "Open https://neo4j.com/docs and inspect its installation section.", "web_open"},
		{"ht08", "near_duplicate_shell", "Read go.mod from the workspace without running it.", "shell_read_file"},
		{"ht09", "multilingual_action", "Invia a Luca una nuova email con oggetto preventivo.", "send_email"},
		{"ht10", "multilingual_action", "Esegui go test ./internal/semindex nella workspace.", "shell_exec"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{
			ID: row.id, Domain: "tool", Prompt: row.prompt, Expected: row.expected,
			Meta: map[string]string{"family": row.family},
		})
	}
	return out
}

func skillCatalog() []catalogItem {
	return []catalogItem{
		{"golang-testing", "Design idiomatic Go tests, benchmarks, fuzzing, coverage, and race checks."},
		{"vitest", "Write and debug Vitest unit tests for TypeScript or JavaScript."},
		{"web-research", "Research current information online using primary sources and extract evidence."},
		{"document-pdf", "Read, extract, inspect, or transform PDF documents."},
		{"pdf-to-docx", "Convert a PDF document into an editable DOCX document."},
		{"xlsx", "Create, edit, inspect, or validate Excel XLSX workbooks."},
		{"docker-compose-orchestration", "Design, run, debug, and validate Docker Compose services."},
		{"postgresql-optimization", "Diagnose and optimize PostgreSQL queries, indexes, and database settings."},
		{"accessibility-a11y", "Audit or implement WCAG accessibility for web interfaces."},
		{"analytics-tracking", "Design or audit product analytics events and tracking plans."},
		{"systematic-debugging", "Investigate a reproducible bug using hypothesis-driven debugging."},
		{"copywriting", "Write or improve marketing and product copy."},
	}
}

func skillScenarios() []scenario {
	rows := []struct{ id, family, prompt, expected string }{
		{"hs01", "new_catalog_neighbor", "Add mocked TypeScript unit tests using vi.fn and fake timers.", "vitest"},
		{"hs02", "new_catalog_neighbor", "Turn this scanned PDF into an editable Word document.", "pdf-to-docx"},
		{"hs03", "new_catalog_neighbor", "Explain this slow EXPLAIN ANALYZE and propose the right PostgreSQL index.", "postgresql-optimization"},
		{"hs04", "new_catalog_neighbor", "Define analytics events for signup, activation, and subscription conversion.", "analytics-tracking"},
		{"hs05", "paraphrase", "Use fuzzing and the race detector to harden this Go parser.", "golang-testing"},
		{"hs06", "paraphrase", "Verify the latest API behavior using only first-party documentation.", "web-research"},
		{"hs07", "paraphrase", "The service fails intermittently; isolate the cause before changing code.", "systematic-debugging"},
		{"hs08", "paraphrase", "Check screen-reader names, focus order, and keyboard traps.", "accessibility-a11y"},
		{"hs09", "multilingual", "Crea un foglio Excel con formule, convalida e formattazione condizionale.", "xlsx"},
		{"hs10", "multilingual", "Rendi piu convincente il testo della pagina prezzi.", "copywriting"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{
			ID: row.id, Domain: "skill", Prompt: row.prompt, Expected: row.expected,
			Meta: map[string]string{"family": row.family},
		})
	}
	return out
}

func knowledgeCorpus() ([]scenario, []knowledgeFact) {
	facts := []knowledgeFact{
		{"hf01", "Project Juniper's private release phrase is SILVER-29.", ""},
		{"hf02", "The Vega-R controller warranty is nine years.", ""},
		{"hf03", "Vendor Meridian's internal support alias is mira-desk.", ""},
		{"hf04", "Depot Echo uses emergency radio channel 11.", ""},
		{"hf05", "Compressor Boreal belongs to Plant Rowan.", "hf06"},
		{"hf06", "Plant Rowan's inspection window is Thursday at 04:30 UTC.", ""},
		{"hf07", "Program Lumen uses supplier Basalt.", "hf08"},
		{"hf08", "Supplier Basalt has a twenty-three-day lead time.", ""},
		{"hf09", "Sensor Nacre is governed by Standard Green.", "hf10"},
		{"hf10", "Standard Green requires key rotation every sixty days.", ""},
		{"hf11", "Drone Sable is assigned to Sector Violet.", "hf12"},
		{"hf12", "Sector Violet requires safety class S4.", ""},
	}
	rows := []struct{ id, family, prompt, expected string }{
		{"hk01", "direct_private_fact", "What is Project Juniper's private release phrase?", "silver-29"},
		{"hk02", "direct_private_fact", "How long is the Vega-R controller warranty?", "9 years"},
		{"hk03", "direct_private_fact", "What is the internal alias for vendor Meridian?", "mira-desk"},
		{"hk04", "direct_private_fact", "Which radio channel does Depot Echo use in an emergency?", "11"},
		{"hk05", "two_hop_private_fact", "When is Compressor Boreal inspected?", "thursday 04:30 utc"},
		{"hk06", "two_hop_private_fact", "What is the supplier lead time for Program Lumen?", "23 days"},
		{"hk07", "two_hop_private_fact", "How often are keys rotated for Sensor Nacre?", "60 days"},
		{"hk08", "two_hop_private_fact", "Which safety class applies to Drone Sable?", "s4"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{
			ID: row.id, Domain: "knowledge", Prompt: row.prompt, Expected: row.expected,
			Meta: map[string]string{"family": row.family},
		})
	}
	return out, facts
}

func catalogPrompt(items []catalogItem) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "- %s: %s\n", item.Name, item.Description)
	}
	return b.String()
}
