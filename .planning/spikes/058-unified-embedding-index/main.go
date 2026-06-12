// Spike 058 (capstone): can ONE reusable, industrial embedding-index core serve
// BOTH the reasoning-tier classifier (spikes 052/053) AND the tool-search ranker
// (spikes 054-057) — and, by extension, any future embed-classify-or-retrieve task
// on Aura? Operator directive: "create reusable code to fuse all tool, reasoning
// and future improvement on aura, the industrial way." Anthropic's own tool-search
// API supports exactly this via a custom embedding search returning tool_reference
// blocks (platform.claude.com tool-search-tool + embeddings cookbook).
//
// The thesis: the shared shape already lives in internal/agent/prompt/
// reasoning_classifier.go — an Embedder seam, a labeled vector bank, cosine + a
// top-2 margin, an incremental cache, and active-learning hooks. This spike
// extracts that into a generic `Index` and runs the SAME core two ways:
//
//   - CENTROID mode  => reasoning classifier: 3 tiers, each a group of def+seeds;
//     argmax over per-group centroids (reproduces 052's behavior).
//   - PER-ITEM mode  => tool ranker: N tools, each its own item; top-K by cosine
//     (reproduces 054's behavior, returns Anthropic-style tool_reference names).
//
// Both call the identical Add / Rank / margin code over the live granite sidecar.
// VALIDATED = one core, both jobs, both correct. Fact spike → forensic log.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/mcp"
)

// ============================================================================
// THE REUSABLE CORE (the proposed industrial shape — ~80 LOC, stdlib + 1 seam)
// ============================================================================

// Embedder is the one external dependency — satisfied by documents.EmbeddingClient
// AND by prompt.Embedder today (identical signature). No new dep, no new sidecar.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Mode selects how Rank scores: PerItem ranks every item (tool search); Centroid
// ranks the mean vector of each group (tier classification).
type Mode int

const (
	PerItem Mode = iota
	Centroid
)

// Item is one labeled document: Group is the label (tier name, or the tool's own
// name); Doc is the text to embed.
type Item struct {
	Group, Doc string
	vec        []float64
}

// Scored is one ranked result. Margin (top1 - top2) is the ambiguity signal the
// active-learning gate keys on (spikes 052/053) — exposed uniformly here.
type Scored struct {
	Label string
	Score float64
}

// Index is the reusable embedding-backed labeled retriever/classifier. The reasoning
// classifier, the tool ranker, and any future router instantiate THIS, differing only
// in Mode and the items they Add. Learner/Store hooks (omitted here for brevity) attach
// the spike-053 self-improvement loop identically for every consumer.
type Index struct {
	embedder Embedder
	mode     Mode
	items    []Item
	groups   []string
	groupVec map[string][]float64
}

func NewIndex(e Embedder, mode Mode) *Index {
	return &Index{embedder: e, mode: mode, groupVec: map[string][]float64{}}
}

// Add embeds and appends items, then (Centroid mode) rebuilds group centroids. This
// is the incremental, invalidate-on-change seam: a runtime MCP mount calls Add with
// only the new tools (~7ms/tool, spike 055) — never a full rebuild.
func (ix *Index) Add(ctx context.Context, items []Item) error {
	docs := make([]string, len(items))
	for i, it := range items {
		docs[i] = it.Doc
	}
	vecs, err := ix.embedder.Embed(ctx, docs)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].vec = l2(vecs[i])
		ix.items = append(ix.items, items[i])
	}
	if ix.mode == Centroid {
		ix.rebuildCentroids()
	}
	return nil
}

func (ix *Index) rebuildCentroids() {
	byGroup := map[string][][]float64{}
	for _, it := range ix.items {
		byGroup[it.Group] = append(byGroup[it.Group], it.vec)
	}
	ix.groups = ix.groups[:0]
	for g, vs := range byGroup {
		ix.groupVec[g] = meanNormalize(vs)
		ix.groups = append(ix.groups, g)
	}
	sort.Strings(ix.groups)
}

