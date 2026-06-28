// Spike 052 — reasoning-tier embedding classifier (replace the paid LLM router)
//
// Question: can the local granite-embedding-97m sidecar (already running on
// :8081, 384d) classify a user turn into none/low/high by SEMANTIC proximity,
// so the adaptive-reasoning router stops costing a DeepSeek round-trip per turn?
//
// Two variants, both scored on the SAME held-out eval set (seeds disjoint):
//
//	A = zero-shot: anchors = the 3 tier DEFINITIONS only (no examples)
//	B = few-shot:  anchors = definition + 5 seed phrases per tier (SetFit-style)
//
// Run (granite sidecar up): go run ./.planning/spikes/052-reasoning-tier-embed-classifier
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const embedURL = "http://127.0.0.1:8081/v1/embeddings"
const gemmaURL = "http://127.0.0.1:8095/v1/chat/completions"

// The EXACT production reasoning-router system prompt (llm_agent_reasoning.go:14).
const routerPrompt = "Classify the latest user request into one reasoning tier for the next assistant turn. Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}. none=greeting/simple stable fact/direct short transform. low=current web/news/weather/prices/schedules/lookups or small tool use. high=coding/debugging/design/proofs/scraping/multi-step analysis."

var tiers = []string{"none", "low", "high"}

// The tier definitions are the ones already written in the production router
// system prompt — reused verbatim as zero-shot anchors.
var defs = map[string]string{
	"none": "saluto, ringraziamento, chiacchiera, fatto semplice e stabile, o trasformazione breve e diretta",
	"low":  "informazione corrente dal web: meteo, notizie, prezzi, orari, ricerche e lookup, oppure piccolo uso di strumenti",
	"high": "scrittura di codice, debug, progettazione, dimostrazioni, scraping, analisi in piu passaggi",
}

// Seeds for variant B (training). DISJOINT from the eval set below.
var seeds = map[string][]string{
	"none": {
		"ciao",
		"grazie mille",
		"come ti chiami?",
		"ripeti per favore",
		"traduci 'gatto' in inglese",
	},
	"low": {
		"che tempo fa a Torino domani?",
		"cerca le ultime notizie su Cuneo",
		"quanto costa il bitcoin adesso?",
		"a che ora chiude la farmacia?",
		"trova un ristorante aperto stasera vicino a me",
	},
	"high": {
		"scrivi uno script python per fare scraping di un sito con gestione errori",
		"aiutami a debuggare questa funzione che va in segfault",
		"progetta lo schema di un database per un e-commerce",
		"dimostra per induzione che la somma dei primi n numeri e n(n+1)/2",
		"rifattorizza questo modulo in piu file mantenendo i test verdi",
	},
}

