// Spike 053 — does the active-learning self-improvement loop actually help?
//
// Hypothesis (operator idea): when the granite embedding reasoning-tier
// classifier is UNCERTAIN (low cosine margin), label that turn with the LLM
// router as an oracle, fold the (embedding, tier) into the example bank, and the
// classifier improves over time — WITHOUT adding latency to the turn (labeling
// is async/offline).
//
// This spike simulates the loop offline and money-free:
//  1. Baseline = validated B classifier (def + 5 seeds/tier centroids).
//  2. Measure accuracy on a FIXED held-out set.
//  3. Stream a pool of prompts: on margin < T, ask the Gemma-E2B oracle to
//     label it and add the embedding to the per-tier bank.
//  4. Re-measure the SAME held-out set with (a) refreshed centroids and
//     (b) kNN over seeds+collected examples.
//  5. Report delta, oracle calls (cost), and oracle LABEL NOISE (the risk).
//
// Embeddings: granite :8081. Oracle: Gemma-E2B :8095. Run:
//
//	go run ./.planning/spikes/053-reasoning-classifier-active-learning
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

const (
	embedURL     = "http://127.0.0.1:8081/v1/embeddings"
	oracleURL    = "http://127.0.0.1:8095/v1/chat/completions"
	marginFloor  = 0.05 // below this top-2 cosine margin → "uncertain" → ask the oracle
	routerPrompt = "Classify the latest user request into one reasoning tier for the next assistant turn. Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}. none=greeting/simple stable fact/direct short transform. low=current web/news/weather/prices/schedules/lookups or small tool use. high=coding/debugging/design/proofs/scraping/multi-step analysis."
)

var tiers = []string{"none", "low", "high"}

var defs = map[string]string{
	"none": "saluto, ringraziamento, chiacchiera, fatto semplice e stabile, o trasformazione breve e diretta",
	"low":  "informazione corrente dal web: meteo, notizie, prezzi, orari, ricerche e lookup, oppure piccolo uso di strumenti",
	"high": "scrittura di codice, debug, progettazione, dimostrazioni, scraping, analisi in piu passaggi",
}

var seeds = map[string][]string{
	"none": {"ciao", "grazie mille", "come ti chiami?", "ripeti per favore", "traduci 'gatto' in inglese"},
	"low":  {"che tempo fa a Torino domani?", "cerca le ultime notizie su Cuneo", "quanto costa il bitcoin adesso?", "a che ora chiude la farmacia?", "trova un ristorante aperto stasera vicino a me"},
	"high": {"scrivi uno script python per fare scraping di un sito con gestione errori", "aiutami a debuggare questa funzione che va in segfault", "progetta lo schema di un database per un e-commerce", "dimostra per induzione che la somma dei primi n numeri e n(n+1)/2", "rifattorizza questo modulo in piu file mantenendo i test verdi"},
}

// heldout is FIXED — measured before and after. Includes the baseline's known
// weak none cases (stable facts, arithmetic) so improvement is observable.
var heldout = []struct{ prompt, gold string }{
	{"buongiorno", "none"}, {"grazie infinite", "none"},
	{"qual e la capitale della Francia?", "none"}, {"quanti giorni ha un anno?", "none"},
	{"quanto fa 8 per 9?", "none"}, {"chi ha dipinto la Gioconda?", "none"},
	{"quante zampe ha un ragno?", "none"}, {"come si scrive 'cuore' in inglese?", "none"},
	{"ripetimi quello che ho detto", "none"}, {"chi e il padre della geometria?", "none"},
	{"che tempo fa a Napoli oggi?", "low"}, {"ultime notizie sulla Juventus", "low"},
	{"prezzo dell'oro in questo momento", "low"}, {"a che ora apre la banca domani?", "low"},
	{"cerca un hotel a Roma per stanotte", "low"}, {"quando arriva il prossimo autobus?", "low"},
	{"com'e il traffico verso Milano ora?", "low"}, {"cambio dollaro euro adesso", "low"},
	{"che spettacoli ci sono stasera in citta?", "low"}, {"trova una pizzeria aperta vicino", "low"},
	{"scrivi una funzione che ordina una lista in Go", "high"}, {"perche il mio programma va in deadlock?", "high"},
	{"progetta l'architettura di un sistema di chat", "high"}, {"dimostra il teorema di Pitagora", "high"},
	{"ottimizza questo ciclo annidato lento", "high"}, {"scrivi una regex per validare le email", "high"},
	{"debugga questo errore di segmentation fault", "high"}, {"analizza la complessita di questo algoritmo", "high"},
	{"crea uno script bash per il backup giornaliero", "high"}, {"spiega e implementa una hash map", "high"},
}

