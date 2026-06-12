package prompt

import (
	"context"
	"math"
	"strings"
	"sync"
)

// Embedder is the narrow embedding seam the reasoning classifier needs. It
// matches documents.EmbeddingClient.Embed exactly so the production granite
// sidecar client satisfies it without an adapter. Defined here (the consumer)
// to keep agent/prompt free of an embedding-package import.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// LabeledVec is one stored reasoning example: an L2-normalized embedding plus
// the tier an oracle (the LLM router) assigned to it.
type LabeledVec struct {
	Tier ReasoningTier
	Vec  []float64
}

// ExampleStore loads oracle-labeled examples that the classifier folds into its
// per-tier centroids (the self-improvement substrate, spike 053). nil store =>
// the classifier uses the curated seeds alone. Backed by Neo4j in production.
type ExampleStore interface {
	LoadExamples(ctx context.Context) ([]LabeledVec, error)
}

// Learner observes every embedding-based classification so a background worker
// can oracle-label the uncertain ones (low margin) and grow the example store
// (self-improvement, spike 053). Observe MUST be non-blocking — it runs on the
// turn's hot path. nil learner => no observation.
type Learner interface {
	Observe(text string, vec []float64, margin float64)
}

// Reasoning-tier anchors. The tier definitions are the production router's own
// wording; the seeds are the few-shot examples validated in spike 052 (variant
// B: 90% accuracy / 92% none-vs-rest over a 60-prompt held-out set, ~10ms CPU).
// Anchors generalize semantically — these phrases are NOT an enumeration of all
// inputs, they are prototypes the embedding model interpolates between.
var reasoningTierDefs = map[ReasoningTier]string{
	ReasoningTierNone: "saluto, ringraziamento, chiacchiera, fatto semplice e stabile, o trasformazione breve e diretta",
	ReasoningTierLow:  "informazione corrente dal web: meteo, notizie, prezzi, orari, ricerche e lookup, oppure piccolo uso di strumenti",
	ReasoningTierHigh: "scrittura di codice, debug, progettazione, dimostrazioni, scraping, analisi in piu passaggi",
}

var reasoningTierSeeds = map[ReasoningTier][]string{
	ReasoningTierNone: {
		"ciao",
		"grazie mille",
		"come ti chiami?",
		"ripeti per favore",
		"traduci 'gatto' in inglese",
	},
	ReasoningTierLow: {
		"che tempo fa a Torino domani?",
		"cerca le ultime notizie su Cuneo",
		"quanto costa il bitcoin adesso?",
		"a che ora chiude la farmacia?",
		"trova un ristorante aperto stasera vicino a me",
	},
	ReasoningTierHigh: {
		"scrivi uno script python per fare scraping di un sito con gestione errori",
		"aiutami a debuggare questa funzione che va in segfault",
		"progetta lo schema di un database per un e-commerce",
		"dimostra per induzione che la somma dei primi n numeri e n(n+1)/2",
		"rifattorizza questo modulo in piu file mantenendo i test verdi",
	},
}

// classifierTierOrder fixes the iteration order so argmax ties break
// deterministically (none < low < high) regardless of map iteration.
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
// to the per-tier anchor centroids, using Aura's local granite embedding sidecar.
// It replaces the per-turn LLM "router" round-trip (the adaptive-reasoning
// latency root cause) with a single ~10ms local embed + cosine argmax.
type ReasoningClassifier struct {
	embed   Embedder
	store   ExampleStore // optional: oracle-labeled examples folded into centroids
	learner Learner      // optional: observes classifications for self-improvement

	mu      sync.Mutex
	anchors map[ReasoningTier][]float64 // L2-normalized centroids; built lazily once
}

// SetLearner attaches a self-improvement learner (safe to call once at wiring
// time, before concurrent Classify). nil is a no-op.
func (c *ReasoningClassifier) SetLearner(l Learner) {
	if c != nil {
		c.learner = l
	}
}

// NewReasoningClassifier returns a classifier over the given embedder (and an
// optional ExampleStore for self-improvement), or nil if embed is nil (callers
// treat nil as "no classifier wired" and fall back to the LLM router).
func NewReasoningClassifier(embed Embedder, store ExampleStore) *ReasoningClassifier {
	if embed == nil {
		return nil
	}
	return &ReasoningClassifier{embed: embed, store: store}
}

