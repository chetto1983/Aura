package prompt

import (
	"context"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/semindex"
	"golang.org/x/sync/singleflight"
)

// Embedder is the narrow embedding seam the reasoning classifier needs. It is a
// type alias of semindex.Embedder (the shared embedding-index core owns the
// canonical seam) so documents.EmbeddingClient satisfies both with no adapter
// and the classifier and the tool ranker depend on one interface (D-01).
type Embedder = semindex.Embedder

// Reasoning-tier anchors. The tier definitions are the production router's own
// wording; the seeds are the few-shot examples validated in spike 052 (variant
// B: 90% accuracy / 92% none-vs-rest over a 60-prompt held-out set, ~10ms CPU).
// Anchors generalize semantically — these phrases are NOT an enumeration of all
// inputs, they are prototypes the embedding model interpolates between.
var reasoningTierDefs = map[ReasoningTier]string{
	ReasoningTierNone: "saluto, ringraziamento, chiacchiera, fatto semplice e stabile gia noto, piccolo calcolo aritmetico, o trasformazione breve e diretta come una traduzione",
	ReasoningTierLow:  "informazione corrente dal web che cambia nel tempo: meteo, notizie, prezzi, orari di apertura, orari dei mezzi, traffico, risultati sportivi, ricerche e lookup, oppure piccolo uso di strumenti",
	ReasoningTierHigh: "scrittura di codice, debug di errori, progettazione di schemi e sistemi, dimostrazioni matematiche, ottimizzazione di algoritmi, scraping, analisi di stack trace, pipeline e build, analisi in piu passaggi",
}

// reasoningTierSeeds are the curated few-shot exemplars (spike-052 variant B:
// 90% accuracy / 92% none-vs-rest). They are prototypes the embedding model
// interpolates between, NOT an enumeration. Each tier carries one exemplar per
// recurring intent shape on the live Italian corpus (stable facts + arithmetic +
// transforms for none; the full set of changes-over-time lookups for low; the
// full code/proof/design/analysis spread for high) so the per-tier centroid sits
// at the semantic center of its tier rather than leaning toward one sub-intent.
var reasoningTierSeeds = map[ReasoningTier][]string{
	ReasoningTierNone: {
		"ciao",
		"grazie mille",
		"come ti chiami?",
		"ripeti per favore",
		"traduci 'gatto' in inglese",
		"qual e la capitale dell'Italia?",
		"quanto fa 7 per 8?",
		"a presto, buona giornata",
	},
	ReasoningTierLow: {
		"che tempo fa a Torino domani?",
		"cerca le ultime notizie su Cuneo",
		"quanto costa il bitcoin adesso?",
		"a che ora chiude la farmacia?",
		"trova un ristorante aperto stasera vicino a me",
		"quando parte il prossimo treno per Milano?",
		"come e finita la partita di ieri?",
		"c'e traffico in autostrada adesso?",
	},
	ReasoningTierHigh: {
		"scrivi uno script python per fare scraping di un sito con gestione errori",
		"aiutami a debuggare questa funzione che va in segfault",
		"progetta lo schema di un database per un e-commerce",
		"dimostra per induzione che la somma dei primi n numeri e n(n+1)/2",
		"rifattorizza questo modulo in piu file mantenendo i test verdi",
		"ottimizza questo algoritmo che e troppo lento",
		"analizza questo stack trace e trova la causa dell'errore",
		"crea una pipeline di build e test per il progetto",
		// Computing over a real file. Added 2026-08-02 after the held-out corpus caught
		// the blind spot: every seed above is software engineering, so a question that
		// aggregates a spreadsheet read as a lookup and landed on `low` or `none` —
		// summing a year of invoice totals scored `low`, and cross-checking a quote
		// against its invoice scored `none`. That is the WORST direction to be wrong
		// in, because under-reasoning an aggregate returns a
		// confident wrong number instead of a slow right one, and it is now Aura's
		// dominant traffic: document_search names the file, document_open hands it over,
		// and the answer comes from computing on it.
		"somma tutti gli importi del foglio di calcolo e dimmi il totale",
		"confronta due documenti e dimmi dove non tornano",
		"quante righe del file rispettano questa condizione",
	},
}

// classifierTierOrder fixes the build order so anchors are added per tier in a
// stable sequence (none < low < high) regardless of map iteration.
var classifierTierOrder = []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh}