// stream is fed through the loop. Gold is for ANALYSIS only (label-noise rate);
// the loop uses the oracle's labels, not gold. Covers heldout failure modes with
// different instances (stable facts / arithmetic the baseline routes to low).
var stream = []struct{ prompt, gold string }{
	{"salve", "none"}, {"buonasera a tutti", "none"}, {"grazie di tutto", "none"}, {"perfetto, ottimo lavoro", "none"},
	{"qual e la capitale della Spagna?", "none"}, {"qual e la capitale del Giappone?", "none"},
	{"quanto fa 12 piu 30?", "none"}, {"quanto fa 100 diviso 4?", "none"},
	{"chi ha scritto l'Odissea?", "none"}, {"chi ha inventato il telefono?", "none"},
	{"quanti continenti ci sono?", "none"}, {"quante note musicali esistono?", "none"},
	{"come si dice 'sole' in francese?", "none"}, {"scrivi 'casa' al contrario", "none"}, {"in che anno cadde l'impero romano?", "none"},
	{"che tempo fa a Palermo domani?", "low"}, {"previsioni per il fine settimana", "low"},
	{"notizie di economia di oggi", "low"}, {"ultime news dal mondo", "low"},
	{"quanto costa un litro di latte adesso?", "low"}, {"prezzo azioni Apple ora", "low"},
	{"orari dei negozi il sabato", "low"}, {"a che ora chiude il museo oggi?", "low"},
	{"cerca voli per Parigi a settembre", "low"}, {"trova un parcheggio libero vicino", "low"},
	{"risultati delle partite di ieri", "low"}, {"classifica della serie A aggiornata", "low"},
	{"dove posso mangiare sushi stasera?", "low"}, {"quando esce il prossimo iphone?", "low"}, {"che film esce al cinema venerdi?", "low"},
	{"scrivi un parser JSON in Python", "high"}, {"implementa una cache LRU", "high"},
	{"perche ricevo un null pointer exception?", "high"}, {"debugga questo memory leak", "high"},
	{"progetta uno schema per un social network", "high"}, {"disegna un'architettura a eventi", "high"},
	{"dimostra che la somma di due pari e pari", "high"}, {"ottimizza le query lente del database", "high"},
	{"scrivi i test per questo modulo di auth", "high"}, {"crea una pipeline di CI/CD completa", "high"},
	{"fai lo scraping di un sito con paginazione", "high"}, {"analizza questo dump di memoria", "high"},
	{"spiega e implementa quicksort", "high"}, {"riduci la complessita di questa funzione ricorsiva", "high"}, {"scrivi un rate limiter con token bucket", "high"},
}

func logf(f string, a ...any) { fmt.Printf(f+"\n", a...) }

func embed(text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]any{"model": "granite", "input": text})
	resp, err := http.Post(embedURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embed %d", resp.StatusCode)
	}
	var er struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &er); err != nil || len(er.Data) == 0 {
		return nil, fmt.Errorf("embed parse")
	}
	return normalize(er.Data[0].Embedding), nil
}

func oracleLabel(prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":                "gemma",
		"messages":             []map[string]string{{"role": "system", "content": routerPrompt}, {"role": "user", "content": prompt}},
		"max_tokens":           48,
		"temperature":          0,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	resp, err := http.Post(oracleURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil || len(cr.Choices) == 0 {
		return "", fmt.Errorf("oracle parse")
	}
	return parseTier(cr.Choices[0].Message.Content), nil
}

func parseTier(raw string) string {
	b := raw
	if s := strings.Index(b, "{"); s >= 0 {
		if e := strings.LastIndex(b, "}"); e >= s {
			b = b[s : e+1]
		}
	}
	var o struct {
		Tier string `json:"tier"`
	}
	t := raw
	if json.Unmarshal([]byte(b), &o) == nil && o.Tier != "" {
		t = o.Tier
	}
	t = strings.ToLower(t)
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

func normalize(v []float64) []float64 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	o := make([]float64, len(v))
	for i, x := range v {
		o[i] = x / n
	}
	return o
}

func cosine(a, b []float64) float64 {
	var d float64
	for i := range a {
		d += a[i] * b[i]
	}
	return d
}

func centroid(vs [][]float64) []float64 {
	acc := make([]float64, len(vs[0]))
	for _, v := range vs {
		for i := range acc {
			acc[i] += v[i]
		}
	}
	for i := range acc {
		acc[i] /= float64(len(vs))
	}
	return normalize(acc)
}

type example struct {
	vec  []float64
	tier string
}

func classifyCentroid(q []float64, anchors map[string][]float64) (string, float64) {
	best, bestS, second := "", -2.0, -2.0
	for _, t := range tiers {
		s := cosine(q, anchors[t])
		if s > bestS {
			second, best, bestS = bestS, t, s
		} else if s > second {
			second = s
		}
	}
	return best, bestS - second
}

func classifyKNN(q []float64, bank []example, k int) string {
	type sc struct {
		s float64
		t string
	}
	scored := make([]sc, len(bank))
	for i, e := range bank {
		scored[i] = sc{cosine(q, e.vec), e.tier}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].s > scored[j].s })
	votes := map[string]float64{}
	for i := 0; i < k && i < len(scored); i++ {
		votes[scored[i].t] += scored[i].s // similarity-weighted
	}
	best, bestV := "", -1.0
	for _, t := range tiers {
		if votes[t] > bestV {
			best, bestV = t, votes[t]
		}
	}
	return best
}

