package main

import (
	"fmt"
	"strings"
)

func reasoningScenarios() []scenario {
	rows := []struct{ id, prompt, expected string }{
		{"r01", "Return only the integer: 18 multiplied by 23.", "414"},
		{"r02", "Return only the city: what is the capital of Italy?", "rome"},
		{"r03", "A bat and ball cost $1.10 total. The bat costs $1 more than the ball. Return only the ball price in dollars.", "0.05"},
		{"r04", "If 5 machines make 5 widgets in 5 minutes, how many minutes do 100 machines need to make 100 widgets? Return only the integer.", "5"},
		{"r05", "A lily-pad patch doubles daily and covers a lake on day 48. Return only the day on which it covers half the lake.", "47"},
		{"r06", "Return only the integer: sum all integers from 1 through 20.", "210"},
		{"r07", "Return only the integer remainder when 987654 is divided by 37.", "24"},
		{"r08", "A price rises 25% and then falls 20%. Return only the final percentage of the original price.", "100"},
		{"r09", "All bloops are razzies. No razzies are lazzies. Can any bloop be a lazzy? Return only yes or no.", "no"},
		{"r10", "In Go, x := 3; x *= 4; x -= 5. Return only x.", "7"},
		{"r11", "Three consecutive integers sum to 72. Return only the largest.", "25"},
		{"r12", "A meeting begins at 23:35 and lasts 155 minutes. Return only the end time in 24-hour HH:MM format.", "02:10"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{ID: row.id, Domain: "reasoning", Prompt: row.prompt, Expected: row.expected})
	}
	return out
}

func toolCatalog() []catalogItem {
	return []catalogItem{
		{"weather_now", "Get current or forecast weather for a location."},
		{"send_email", "Send an email message to a recipient."},
		{"document_search", "Search the user's private indexed documents and manuals."},
		{"memory_add_preference", "Remember a durable user preference for future conversations."},
		{"calendar_create_event", "Create an event or appointment in the user's calendar."},
		{"finance_quote", "Get the latest market price for a stock or cryptocurrency."},
		{"web_search", "Search the public internet for current information."},
		{"shell_exec", "Execute a command or script in the user workspace."},
	}
}

func toolScenarios() []scenario {
	rows := []struct{ id, prompt, expected string }{
		{"t01", "What is the weather in Turin right now?", "weather_now"},
		{"t02", "Send Marco an email titled quarterly review.", "send_email"},
		{"t03", "Search my inverter manual for the warranty period.", "document_search"},
		{"t04", "Remember that I always want temperatures in Celsius.", "memory_add_preference"},
		{"t05", "Add a dentist appointment next Tuesday at 14:30.", "calendar_create_event"},
		{"t06", "What is the latest Bitcoin price?", "finance_quote"},
		{"t07", "Find today's official Neo4j release notes online.", "web_search"},
		{"t08", "Run go test ./internal/semindex in my workspace.", "shell_exec"},
		{"t09", "Cerca nei miei documenti la procedura di reset dell'inverter.", "document_search"},
		{"t10", "Ricordati che preferisco risposte concise.", "memory_add_preference"},
		{"t11", "Schedule lunch with Sara for Friday at noon.", "calendar_create_event"},
		{"t12", "Look up the current NVIDIA share price.", "finance_quote"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{ID: row.id, Domain: "tool", Prompt: row.prompt, Expected: row.expected})
	}
	return out
}

func skillCatalog() []catalogItem {
	return []catalogItem{
		{"golang-testing", "Design or review idiomatic Go tests, benchmarks, fuzzing, coverage, and race checks."},
		{"web-research", "Research current information online using primary sources and extract evidence."},
		{"document-pdf", "Read, extract, inspect, or transform PDF documents."},
		{"xlsx", "Create, edit, inspect, or validate Excel XLSX workbooks."},
		{"docker-compose-orchestration", "Design, run, debug, and validate Docker Compose services."},
		{"accessibility-a11y", "Audit or implement WCAG accessibility for web interfaces."},
		{"systematic-debugging", "Investigate a reproducible bug using hypothesis-driven debugging."},
		{"copywriting", "Write or improve marketing and product copy."},
	}
}

func skillScenarios() []scenario {
	rows := []struct{ id, prompt, expected string }{
		{"s01", "Add table-driven Go tests and a race benchmark for this worker pool.", "golang-testing"},
		{"s02", "Research the newest official Neo4j GDS pipeline guidance.", "web-research"},
		{"s03", "Extract the tables from this PDF maintenance manual.", "document-pdf"},
		{"s04", "Create an Excel workbook with formulas and conditional formatting.", "xlsx"},
		{"s05", "Diagnose why my compose service never becomes healthy.", "docker-compose-orchestration"},
		{"s06", "Audit this dialog against WCAG keyboard and focus requirements.", "accessibility-a11y"},
		{"s07", "This test flakes once in fifty runs; investigate the root cause.", "systematic-debugging"},
		{"s08", "Rewrite this landing-page headline to improve conversion.", "copywriting"},
		{"s09", "Scrivi benchmark Go ripetibili con benchmem e race detector.", "golang-testing"},
		{"s10", "Cerca online solo fonti ufficiali sulla nuova API.", "web-research"},
		{"s11", "Controlla accessibilità, focus e contrasto di questa pagina.", "accessibility-a11y"},
		{"s12", "Analizza un crash intermittente senza applicare ancora la fix.", "systematic-debugging"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{ID: row.id, Domain: "skill", Prompt: row.prompt, Expected: row.expected})
	}
	return out
}

func knowledgeCorpus() ([]scenario, []knowledgeFact) {
	facts := []knowledgeFact{
		{"f01", "Project Helios launch code is KESTREL-47.", ""},
		{"f02", "The Orion-X inverter warranty is seven years.", ""},
		{"f03", "The Northstar vendor contact alias is nora-ops.", ""},
		{"f04", "Warehouse Delta's emergency channel is radio 6.", ""},
		{"f05", "Pump Atlas belongs to Site Cedar.", "f06"},
		{"f06", "Site Cedar's maintenance window is Tuesday at 02:00 UTC.", ""},
		{"f07", "Project Ember uses supplier Quartz.", "f08"},
		{"f08", "Supplier Quartz has an eighteen-day lead time.", ""},
		{"f09", "Device Kappa is governed by Policy Blue.", "f10"},
		{"f10", "Policy Blue requires credential rotation every ninety days.", ""},
		{"f11", "Robot Mica is assigned to Zone Amber.", "f12"},
		{"f12", "Zone Amber requires safety class S3.", ""},
	}
	rows := []struct{ id, prompt, expected string }{
		{"k01", "What is the private launch code for Project Helios?", "kestrel-47"},
		{"k02", "How many years is the Orion-X inverter warranty?", "7"},
		{"k03", "What is the contact alias for vendor Northstar?", "nora-ops"},
		{"k04", "Which emergency radio channel does Warehouse Delta use?", "6"},
		{"k05", "When is the maintenance window for Pump Atlas?", "tuesday 02:00 utc"},
		{"k06", "What is the supplier lead time for Project Ember?", "18 days"},
		{"k07", "How often must credentials rotate for Device Kappa?", "90 days"},
		{"k08", "Which safety class applies to Robot Mica?", "s3"},
	}
	out := make([]scenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, scenario{ID: row.id, Domain: "knowledge", Prompt: row.prompt, Expected: row.expected})
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