// Rank returns results sorted by descending cosine to the query, plus the top-2
// margin. PerItem ranks items; Centroid ranks group centroids. Identical for every
// consumer — only the candidate set differs.
func (ix *Index) Rank(ctx context.Context, query string) ([]Scored, float64, error) {
	qvs, err := ix.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, 0, err
	}
	q := l2(qvs[0])
	var out []Scored
	if ix.mode == Centroid {
		for _, g := range ix.groups {
			out = append(out, Scored{g, dot(q, ix.groupVec[g])})
		}
	} else {
		for _, it := range ix.items {
			out = append(out, Scored{it.Group, dot(q, it.vec)})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].Label < out[b].Label
	})
	margin := 0.0
	if len(out) >= 2 {
		margin = out[0].Score - out[1].Score
	}
	return out, margin, nil
}

// ============================================================================
// USE-CASE PROOFS — both run the SAME Index core
// ============================================================================

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if os.Getenv("AURA_RUN_DIR") == "" {
		_ = os.Setenv("AURA_RUN_DIR", os.TempDir())
	}
	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteURL(),
		Client:     &http.Client{Timeout: 60 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()

	clsOK, clsN := proveClassifier(ctx, embedder)
	rankOK, rankN, closers := proveToolRanker(ctx, embedder)
	for _, c := range closers {
		_ = c()
	}

	verdict := "INVALIDATED"
	if clsOK == clsN && rankOK >= rankN*7/10 { // classifier exact; ranker matches 054 (~8/11)
		verdict = "VALIDATED"
	}
	out := map[string]any{
		"spike":              "058-unified-embedding-index",
		"core_loc_estimate":  "~90 (Index) reused by both",
		"classifier_correct": fmt.Sprintf("%d/%d", clsOK, clsN),
		"toolrank_top1":      fmt.Sprintf("%d/%d", rankOK, rankN),
		"verdict":            verdict,
	}
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
	logf("SUMMARY", "verdict=%s | ONE Index core | classifier %d/%d (Centroid) | tool-rank %d/%d (PerItem)",
		verdict, clsOK, clsN, rankOK, rankN)
	if verdict == "INVALIDATED" {
		os.Exit(1)
	}
}

// proveClassifier: reasoning-tier classification via Centroid mode (reproduces 052).
func proveClassifier(ctx context.Context, e Embedder) (correct, total int) {
	ix := NewIndex(e, Centroid)
	tiers := map[string][]string{
		"none": {"saluto, ringraziamento, chiacchiera, fatto semplice e stabile", "ciao", "grazie mille", "come ti chiami?"},
		"low":  {"informazione corrente dal web: meteo, notizie, prezzi, ricerche", "che tempo fa a Torino domani?", "ultime notizie su Cuneo", "quanto costa il bitcoin?"},
		"high": {"scrittura di codice, debug, progettazione, dimostrazioni, scraping", "scrivi uno script python di scraping", "debugga questo segfault", "progetta lo schema di un db"},
	}
	var items []Item
	for tier, docs := range tiers {
		for _, d := range docs {
			items = append(items, Item{Group: tier, Doc: d})
		}
	}
	if err := ix.Add(ctx, items); err != nil {
		failf("classifier add: %v", err)
	}
	probes := []struct{ q, want string }{
		{"buongiorno come stai?", "none"},
		{"traducimi gatto in inglese", "none"},
		{"che tempo fa a Caraglio?", "low"},
		{"prezzo attuale di ethereum", "low"},
		{"scrivi una funzione go che inverte una stringa", "high"},
		{"aiutami a debuggare questo nil pointer", "high"},
	}
	for _, p := range probes {
		ranked, margin, err := ix.Rank(ctx, p.q)
		if err != nil {
			failf("classify: %v", err)
		}
		got := ranked[0].Label
		ok := got == p.want
		if ok {
			correct++
		}
		total++
		logf("CLASSIFY", "%-42q -> %-5s want=%-5s margin=%.3f %s", p.q, got, p.want, margin, tick(ok))
	}
	return correct, total
}

