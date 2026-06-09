package openai_compat

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

const minItalianAdaptiveReasoningScore = 0.90

func TestAdaptiveReasoningItalianCorpusE2E(t *testing.T) {
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	cfg := llm.Config{
		Provider:          "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		Model:             "deepseek/deepseek-v4-flash:exacto",
		Temperature:       0.2,
		MaxTokens:         4096,
		AdaptiveReasoning: true,
	}

	cases := []struct {
		query      string
		effort     string
		exclude    bool
		maxTokens  int
		wantReason string
	}{
		{"ciao", "none", true, 512, "saluto"},
		{"buongiorno Aura", "none", true, 512, "saluto"},
		{"grazie mille", "none", true, 512, "cortesia"},
		{"ok perfetto", "none", true, 512, "ack"},
		{"come ti chiami?", "none", true, 512, "domanda semplice"},
		{"chi ha scritto Il nome della rosa?", "none", true, 512, "fatto stabile"},
		{"traduci 'good morning' in italiano", "none", true, 512, "trasformazione semplice"},
		{"riassumi questa frase in cinque parole", "none", true, 512, "sintesi breve"},
		{"fammi un elenco di tre colori primari", "none", true, 512, "risposta breve"},
		{"quanto fa 15 piu 27?", "none", true, 512, "aritmetica semplice"},
		{"scrivi una frase gentile per salutare un cliente", "none", true, 512, "generazione breve"},
		{"converti 10 euro in centesimi", "none", true, 512, "calcolo diretto"},
		{"dimmi il plurale di ciliegia", "none", true, 512, "lingua semplice"},
		{"prepara un titolo breve per una nota interna", "none", true, 512, "testo breve"},
		{"spiegami cos'e un database in due righe", "none", true, 512, "spiegazione breve"},
		{"dammi tre sinonimi di veloce", "none", true, 512, "lessico"},
		{"rispondi con si o no: Roma e in Italia?", "none", true, 512, "fatto stabile"},
		{"scrivi un messaggio di auguri per Marta", "none", true, 512, "testo semplice"},
		{"metti in ordine alfabetico: pera mela banana", "none", true, 512, "trasformazione semplice"},
		{"quanto sono 2 ore in minuti?", "none", true, 512, "conversione semplice"},
		{"cerca notizie di cuneo", "low", false, 2048, "news locale"},
		{"trova le ultime notizie su Cuneo oggi", "low", false, 2048, "news locale"},
		{"controlla il meteo di Torino per domani", "low", false, 2048, "meteo"},
		{"cerca aggiornamenti sullo sciopero dei treni", "low", false, 2048, "news operativa"},
		{"trova il prezzo attuale di bitcoin", "low", false, 2048, "quotazione"},
		{"verifica se la farmacia di turno e aperta a Cuneo", "low", false, 2048, "lookup locale"},
		{"recupera gli orari del museo egizio", "low", false, 2048, "lookup orari"},
		{"cerca fonti recenti sul bonus casa", "low", false, 2048, "ricerca fonti"},
		{"trova il risultato della Juventus di ieri", "low", false, 2048, "sport recente"},
		{"controlla la classifica aggiornata di Serie A", "low", false, 2048, "sport classifica"},
		{"cerca eventi questo weekend a Bra", "low", false, 2048, "eventi"},
		{"aggiorna la situazione traffico sulla A6", "low", false, 2048, "traffico"},
		{"trova il numero del comune di Cuneo", "low", false, 2048, "lookup contatto"},
		{"cerca recensioni recenti di un ristorante a Saluzzo", "low", false, 2048, "recensioni"},
		{"verifica la quotazione di Stellantis oggi", "low", false, 2048, "finanza"},
		{"controlla se domani piove a Mondovi", "low", false, 2048, "meteo"},
		{"cerca aggiornamenti sulla viabilita in centro", "low", false, 2048, "lookup locale"},
		{"trova fonti sulla nuova legge regionale", "low", false, 2048, "fonti"},
		{"recupera le ultime uscite al cinema Monviso", "low", false, 2048, "eventi cinema"},
		{"cerca il cambio euro dollaro aggiornato", "low", false, 2048, "quotazione"},
		{"scrivi uno script di scraping di la stampa", "high", false, 4096, "codice scraping"},
		{"analizza i pro e contro di usare Neo4j per la memoria", "high", false, 4096, "analisi architetturale"},
		{"debugga questo errore di concorrenza nel worker pool", "high", false, 4096, "debug"},
		{"dimostra per assurdo che la radice di 2 e irrazionale", "high", false, 4096, "dimostrazione"},
		{"progetta un'architettura per notifiche Telegram resilienti", "high", false, 4096, "architettura"},
		{"confronta due strategie di caching per un agente LLM", "high", false, 4096, "confronto tecnico"},
		{"ottimizza questa query SQL con join e filtri", "high", false, 4096, "ottimizzazione"},
		{"implementa una funzione Go per scrivere file atomici", "high", false, 4096, "codice"},
		{"spiega perche due servizi possono duplicare un job", "high", false, 4096, "causalita complessa"},
		{"crea un piano tecnico per migrare il database senza downtime", "high", false, 4096, "piano tecnico"},
		{"scrivi un Dockerfile sicuro per un servizio Go", "high", false, 4096, "codice infra"},
		{"analizza un tradeoff tra latenza e accuratezza nel retrieval", "high", false, 4096, "tradeoff"},
		{"risolvi un bug dove due goroutine chiudono lo stesso canale", "high", false, 4096, "debug concurrency"},
		{"progetta un workflow CI per test unitari e integrazione", "high", false, 4096, "workflow"},
		{"implementa uno scraper con rate limit e rispetto del robots.txt", "high", false, 4096, "scraper"},
		{"confronta llama.cpp e vLLM per un modello locale", "high", false, 4096, "confronto tecnico"},
		{"dimostra la correttezza di un algoritmo di deduplica", "high", false, 4096, "dimostrazione algoritmo"},
		{"debugga un timeout intermittente su una chiamata HTTP streaming", "high", false, 4096, "debug streaming"},
		{"progetta una strategia di rollback per una migration fallita", "high", false, 4096, "strategia"},
		{"ottimizza un prompt builder senza rompere la cache prefix", "high", false, 4096, "ottimizzazione tecnica"},
	}

	correct := 0
	var misses []string
	for _, tc := range cases {
		history := []llm.Message{
			{Role: llm.RoleSystem, Content: "STATIC-AURA-SYSTEM"},
			{Role: llm.RoleUser, Content: tc.query},
		}
		req := builder.Build(history, reg, cfg.Provider, cfg, prompt.Budget{})
		body := decodeBody(t, captureBody(t, req))
		effort, exclude, ok := reasoningFields(body)
		gotMax, _ := body["max_tokens"].(float64)
		choice, _ := body["tool_choice"].(string)
		_, toolsPresent := body["tools"]
		wantChoice, wantTools := expectedToolWire(tc.effort)
		if ok && effort == tc.effort && exclude == tc.exclude && int(gotMax) == tc.maxTokens &&
			choice == wantChoice && toolsPresent == wantTools {
			correct++
			continue
		}
		misses = append(misses, fmt.Sprintf("%q (%s): got effort=%q exclude=%v max_tokens=%d tool_choice=%q tools=%v, want effort=%q exclude=%v max_tokens=%d tool_choice=%q tools=%v",
			tc.query, tc.wantReason, effort, exclude, int(gotMax), choice, toolsPresent,
			tc.effort, tc.exclude, tc.maxTokens, wantChoice, wantTools))
	}

	score := float64(correct) / float64(len(cases))
	t.Logf("Italian adaptive reasoning E2E score: %.1f%% (%d/%d)", score*100, correct, len(cases))
	if score < minItalianAdaptiveReasoningScore {
		t.Fatalf("Italian adaptive reasoning E2E score %.1f%% below %.0f%% gate; misses:\n%s",
			score*100, minItalianAdaptiveReasoningScore*100, joinLines(misses))
	}
}

func decodeBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	return body
}

func reasoningFields(body map[string]any) (effort string, exclude bool, ok bool) {
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning == nil {
		return "", false, false
	}
	effort, _ = reasoning["effort"].(string)
	exclude, _ = reasoning["exclude"].(bool)
	return effort, exclude, effort != ""
}

func expectedToolWire(effort string) (choice string, toolsPresent bool) {
	if effort == "none" {
		return "none", false
	}
	return "auto", true
}

func joinLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += "- " + line + "\n"
	}
	return out
}