// Refresh drops the cached centroids so the next Classify rebuilds them,
// re-folding any newly stored examples. Safe to call concurrently.
func (c *ReasoningClassifier) Refresh() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.anchors = nil
	c.mu.Unlock()
}

// Classify returns the reasoning tier for userText and true when it produced a
// usable verdict. It returns ("", false) on any embedding failure so the caller
// can fall back to the LLM router — the embedding path is an optimization, never
// a hard dependency. The greeting pre-filter answers without any embed call.
func (c *ReasoningClassifier) Classify(ctx context.Context, userText string) (ReasoningTier, bool) {
	if c == nil {
		return "", false
	}
	if normalizeForGreeting(userText) != "" {
		if _, ok := trivialGreetings[normalizeForGreeting(userText)]; ok {
			return ReasoningTierNone, true
		}
	}
	anchors, err := c.ensureAnchors(ctx)
	if err != nil {
		return "", false
	}
	vecs, err := c.embed.Embed(ctx, []string{userText})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return "", false
	}
	q := l2normalize(vecs[0])
	best, bestScore, second := ReasoningTier(""), -2.0, -2.0
	for _, t := range classifierTierOrder {
		s := cosineSimilarity(q, anchors[t])
		if s > bestScore {
			second, best, bestScore = bestScore, t, s
		} else if s > second {
			second = s
		}
	}
	if !best.Valid() {
		return "", false
	}
	if c.learner != nil {
		c.learner.Observe(userText, q, bestScore-second)
	}
	return best, true
}

// ensureAnchors builds the per-tier centroids once (def + seeds, mean of
// L2-normalized embeddings). A build failure is NOT cached: the next call
// retries, so a transiently-down sidecar self-heals.
func (c *ReasoningClassifier) ensureAnchors(ctx context.Context) (map[ReasoningTier][]float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.anchors != nil {
		return c.anchors, nil
	}
	// Per-tier vectors start from the curated def+seeds (always authoritative).
	perTier := make(map[ReasoningTier][][]float64, len(classifierTierOrder))
	for _, t := range classifierTierOrder {
		texts := append([]string{reasoningTierDefs[t]}, reasoningTierSeeds[t]...)
		vecs, err := c.embed.Embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		perTier[t] = vecs
	}
	// Fold in oracle-labeled examples (self-improvement). A store error is
	// non-fatal: fall back to seed-only centroids rather than failing the turn.
	if c.store != nil {
		if examples, err := c.store.LoadExamples(ctx); err == nil {
			for _, ex := range examples {
				if ex.Tier.Valid() && len(ex.Vec) > 0 {
					perTier[ex.Tier] = append(perTier[ex.Tier], l2normalize(ex.Vec))
				}
			}
		}
	}
	built := make(map[ReasoningTier][]float64, len(classifierTierOrder))
	for _, t := range classifierTierOrder {
		built[t] = meanNormalize(perTier[t])
	}
	c.anchors = built
	return c.anchors, nil
}

// normalizeForGreeting lowercases, trims surrounding whitespace, and strips a
// trailing run of punctuation so "Buonasera!" matches "buonasera".
func normalizeForGreeting(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimRight(s, " .!?,;:")
}

func l2normalize(v []float64) []float64 {
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

// cosineSimilarity assumes both inputs are already L2-normalized (anchors and
// the query both are), so it is a plain dot product.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return -2.0
	}
	var d float64
	for i := range a {
		d += a[i] * b[i]
	}
	return d
}

// meanNormalize averages the vectors then L2-normalizes the centroid.
func meanNormalize(vecs [][]float64) []float64 {
	if len(vecs) == 0 {
		return nil
	}
	acc := make([]float64, len(vecs[0]))
	for _, v := range vecs {
		for i := range acc {
			if i < len(v) {
				acc[i] += v[i]
			}
		}
	}
	for i := range acc {
		acc[i] /= float64(len(vecs))
	}
	return l2normalize(acc)
}