// Held-out eval set (test), 60 prompts, 20 per tier. Deliberately DISJOINT
// from the seeds, with different vocabulary, multi-clause phrasings, slang,
// English, and genuinely borderline cases to stress generalization.
var eval = []struct{ prompt, gold string }{
	// --- none: greetings, thanks, chit-chat, stable facts, short transforms ---
	{"ciao Aura come va?", "none"},
	{"grazie sei stata utile", "none"},
	{"ok perfetto", "none"},
	{"buonasera", "none"},
	{"chi sei?", "none"},
	{"come stai oggi?", "none"},
	{"qual e la capitale della Francia?", "none"},
	{"quanti giorni ci sono in un anno?", "none"},
	{"scrivi 'ciao mondo' tutto maiuscolo", "none"},
	{"come si dice 'libro' in tedesco?", "none"},
	{"quanto fa 15 piu 27?", "none"},
	{"dimmi una barzelletta", "none"},
	{"ripeti l'ultima frase per favore", "none"},
	{"a presto!", "none"},
	{"che lingua si parla in Brasile?", "none"},
	{"conta da 1 a 5", "none"},
	{"ti ringrazio molto", "none"},
	{"spiegami in una riga cos'e un gatto", "none"},
	{"ok ho capito, grazie mille", "none"},
	{"hey, are you there?", "none"},
	// --- low: weather, news, prices, schedules, lookups, current events ---
	{"che tempo fa adesso a Genova?", "low"},
	{"ci sono notizie sull'alluvione in Piemonte?", "low"},
	{"quanto vale l'oro oggi?", "low"},
	{"a che ora apre il supermercato domani?", "low"},
	{"cerca voli economici per Madrid a luglio", "low"},
	{"qual e il cambio euro dollaro in questo momento?", "low"},
	{"trova un idraulico aperto adesso a Cuneo", "low"},
	{"quando gioca la Juventus la prossima partita?", "low"},
	{"ultime notizie di tecnologia di oggi", "low"},
	{"che traffico c'e sulla A6 in questo momento?", "low"},
	{"previsioni meteo per il weekend a Caraglio", "low"},
	{"prezzo medio di un caffe a Milano", "low"},
	{"cerca le recensioni del ristorante da Mario", "low"},
	{"is there a train strike tomorrow in Italy?", "low"},
	{"quanto costa il biglietto del cinema stasera?", "low"},
	{"che film danno al cinema questa settimana?", "low"},
	{"orario dei treni Torino-Cuneo domani mattina", "low"},
	{"come sta andando la borsa oggi?", "low"},
	{"search the latest news about the italian elections", "low"},
	{"quando e il prossimo concerto a Torino?", "low"},
	// --- high: code, debug, design, proofs, scraping, multi-step analysis ---
	{"scrivi una funzione python che calcola il fattoriale in modo ricorsivo", "high"},
	{"il mio container docker esce con codice 137, perche?", "high"},
	{"progetta le tabelle di un database per un sistema di prenotazioni", "high"},
	{"dimostra che ci sono infiniti numeri primi", "high"},
	{"fai lo scraping dei titoli di un sito di news rispettando il robots.txt", "high"},
	{"ottimizza questa query SQL che impiega 30 secondi", "high"},
	{"spiega passo passo come implementare un rate limiter token bucket", "high"},
	{"refactora questo codice spaghetti in funzioni pure e testabili", "high"},
	{"qual e la complessita temporale del quicksort e perche?", "high"},
	{"scrivi i test unitari per questa classe di pagamento", "high"},
	{"debugga: ricevo 'nil pointer dereference' in questo handler Go", "high"},
	{"progetta un'API REST per la gestione di un magazzino", "high"},
	{"write a recursive descent parser for arithmetic expressions", "high"},
	{"analizza pro e contro tra microservizi e monolite per la mia startup", "high"},
	{"implementa l'algoritmo di Dijkstra in Go con una heap", "high"},
	{"perche il mio modello ML va in overfitting e come lo risolvo?", "high"},
	{"crea un Dockerfile multi-stage per un'app Go con build cache", "high"},
	{"dimostra per assurdo che radice di 3 non e razionale", "high"},
	{"scrivi uno scraper asincrono con throttling per 10000 pagine", "high"},
	{"analizza questo log di crash e proponi tre ipotesi ordinate per probabilita", "high"},
}

func logf(format string, a ...any) { fmt.Printf(format+"\n", a...) }

type embedResp struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func embed(text string) ([]float64, time.Duration, error) {
	body, _ := json.Marshal(map[string]any{"model": "granite", "input": text})
	start := time.Now()
	resp, err := http.Post(embedURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("embed %d: %s", resp.StatusCode, string(raw))
	}
	var er embedResp
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, 0, err
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, 0, fmt.Errorf("empty embedding")
	}
	return normalize(er.Data[0].Embedding), time.Since(start), nil
}

func normalize(v []float64) []float64 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

func cosine(a, b []float64) float64 {
	var d float64
	for i := range a {
		d += a[i] * b[i]
	}
	return d // both already L2-normalized
}

func meanNormalize(vs [][]float64) []float64 {
	if len(vs) == 0 {
		return nil
	}
	acc := make([]float64, len(vs[0]))
	for _, v := range vs {
		for i, x := range v {
			acc[i] += x
		}
	}
	for i := range acc {
		acc[i] /= float64(len(vs))
	}
	return normalize(acc)
}

