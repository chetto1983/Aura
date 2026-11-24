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
// indexes. Only names Aura owns belong here: built-ins, the memory server and the PIM
// (Aura's fork, mounted as `calendar`). A third-party MCP server names its own tools and
// can rename them. A PIM mounted under another server name gets none of its keywords.
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
	"mail_watch": {
		"email", "inbox", "notify", "digest", "triage", "sender", "posta", "avvisami", "avvisarmi", "riepilogo",
		"mittente", "smista", "urgenti",
	},
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

	// The PIM's 29 tools, since the 2026-11-24 split made the model find one of them. The
	// generic nouns (mail, posta, agenda, rubrica) sit on the tools that read; the tools that
	// change something carry their verbs, so a reading query does not rank a delete. Italian
	// nouns are listed in both numbers: foldPlural folds only English plurals.
	"calendar__get_emails":                   {"mail", "inbox", "unread", "received", "posta", "casella", "ricevuta", "ricevute"},
	"calendar__search_emails":                {"mail", "posta", "find", "cerca", "trova"},
	"calendar__get_email_details":            {"body", "content", "apri", "contenuto", "testo"},
	"calendar__get_email_attachment":         {"allegato", "allegati"},
	"calendar__get_contextual_email_summary": {"summary", "overview", "topic", "riassunto", "sintesi", "argomento", "argomenti"},
	"calendar__send_email":                   {"mail", "forward", "invia", "inviare", "spedisci", "rispondi", "inoltra"},
	"calendar__mark_email_read":              {"seen", "letta", "letto"},
	"calendar__bulk_mark_emails_read":        {"seen", "lette", "tutte"},
	"calendar__delete_email":                 {"trash", "elimina", "cancella", "cestino"},
	"calendar__bulk_delete_emails":           {"trash", "elimina", "cestino", "tutte"},
	"calendar__move_email":                   {"archive", "folder", "label", "archivia", "cartella", "etichetta", "sposta"},
	"calendar__bulk_move_emails":             {"archive", "folder", "archivia", "cartella", "sposta", "tutte"},
	"calendar__get_unsubscribe_info":         {"newsletter", "mailing"},
	"calendar__unsubscribe_from_email":       {"newsletter", "spam", "disiscriviti", "disiscrivimi"},
	"calendar__get_calendar_events": {
		"agenda", "meeting", "appointment", "busy", "calendario", "impegno", "impegni",
		"appuntamento", "appuntamenti", "riunione", "riunioni", "evento", "eventi",
	},
	"calendar__create_event":               {"meeting", "appointment", "appuntamento", "riunione", "crea", "nuovo", "prenota", "organizza"},
	"calendar__update_event":               {"reschedule", "postpone", "sposta", "rinvia", "posticipa", "anticipa", "modifica"},
	"calendar__delete_event":               {"cancel", "annulla", "disdici", "elimina"},
	"calendar__respond_to_event":           {"invitation", "invite", "rsvp", "invito", "accetta", "rifiuta", "partecipa", "conferma"},
	"calendar__get_calendar_event_details": {"attendee", "dettagli", "partecipante", "partecipanti"},
	"calendar__list_calendars":             {"calendari"},
	"calendar__get_contacts":               {"phonebook", "rubrica", "contatto", "contatti", "numero"},
	"calendar__search_contacts":            {"phonebook", "rubrica", "numero", "cerca", "trova"},
	"calendar__get_contact_details":        {"scheda"},
	"calendar__create_contact":             {"rubrica", "nuovo"},
	"calendar__update_contact":             {"rubrica", "modifica", "aggiorna"},
	"calendar__delete_contact":             {"rubrica", "elimina"},
	"calendar__list_accounts":              {"mailbox", "indirizzo", "caselle"},
	"calendar__get_guide":                  {"help", "manual", "guida", "istruzioni"},
}
