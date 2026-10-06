package tools

// retrievalKeywords is the vocabulary an operator or a model uses for a capability when it
// does not use the tool's own words. It enters the BM25 retrieval document (searchDocument)
// and nothing else: it is never rendered to the model.
//
// It exists because lexical retrieval fails on term mismatch, not on scale. Measured
// 2026-10-06 on the live stack (the queries are pinned in
// TestRetrievalKeywordsRecoverLiveStackMisses): an Italian reminder request found no tool
// at all and "look up the current price of something online" loaded current_time, while
// `task` and `web_search` sat in the roster. Anthropic's own tool search gives the
// same advice for the same ranker: "use keywords in descriptions that match how users
// describe tasks" (platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
//
// Keyed by tool NAME, not carried on Spec, so the gate's dumped production corpus
// (testdata/deferred_manifest.json) is scored with exactly the vocabulary production
// indexes. Only names Aura owns belong here: built-ins and the memory server, whose mount
// is memoryServerName. A third-party MCP server names its own tools and can rename them.
//
// Words are written as tokenize emits them: lowercase ASCII, no accents (it splits on
// "à"), content words only. Italian keeps "ricordami" (remind me: task) apart from
// "ricordati" (remember: a memory fact) because the bare stem means both.
var retrievalKeywords = map[string][]string{
	"current_time": {"clock", "hour", "date", "today", "now", "ora", "ore", "orario", "data", "oggi", "adesso"},
	"task": {
		"reminder", "remind", "alarm", "later", "tomorrow", "recurring", "daily", "weekly", "morning",
		"ricordami", "ricordarmi", "promemoria", "sveglia", "domani", "programma",
		"programmare", //nolint:misspell // Italian "to schedule", not English "programmer".
		"pianifica", "ogni", "settimana", "mattina", "sera",
	},
	"todo_write":   {"checklist", "step", "plan", "progress", "track", "passi", "piano", "elenco", "attivita"},
	"web_search":   {"internet", "online", "google", "lookup", "latest", "price", "weather", "cerca", "cercare", "ricerca", "notizie", "prezzo", "meteo", "previsioni"},
	"web_fetch":    {"page", "website", "url", "link", "article", "download", "scarica", "pagina", "sito", "articolo"},
	"swarm_spawn":  {"parallel", "simultaneously", "concurrently", "helper", "delegate", "split", "parallelo", "contemporaneamente", "dividi", "delega"},
	"swarm_status": {"worker", "helper", "delegated", "progress", "stato", "avanzamento"},
	"skill_manage": {"install", "capability", "learn", "teach", "extension", "installa", "impara", "insegna", "capacita", "competenza"},
	"plugin_pack":  {"plugin", "bundle", "connector", "pacchetto"},
	"image_generate": {
		"image", "picture", "photo", "draw", "illustration", "logo", "poster",
		"immagine", "foto", "disegna", "disegno", "illustrazione", "locandina",
	},
	"video_generate": {"video", "clip", "animation", "animate", "filmato", "anima", "animazione"},
	"shell_poll":     {"output", "finished", "done", "running", "build", "log", "finito", "terminato", "completato"}, //nolint:misspell // Italian "terminato" (finished), not "termination".
	"shell_kill":     {"stop", "kill", "abort", "terminate", "interrompi", "ferma", "blocca"},

	"memory__memory_recall":      {"remember", "recall", "know", "previously", "said", "ricordi", "sai", "detto", "memoria"},
	"memory__memory_upsert_fact": {"remember", "save", "note", "store", "preference", "memorize", "segna", "segnati", "salva", "ricordati", "preferenza", "memorizza", "annota"},
	"memory__memory_forget":      {"forget", "erase", "dimentica", "dimenticati", "cancella"},
}