// proveToolRanker: tool search via PerItem mode over the live corpus (reproduces 054).
func proveToolRanker(ctx context.Context, e Embedder) (correct, total int, closers []func() error) {
	ix := NewIndex(e, PerItem)
	reg := tools.NewRegistry()
	corpus, cl := buildLiveCorpus(ctx, reg)
	closers = cl
	if err := ix.Add(ctx, corpus); err != nil {
		failf("toolrank add: %v", err)
	}
	logf("TOOLRANK", "corpus = %d tools (PerItem)", len(corpus))
	probes := []struct{ q, gold string }{
		{"che tempo fa a Caraglio domani?", "web_search"},
		{"ultime notizie su Cuneo di oggi", "web_search"},
		{"trova un ristorante aperto stasera", "web_search"},
		{"scarica e riassumi questo articolo dal link", "web_fetch"},
		{"ricorda che preferisco il caffe senza zucchero", "memory_add_preference"},
		{"scrivimi su whatsapp quando il job e finito", "send_message"},
		{"manda questo file excel all'utente", "send_file"},
		{"esegui questo script python", "shell_exec"},
		{"manda una mail a Marco col riepilogo", "send_email"},
		{"cerca nei miei documenti il manuale Siemens", "document_search"},
		{"leggi questa pagina https://example.com", "web_fetch"},
	}
	for _, p := range probes {
		ranked, margin, err := ix.Rank(ctx, p.q)
		if err != nil {
			failf("rank: %v", err)
		}
		got := ranked[0].Label
		ok := got == p.gold || strings.HasSuffix(got, "__"+p.gold) || strings.HasSuffix(got, p.gold)
		if ok {
			correct++
		}
		total++
		logf("TOOLRANK", "%-42q -> %-26s gold=%-22s margin=%.3f %s", p.q, got, p.gold, margin, tick(ok))
	}
	return correct, total, closers
}

func buildLiveCorpus(ctx context.Context, reg *tools.Registry) ([]Item, []func() error) {
	for _, t := range []tools.Tool{
		&tools.WebSearch{}, &tools.WebFetch{}, &tools.ShellExec{}, &tools.SendFile{},
		&tools.SwarmSpawn{}, &tools.DocumentSearch{}, &tools.ShellPoll{}, &tools.ShellKill{},
	} {
		reg.Register(t)
	}
	var closers []func() error
	memSrv := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: memEndpoint(), Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}}
	if closer, _, err := mcptools.MountManagedServer(ctx, reg, "memory", memSrv); err == nil {
		closers = append(closers, closer)
	}
	if cfg, err := config.Load(); err == nil {
		for name, sc := range cfg.MCPServers {
			mctx, mcancel := context.WithTimeout(ctx, 25*time.Second)
			closer, _, err := mcptools.MountServer(mctx, reg, name, sc)
			mcancel()
			if err == nil {
				closers = append(closers, closer)
			}
		}
	}
	var corpus []Item
	for _, t := range reg.All() {
		s := t.Spec()
		if !s.Deferred {
			continue
		}
		doc := strings.TrimSpace(strings.ReplaceAll(s.Name, "_", " ") + ": " + s.Summary + ". " + s.Description)
		corpus = append(corpus, Item{Group: s.Name, Doc: doc})
	}
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].Group < corpus[j].Group })
	return corpus, closers
}

// ---- shared math (the only numeric primitives the core needs) ----

func l2(v []float64) []float64 {
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

func dot(a, b []float64) float64 {
	if len(a) != len(b) {
		return -2
	}
	var d float64
	for i := range a {
		d += a[i] * b[i]
	}
	return d
}

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
	return l2(acc)
}

func tick(ok bool) string {
	if ok {
		return "OK"
	}
	return "MISS"
}

func graniteURL() string {
	if v := strings.TrimSpace(os.Getenv("AURA_EMBED_BASE_URL")); v != "" {
		return v
	}
	return "http://127.0.0.1:8081"
}

func memEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if port == "" {
		port = "8091"
	}
	return "http://127.0.0.1:" + port + "/mcp/"
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}

func failf(format string, args ...any) {
	logf("ERROR", format, args...)
	os.Exit(1)
}