// trivialGreetings is the conservative pre-filter allowlist: an exact normalized
// match routes straight to the None tier with NO embedding round-trip. Only
// unambiguous greetings/acks live here — anything else falls through to the
// embedding classifier, so the pre-filter can never mislabel a real request.
var trivialGreetings = map[string]struct{}{
	"ciao": {}, "ciao ciao": {}, "salve": {}, "buongiorno": {}, "buonasera": {},
	"buonanotte": {}, "ehi": {}, "hey": {}, "grazie": {}, "grazie mille": {},
	"ti ringrazio": {}, "ti ringrazio molto": {}, "ok": {}, "okay": {}, "perfetto": {},
	"ok perfetto": {}, "ok grazie": {}, "va bene": {}, "capito": {}, "a presto": {},
	"a dopo": {}, "thanks": {}, "thank you": {}, "a presto!": {},
}

// ReasoningClassifier maps a user turn to a reasoning tier by semantic proximity
// to the per-tier anchor centroids, using Aura's local embedding sidecar.
// It replaces the per-turn LLM "router" round-trip (the adaptive-reasoning
// latency root cause) with a single ~10ms local embed + cosine argmax. The
// centroid/cosine/margin math lives in semindex.Classifier (Centroid mode); this
// type owns only the tier policy (defs/seeds, greeting pre-filter, soft fallback).
type ReasoningClassifier struct {
	embed Embedder

	mu         sync.Mutex
	build      singleflight.Group
	generation uint64
	cls        *semindex.Classifier // per-tier centroid bank; built lazily once
	built      bool                 // false => the next Classify rebuilds the bank
}

// NewReasoningClassifier returns a classifier over the static curated anchors,
// or nil if embed is nil.
func NewReasoningClassifier(embed Embedder) *ReasoningClassifier {
	if embed == nil {
		return nil
	}
	return &ReasoningClassifier{embed: embed}
}

// Classify returns the reasoning tier for userText and true when it produced a
// usable verdict. It returns ("", false) on any embedding failure so the caller
// can fall back conservatively; the embedding path is an optimization, never a
// hard dependency. The greeting pre-filter answers without any embed call.
func (c *ReasoningClassifier) Classify(ctx context.Context, userText string) (ReasoningTier, bool) {
	if c == nil {
		return "", false
	}
	if g := normalizeForGreeting(userText); g != "" {
		if _, ok := trivialGreetings[g]; ok {
			return ReasoningTierNone, true
		}
	}
	cls, err := c.ensureAnchors(ctx)
	if err != nil {
		return "", false
	}
	vecs, err := c.embed.Embed(ctx, []string{userText})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return "", false
	}
	v := semindex.Normalize(vecs[0])
	verdict := cls.RankVecs(v)
	tier := ReasoningTier(verdict.Label)
	if !verdict.Ok || !tier.Valid() {
		return "", false
	}
	return tier, true
}

// ensureAnchors builds the per-tier centroid bank once (def + seeds, folded by
// semindex.Classifier into the per-group mean of L2-normalized embeddings). A
// build failure is NOT cached: the next call retries, so a transiently-down
// sidecar self-heals (mirror of the semindex build-failure-not-cached rule).
func (c *ReasoningClassifier) ensureAnchors(ctx context.Context) (*semindex.Classifier, error) {
	c.mu.Lock()
	if c.built && c.cls != nil {
		cls := c.cls
		c.mu.Unlock()
		return cls, nil
	}
	c.mu.Unlock()

	v, err, _ := c.build.Do("anchors", func() (any, error) {
		c.mu.Lock()
		if c.built && c.cls != nil {
			cls := c.cls
			c.mu.Unlock()
			return cls, nil
		}
		generation := c.generation
		c.mu.Unlock()

		cls, err := c.buildAnchors(ctx)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		if c.generation == generation {
			c.cls, c.built = cls, true
		}
		c.mu.Unlock()
		return cls, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*semindex.Classifier), nil
}

func (c *ReasoningClassifier) buildAnchors(ctx context.Context) (*semindex.Classifier, error) {
	cls := semindex.NewClassifier(c.embed)
	// Per-tier vectors start from the curated def+seeds (always authoritative).
	for _, t := range classifierTierOrder {
		texts := append([]string{reasoningTierDefs[t]}, reasoningTierSeeds[t]...)
		vecs, err := c.embed.Embed(ctx, texts)
		if err != nil {
			return nil, err // not cached: a retry rebuilds the whole bank
		}
		cls.AddVecs(string(t), vecs...)
	}
	return cls, nil
}

// normalizeForGreeting lowercases, trims surrounding whitespace, and strips a
// trailing run of punctuation so "Buonasera!" matches "buonasera".
func normalizeForGreeting(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimRight(s, " .!?,;:")
}