func evalCentroid(anchors map[string][]float64, heldEmb [][]float64) (acc, noneVsRest float64) {
	correct, nb := 0, 0
	for i, c := range heldout {
		pred, _ := classifyCentroid(heldEmb[i], anchors)
		if pred == c.gold {
			correct++
		}
		if (pred == "none") == (c.gold == "none") {
			nb++
		}
	}
	n := float64(len(heldout))
	return 100 * float64(correct) / n, 100 * float64(nb) / n
}

func evalKNN(bank []example, heldEmb [][]float64, k int) (acc, noneVsRest float64) {
	correct, nb := 0, 0
	for i, c := range heldout {
		pred := classifyKNN(heldEmb[i], bank, k)
		if pred == c.gold {
			correct++
		}
		if (pred == "none") == (c.gold == "none") {
			nb++
		}
	}
	n := float64(len(heldout))
	return 100 * float64(correct) / n, 100 * float64(nb) / n
}

func main() {
	logf("spike 053 — active-learning loop (oracle=Gemma-E2B), heldout=%d stream=%d, marginFloor=%.2f", len(heldout), len(stream), marginFloor)
	if _, err := embed("probe"); err != nil {
		logf("granite :8081 unreachable: %v", err)
		os.Exit(1)
	}

	// Seed bank + baseline centroids.
	bank := []example{}
	anchors := map[string][]float64{}
	for _, t := range tiers {
		texts := append([]string{defs[t]}, seeds[t]...)
		var vs [][]float64
		for _, x := range texts {
			v, err := embed(x)
			if err != nil {
				logf("seed embed failed: %v", err)
				os.Exit(1)
			}
			vs = append(vs, v)
			bank = append(bank, example{v, t})
		}
		anchors[t] = centroid(vs)
	}

	// Pre-embed heldout once.
	heldEmb := make([][]float64, len(heldout))
	for i, c := range heldout {
		v, err := embed(c.prompt)
		if err != nil {
			logf("heldout embed failed: %v", err)
			os.Exit(1)
		}
		heldEmb[i] = v
	}

	baseAcc, baseNB := evalCentroid(anchors, heldEmb)
	baseKAcc, baseKNB := evalKNN(bank, heldEmb, 5)
	logf("\nBASELINE (seeds only):")
	logf("  centroid: accuracy %.0f%% | none-vs-rest %.0f%%", baseAcc, baseNB)
	logf("  kNN(5)  : accuracy %.0f%% | none-vs-rest %.0f%%", baseKAcc, baseKNB)

	// Wait for oracle.
	dl := time.Now().Add(120 * time.Second)
	for {
		r, err := http.Get("http://127.0.0.1:8095/health")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(dl) {
			logf("oracle :8095 not healthy — cannot run loop")
			os.Exit(1)
		}
		time.Sleep(3 * time.Second)
	}

	// Active-learning loop over the stream.
	oracleCalls, oracleWrong, added := 0, 0, 0
	confident := 0
	for _, s := range stream {
		v, err := embed(s.prompt)
		if err != nil {
			continue
		}
		_, margin := classifyCentroid(v, anchors)
		if margin >= marginFloor {
			confident++
			continue // confident: no oracle, no cost
		}
		label, err := oracleLabel(s.prompt)
		if err != nil || (label != "none" && label != "low" && label != "high") {
			continue
		}
		oracleCalls++
		if label != s.gold {
			oracleWrong++ // label noise: oracle disagreed with gold
		}
		bank = append(bank, example{v, label})
		added++
	}
	// Rebuild centroids including collected examples.
	byTier := map[string][][]float64{}
	for _, e := range bank {
		byTier[e.tier] = append(byTier[e.tier], e.vec)
	}
	after := map[string][]float64{}
	for _, t := range tiers {
		after[t] = centroid(byTier[t])
	}

	aAcc, aNB := evalCentroid(after, heldEmb)
	kAcc, kNB := evalKNN(bank, heldEmb, 5)
	logf("\nLOOP: %d/%d stream prompts uncertain → oracle; %d labels added; oracle noise %d/%d wrong vs gold", oracleCalls, len(stream), added, oracleWrong, oracleCalls)
	logf("  %d/%d were confident (no oracle call — zero cost)", confident, len(stream))
	logf("\nAFTER LEARNING:")
	logf("  centroid: accuracy %.0f%% (%+.0f) | none-vs-rest %.0f%% (%+.0f)", aAcc, aAcc-baseAcc, aNB, aNB-baseNB)
	logf("  kNN(5)  : accuracy %.0f%% (%+.0f) | none-vs-rest %.0f%% (%+.0f)", kAcc, kAcc-baseKAcc, kNB, kNB-baseKNB)

	logf("\nVERDICT: %s", verdict(baseAcc, aAcc, baseKAcc, kAcc, baseNB, aNB, baseKNB, kNB))
}

func verdict(bc, ac, bk, ak, bcn, acn, bkn, akn float64) string {
	improvedC := ac > bc || acn > bcn
	improvedK := ak > bk || akn > bkn
	if improvedC || improvedK {
		return "active learning HELPED — folding oracle-labeled uncertain cases raised held-out accuracy"
	}
	return "NO improvement — oracle labels did not lift held-out accuracy (idea weak on this setup)"
}