func classify(q []float64, anchors map[string][]float64) (string, float64, float64) {
	best, bestScore, second := "", -2.0, -2.0
	for _, t := range tiers {
		s := cosine(q, anchors[t])
		if s > bestScore {
			second = bestScore
			best, bestScore = t, s
		} else if s > second {
			second = s
		}
	}
	return best, bestScore, bestScore - second // margin
}

func buildAnchors(withSeeds bool) (map[string][]float64, error) {
	anchors := map[string][]float64{}
	for _, t := range tiers {
		vecs := [][]float64{}
		dv, _, err := embed(defs[t])
		if err != nil {
			return nil, err
		}
		vecs = append(vecs, dv)
		if withSeeds {
			for _, s := range seeds[t] {
				sv, _, err := embed(s)
				if err != nil {
					return nil, err
				}
				vecs = append(vecs, sv)
			}
		}
		anchors[t] = meanNormalize(vecs)
	}
	return anchors, nil
}

func runVariant(name string, withSeeds bool) {
	anchors, err := buildAnchors(withSeeds)
	if err != nil {
		logf("[%s] FAILED building anchors: %v", name, err)
		os.Exit(1)
	}
	correct, noneBinaryCorrect := 0, 0
	confusion := map[string]map[string]int{}
	for _, t := range tiers {
		confusion[t] = map[string]int{}
	}
	var lat []float64
	logf("\n=== Variant %s ===", name)
	for _, c := range eval {
		q, d, err := embed(c.prompt)
		if err != nil {
			logf("  embed FAILED %q: %v", c.prompt, err)
			os.Exit(1)
		}
		lat = append(lat, float64(d.Milliseconds()))
		pred, score, margin := classify(q, anchors)
		confusion[c.gold][pred]++
		ok := pred == c.gold
		if ok {
			correct++
		}
		// The consequential boundary: none vs not-none (DeepSeek collapses low->high).
		if (pred == "none") == (c.gold == "none") {
			noneBinaryCorrect++
		}
		mark := "  "
		if !ok {
			mark = "✗ "
		}
		logf("  %sgold=%-4s pred=%-4s (cos=%.3f margin=%.3f)  %q", mark, c.gold, pred, score, margin, c.prompt)
	}
	sort.Float64s(lat)
	p50 := lat[len(lat)/2]
	logf("  --- %s: accuracy %d/%d (%.0f%%) | none-vs-rest %d/%d (%.0f%%) | embed p50 %.0fms ---",
		name, correct, len(eval), 100*float64(correct)/float64(len(eval)),
		noneBinaryCorrect, len(eval), 100*float64(noneBinaryCorrect)/float64(len(eval)), p50)
	logf("  confusion (gold->pred):")
	for _, g := range tiers {
		logf("    %-4s -> none:%d low:%d high:%d", g, confusion[g]["none"], confusion[g]["low"], confusion[g]["high"])
	}
}

