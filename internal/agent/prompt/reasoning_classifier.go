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
// canonical seam) so embeddings.Client satisfies both with no adapter
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
// recurring intent shape (stable facts + arithmetic + transforms for none; the
// changes-over-time lookups and small direct tool use for low; the
// code/proof/design/analysis spread for high), because a turn takes the tier of
// its nearest exemplars and an intent with no exemplar has no near neighbour.
//
// The tool-use, memory and everyday-computation exemplars were added 2026-10-06
// (prd.md §6): every `low` seed was a web lookup, so the reminders and messages of
// the lab VM's real traffic sat far from every tier and went to `high`, reasoning
// 10-21 s to schedule one task.
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
		"ricordati che mia figlia è allergica alle arachidi",
		"come si chiama il mio medico di base?",
		"grazie mille, perfetto così",
		"ok ricevuto, ottimo",
		"segnati che il mio compleanno è il 14 marzo",
		"ti ricordi che targa ha la mia macchina?",
		"ma tu che modello sei?",
		"parli anche inglese?",
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
		"ricordami tra 10 minuti di togliere la pasta dal fuoco",
		"domani alle 8 ricordami di chiamare il dentista",
		"ogni lunedì alle 9 ricordami di mettere fuori il bidone della carta",
		"mandami un messaggio di prova su Telegram",
		"scrivi a Marco su WhatsApp che arrivo tra un quarto d'ora",
		"manda una mail a Giulia: la riunione è spostata a giovedì",
		"cosa ho in agenda venerdì pomeriggio?",
		"aggiungi al calendario cena da mia madre sabato alle 20",
		"quali promemoria ho programmato?",
		"cancella il promemoria della palestra",
		"metti un timer di 25 minuti",
		"mandami su Telegram il pdf della bolletta che ti ho caricato ieri",
		"fai un test, scrivimi ciao su WhatsApp",
		"alle 18 manda un whatsapp a Sara con scritto buon compleanno!",
		"ogni mattina alle 7:30 mandami il meteo su Telegram",
		"giovedì mattina sono libero o ho già qualcosa?",
		"sposta l'appuntamento dal commercialista a martedì alle 15",
		"disattiva il riepilogo mattutino delle notizie",
		"prova a mandarmi una notifica, voglio vedere se arriva sul telefono",
		"il primo di ogni mese ricordami di pagare l'affitto",
		"manda per email il contratto firmato all'avvocato Rossi",
		"tra un'ora avvisami di passare in lavanderia",
		"facciamo un test dei promemoria: avvisami tra 2 minuti",
		"rispondi a Chiara su Telegram che per domani va bene",
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
		"nel foglio delle spese che ti ho caricato quanto ho speso in ristoranti a settembre?",
		"confronta i due preventivi del tetto voce per voce e dimmi quale conviene davvero",
		"nell'excel delle bollette di quanto è aumentato in media il gas rispetto all'anno scorso?",
		"nel file clienti trova chi non ordina da più di sei mesi e raggruppali per città",
		"organizzami 5 giorni in Puglia con 800 euro di budget, tappe e spostamenti compresi",
		"controlla se le fatture del pdf tornano con i movimenti della banca e segnami le differenze",
		"ho tre turni diversi e due figli da accompagnare a scuola e sport, fammi un piano settimanale senza sovrapposizioni",
		"dal registro presenze calcola le ore di straordinario di ogni dipendente ad agosto",
	},
}

// tierNeighbours is how many of a tier's exemplars nearest to the turn score that tier.
// Three, as measured on 2026-10-06 (prd.md §6).
const tierNeighbours = 3

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

// ReasoningClassifier maps a user turn to the reasoning tier of its nearest anchors,
// using Aura's local embedding sidecar. It replaces the per-turn LLM "router"
// round-trip (the adaptive-reasoning latency root cause) with a single ~10ms local
// embed. The nearest-exemplar math lives in semindex.Classifier; this type owns only
// the tier policy (defs/seeds, greeting pre-filter, soft fallback).
//
// A centroid used to score each tier, and lost every turn whose intent the tier held
// as one exemplar among many. Scoring the 3 nearest exemplars instead, with the
// tool-use seeds above, moved the lab VM's real traffic from 8 to 14 of 15
// (prd.md §6, measured 2026-10-06).
type ReasoningClassifier struct {
	embed Embedder

	mu    sync.Mutex
	build singleflight.Group
	cls   *semindex.Classifier // per-tier exemplar bank; built lazily once
	built bool                 // false => the next Classify rebuilds the bank
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
	verdict := cls.RankNearest(vecs[0], tierNeighbours)
	tier := ReasoningTier(verdict.Label)
	if !verdict.Ok || !tier.Valid() {
		return "", false
	}
	return tier, true
}

// ensureAnchors builds the per-tier exemplar bank once (def + seeds). A build failure
// is NOT cached: the next call retries, so a transiently-down sidecar self-heals
// (mirror of the semindex build-failure-not-cached rule).
//
// The publish is unconditional, and that is the whole invalidation story: the anchors are
// static, nothing ever marks the bank stale, and singleflight already serialises builds on
// this key, so two of them can never race to publish. A generation counter guarding this
// write used to sit here — declared, snapshotted and compared, but assigned NOWHERE, so the
// comparison was always true and the guard always taken. It was removed rather than
// completed: making it live would have meant inventing an invalidation trigger for a bank
// that has nothing to invalidate.
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
		c.mu.Unlock()

		cls, err := c.buildAnchors(ctx)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.cls, c.built = cls, true
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