// llmTier replays the production LLM-router call against any local OpenAI-compat
// sidecar: same system prompt, reasoning off, 32-token JSON answer. Returns the
// parsed tier plus the raw content (for diagnosing format mismatches).
func llmTier(url, model, userPrompt string) (tier, rawContent string, d time.Duration, err error) {
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": routerPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":           96, // headroom in case a model ignores enable_thinking=false
		"temperature":          0,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	start := time.Now()
	resp, e := http.Post(url, "application/json", bytes.NewReader(body))
	if e != nil {
		return "", "", 0, e
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", "", time.Since(start), fmt.Errorf("%s %d: %s", model, resp.StatusCode, string(raw))
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil || len(cr.Choices) == 0 {
		return "", string(raw), time.Since(start), fmt.Errorf("parse: %v", err)
	}
	content := cr.Choices[0].Message.Content
	// FunctionGemma may answer via a tool_call instead of content — fold args in.
	for _, tc := range cr.Choices[0].Message.ToolCalls {
		content += " " + tc.Function.Name + " " + tc.Function.Arguments
	}
	return parseTier(content), content, time.Since(start), nil
}

// parseTier mirrors production parseReasoningRouterTier: extract the {...} and
// normalize none/low/high.
func parseTier(raw string) string {
	body := strings.TrimSpace(raw)
	if s := strings.Index(body, "{"); s >= 0 {
		if e := strings.LastIndex(body, "}"); e >= s {
			body = body[s : e+1]
		}
	}
	var obj struct {
		Tier string `json:"tier"`
	}
	t := raw
	if json.Unmarshal([]byte(body), &obj) == nil && obj.Tier != "" {
		t = obj.Tier
	}
	t = strings.ToLower(strings.Trim(strings.TrimSpace(t), "`\"' \t\r\n"))
	switch {
	case strings.Contains(t, "none"):
		return "none"
	case strings.Contains(t, "low"):
		return "low"
	case strings.Contains(t, "high"):
		return "high"
	}
	return "?"
}

func runLLMRouter(name, baseURL, model string) {
	url := baseURL + "/v1/chat/completions"
	correct, noneBin, fallbacks := 0, 0, 0
	confusion := map[string]map[string]int{}
	for _, t := range tiers {
		confusion[t] = map[string]int{}
	}
	var lat []float64
	rawSamples := 0
	logf("\n=== Variant %s (production router prompt, %s) ===", name, model)
	for _, c := range eval {
		pred, raw, d, err := llmTier(url, model, c.prompt)
		lat = append(lat, float64(d.Milliseconds()))
		bad := err != nil || (pred != "none" && pred != "low" && pred != "high")
		if bad {
			if rawSamples < 5 {
				logf("    [raw] %q -> err=%v content=%q", c.prompt, err, raw)
				rawSamples++
			}
			pred = "low" // production fallback on error/invalid
			fallbacks++
		}
		confusion[c.gold][pred]++
		ok := !bad && pred == c.gold
		if ok {
			correct++
		}
		if (pred == "none") == (c.gold == "none") {
			noneBin++
		}
		mark := "  "
		if pred != c.gold {
			mark = "✗ "
		}
		logf("  %sgold=%-4s pred=%-4s (%dms)  %q", mark, c.gold, pred, d.Milliseconds(), c.prompt)
	}
	sort.Float64s(lat)
	p50, p95 := lat[len(lat)/2], lat[int(0.95*float64(len(lat)-1))]
	logf("  --- %s: accuracy %d/%d (%.0f%%) | none-vs-rest %d/%d (%.0f%%) | latency p50 %.0fms p95 %.0fms | fallbacks %d ---",
		name, correct, len(eval), 100*float64(correct)/float64(len(eval)),
		noneBin, len(eval), 100*float64(noneBin)/float64(len(eval)), p50, p95, fallbacks)
	logf("  confusion (gold->pred):")
	for _, g := range tiers {
		logf("    %-4s -> none:%d low:%d high:%d", g, confusion[g]["none"], confusion[g]["low"], confusion[g]["high"])
	}
}

func waitHealth(baseURL string) bool {
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(3 * time.Second)
	}
	return false
}

func main() {
	logf("spike 052 — reasoning-tier classifier head-to-head on %d held-out prompts (IT+EN)", len(eval))
	if _, _, err := embed("probe"); err != nil {
		logf("granite sidecar unreachable at %s: %v", embedURL, err)
		os.Exit(1)
	}
	runVariant("A_zero_shot_definitions", false)
	runVariant("B_few_shot_seeds", true)
	if waitHealth("http://127.0.0.1:8095") {
		runLLMRouter("C_Gemma_E2B_router_GPU", "http://127.0.0.1:8095", "gemma")
	} else {
		logf("\nGemma E2B sidecar (:8095) not healthy — skipping variant C")
	}
	if waitHealth("http://127.0.0.1:8099") {
		runLLMRouter("D_FunctionGemma_270M_router_CPU", "http://127.0.0.1:8099", "functiongemma")
	} else {
		logf("\nFunctionGemma sidecar (:8099) not healthy — skipping variant D")
	}
	if waitHealth("http://127.0.0.1:8101") {
		runLLMRouter("E_Qwen3_0.6B_router_CPU", "http://127.0.0.1:8101", "qwen3")
	} else {
		logf("\nQwen3-0.6B sidecar (:8101) not healthy — skipping variant E")
	}
}
