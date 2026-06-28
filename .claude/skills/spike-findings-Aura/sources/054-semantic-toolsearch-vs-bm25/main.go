// Spike 054: semantic tool_search vs BM25, head-to-head over a LIVE-mounted
// deferred-tool corpus. Builds a real tools.Registry from the in-repo deferred
// specs + live-mounted MCP servers (agent-memory over streamable HTTP, plus the
// managed stdio mail/whatsapp servers best-effort), then ranks the documented
// mis-route queries two ways:
//
//   - BM25: the REAL production path — tools.ToolSearch.Execute (search.go/bm25.go),
//     parsed back from its "## <name>" headers. No reimplementation; this is what
//     ships today.
//   - Embedding: granite cosine over each tool's doc (two doc variants: the
//     BM25-style flattened doc for apples-to-apples, and a natural-language doc).
//
// Gold = the tool that SHOULD win each query. We report top-1, recall@3, and
// MRR@10 for each ranker. Verdict turns on whether embedding surfaces the right
// deferred tool where BM25 (English descriptions vs Italian queries) returns
// nothing. Fact/benchmark spike → forensic log + JSON summary, no UI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
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

// queryCase pairs an Italian user turn with the bare name of the tool that
// should win it. Match is suffix-aware so MCP namespacing (memory__memory_search)
// still resolves against the bare gold ("memory_search").
type queryCase struct {
	q, gold, note string
}

var cases = []queryCase{
	{"quanto costa il bitcoin adesso?", "web_search", "documented mis-route"},
	{"prezzo bitcoin", "web_search", "documented mis-route (terse)"},
	{"che tempo fa a Caraglio domani?", "web_search", "meteo"},
	{"ultime notizie su Cuneo di oggi", "web_search", "news"},
	{"trova un ristorante aperto stasera vicino a me", "web_search", "local lookup"},
	{"leggi il contenuto di questa pagina https://example.com/articolo", "web_fetch", "url read"},
	{"scarica e riassumi questo articolo dal link che ti ho mandato", "web_fetch", "url read"},
	{"ricorda che preferisco il caffe senza zucchero", "memory_add_preference", "memory write"},
	{"cosa ti avevo detto la settimana scorsa sul progetto Aura?", "memory_search", "memory recall"},
	{"manda una mail a Marco con il riepilogo del progetto", "send_email", "mail send"},
	{"controlla se ho ricevuto nuove email da Anna", "search_emails", "mail read"},
	{"scrivimi su whatsapp un messaggio quando il job e finito", "send_message", "whatsapp"},
	{"cerca nei miei documenti caricati il manuale della caldaia Siemens", "document_search", "docs"},
	{"esegui questo script python per processare il csv", "shell_exec", "shell"},
	{"manda questo file excel all'utente come allegato", "send_file", "artifact delivery"},
}

const rankK = 10 // realistic tool_search surface; rank-of-gold beyond this = "absent"

type entry struct {
	name, source string
	bm25doc      string
	natDoc       string
	vecNat       []float64
	vecBM25      []float64
}

type rankerResult struct {
	Ranker    string            `json:"ranker"`
	Top1      int               `json:"top1_hits"`
	Recall3   int               `json:"recall_at_3"`
	MRRSum    float64           `json:"-"`
	MRR       float64           `json:"mrr_at_10"`
	PerQuery  map[string]string `json:"per_query"` // query -> "rank N: winner" or "absent"
	GoldRanks map[string]int    `json:"gold_rank"` // query -> rank (0 = absent)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Ensure any tool_search spillover lands somewhere harmless.
	if os.Getenv("AURA_RUN_DIR") == "" {
		_ = os.Setenv("AURA_RUN_DIR", os.TempDir())
	}

	reg := tools.NewRegistry()
	corpus, closers := buildCorpus(ctx, reg)
	defer func() {
		for _, c := range closers {
			_ = c()
		}
	}()
	logf("CORPUS", "deferred tools = %d (%s)", len(corpus), sourceBreakdown(corpus))
	if len(corpus) < 12 {
		failf("corpus too small (%d) — is the agent-memory MCP up on :8091?", len(corpus))
	}

	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteURL(),
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()

	// Batch-embed every tool doc once, both variants. This is the one-time anchor
	// build (cached in prod like the classifier anchors).
	embedStart := time.Now()
	if err := embedCorpus(ctx, embedder, corpus); err != nil {
		failf("embed corpus: %v", err)
	}
	logf("EMBED", "embedded %d tools x2 variants in %s", len(corpus), time.Since(embedStart).Round(time.Millisecond))

	ts := &tools.ToolSearch{Registry: reg}

	bm25 := &rankerResult{Ranker: "bm25-production", PerQuery: map[string]string{}, GoldRanks: map[string]int{}}
	embNat := &rankerResult{Ranker: "embedding-natural", PerQuery: map[string]string{}, GoldRanks: map[string]int{}}
	embBM := &rankerResult{Ranker: "embedding-bm25doc", PerQuery: map[string]string{}, GoldRanks: map[string]int{}}

	var queryEmbedTotal time.Duration
	for _, c := range cases {
		// --- BM25 (real production path) ---
		bm25Ranked := bm25Execute(ctx, ts, c.q)
		score(bm25, c, bm25Ranked)

		// --- Embedding ---
		t0 := time.Now()
		qv, err := embedOne(ctx, embedder, c.q)
		queryEmbedTotal += time.Since(t0)
		if err != nil {
			failf("embed query %q: %v", c.q, err)
		}
		score(embNat, c, cosineRank(qv, corpus, false))
		score(embBM, c, cosineRank(qv, corpus, true))
		if os.Getenv("SPIKE054_DUMP") != "" {
			dumpTop(c.q, qv, corpus)
		}

		logf("Q", "%-52q gold=%s | bm25=%s embNat=%s",
			c.q, c.gold, bm25.PerQuery[c.q], embNat.PerQuery[c.q])
	}
	finalize(bm25)
	finalize(embNat)
	finalize(embBM)

	avgQ := queryEmbedTotal / time.Duration(len(cases))
	logf("LATENCY", "mean query-embed = %s (anchor build amortized once)", avgQ.Round(time.Microsecond))

	verdict := "INVALIDATED"
	// Win condition: embedding must beat BM25 on top-1 AND surface the bitcoin/
	// meteo/news web cases that BM25 misses outright.
	if embNat.Top1 > bm25.Top1 && embNat.MRR > bm25.MRR {
		verdict = "VALIDATED"
	} else if embNat.Top1 >= bm25.Top1 {
		verdict = "PARTIAL"
	}

	out := map[string]any{
		"spike":               "054-semantic-toolsearch-vs-bm25",
		"corpus_size":         len(corpus),
		"sources":             sourceBreakdown(corpus),
		"n_queries":           len(cases),
		"mean_query_embed_ms": avgQ.Seconds() * 1000,
		"rankers":             []*rankerResult{bm25, embNat, embBM},
		"verdict":             verdict,
	}
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
	logf("SUMMARY", "verdict=%s | top1 bm25=%d/%d embNat=%d/%d embBM=%d/%d | MRR bm25=%.3f embNat=%.3f embBM=%.3f",
		verdict, bm25.Top1, len(cases), embNat.Top1, len(cases), embBM.Top1, len(cases),
		bm25.MRR, embNat.MRR, embBM.MRR)
	if verdict == "INVALIDATED" {
		os.Exit(1)
	}
}

// ---- corpus ----

func buildCorpus(ctx context.Context, reg *tools.Registry) ([]entry, []func() error) {
	source := map[string]string{}
	register := func(src string, ts ...tools.Tool) {
		for _, t := range ts {
			reg.Register(t)
			source[t.Spec().Name] = src
		}
	}
	register("in-repo",
		&tools.WebSearch{}, &tools.WebFetch{}, &tools.ShellExec{}, &tools.SendFile{},
		&tools.SwarmSpawn{}, &tools.DocumentSearch{}, &tools.ShellPoll{}, &tools.ShellKill{},
	)

	var closers []func() error

	// agent-memory MCP over streamable HTTP (required — the bulk of the corpus).
	memSrv := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   memEndpoint(),
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	if closer, names, err := mcptools.MountManagedServer(ctx, reg, "memory", memSrv); err != nil {
		logf("MCP", "agent-memory mount FAILED: %v", err)
	} else {
		closers = append(closers, closer)
		for _, n := range names {
			source[n] = "mcp:memory"
		}
		logf("MCP", "agent-memory mounted %d tools", len(names))
	}

	// Managed stdio servers (mail/whatsapp) — best-effort; they prove the live
	// dynamic corpus but their subprocess spawn can be flaky on a dev box.
	if cfg, err := config.Load(); err == nil {
		for name, sc := range cfg.MCPServers {
			mctx, mcancel := context.WithTimeout(ctx, 25*time.Second)
			closer, names, err := mcptools.MountServer(mctx, reg, name, sc)
			mcancel()
			if err != nil {
				logf("MCP", "%s mount skipped: %v", name, err)
				continue
			}
			closers = append(closers, closer)
			for _, n := range names {
				source[n] = "mcp:" + name
			}
			logf("MCP", "%s mounted %d tools", name, len(names))
		}
	} else {
		logf("MCP", "config.Load failed (stdio servers skipped): %v", err)
	}

	var corpus []entry
	for _, t := range reg.All() {
		s := t.Spec()
		if !s.Deferred {
			continue
		}
		src := source[s.Name]
		if src == "" {
			src = "unknown"
		}
		corpus = append(corpus, entry{
			name:    s.Name,
			source:  src,
			bm25doc: bm25Document(s),
			natDoc:  naturalDocument(s),
		})
	}
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })
	return corpus, closers
}

// bm25Document mirrors tools.searchDocument (unexported): name + name-with-spaces
// + description + every nested schema description and property key. This is the
// exact text the production BM25 indexes, so the embedding-bm25doc variant is a
// fair apples-to-apples comparison of RANKER, holding the document constant.
func bm25Document(s tools.Spec) string {
	var parts []string
	push := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	push(s.Name)
	push(strings.ReplaceAll(s.Name, "_", " "))
	push(s.Description)
	var node schemaNode
	if json.Unmarshal(s.Parameters, &node) == nil {
		appendSchema(node, push)
	}
	return strings.Join(parts, " ")
}

// naturalDocument is the production-shaped embedding input: a readable phrase, no
// schema-key noise. This is what a real semantic index would store.
func naturalDocument(s tools.Spec) string {
	name := strings.ReplaceAll(s.Name, "_", " ")
	desc := s.Description
	if desc == "" {
		desc = s.Summary
	}
	return strings.TrimSpace(name + ": " + s.Summary + ". " + desc)
}

type schemaNode struct {
	Description string                `json:"description"`
	Properties  map[string]schemaNode `json:"properties"`
	Items       *schemaNode           `json:"items"`
	AnyOf       []schemaNode          `json:"anyOf"`
}

func appendSchema(n schemaNode, push func(string)) {
	push(n.Description)
	keys := make([]string, 0, len(n.Properties))
	for k := range n.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		push(k)
		appendSchema(n.Properties[k], push)
	}
	if n.Items != nil {
		appendSchema(*n.Items, push)
	}
	for _, v := range n.AnyOf {
		appendSchema(v, push)
	}
}

// ---- embedding ----

func embedCorpus(ctx context.Context, e *documents.EmbeddingClient, corpus []entry) error {
	nat := make([]string, len(corpus))
	bm := make([]string, len(corpus))
	for i, c := range corpus {
		nat[i] = c.natDoc
		bm[i] = c.bm25doc
	}
	natVecs, err := e.Embed(ctx, nat)
	if err != nil {
		return fmt.Errorf("natural: %w", err)
	}
	bmVecs, err := e.Embed(ctx, bm)
	if err != nil {
		return fmt.Errorf("bm25doc: %w", err)
	}
	for i := range corpus {
		corpus[i].vecNat = l2(natVecs[i])
		corpus[i].vecBM25 = l2(bmVecs[i])
	}
	return nil
}

func embedOne(ctx context.Context, e *documents.EmbeddingClient, text string) ([]float64, error) {
	vs, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return l2(vs[0]), nil
}

type scoredName struct {
	name string
	s    float64
}

func cosineScored(qv []float64, corpus []entry, useBM25Doc bool) []scoredName {
	scored := make([]scoredName, len(corpus))
	for i, c := range corpus {
		v := c.vecNat
		if useBM25Doc {
			v = c.vecBM25
		}
		scored[i] = scoredName{c.name, dot(qv, v)}
	}
	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].s != scored[b].s {
			return scored[a].s > scored[b].s
		}
		return scored[a].name < scored[b].name
	})
	return scored
}

// cosineRank returns tool names sorted by descending cosine to the query vector.
func cosineRank(qv []float64, corpus []entry, useBM25Doc bool) []string {
	scored := cosineScored(qv, corpus, useBM25Doc)
	out := make([]string, len(scored))
	for i, s := range scored {
		out[i] = s.name
	}
	return out
}

func dumpTop(query string, qv []float64, corpus []entry) {
	top := cosineScored(qv, corpus, false)
	parts := make([]string, 0, 5)
	for i := 0; i < 5 && i < len(top); i++ {
		parts = append(parts, fmt.Sprintf("%s=%.3f", top[i].name, top[i].s))
	}
	logf("TOP5", "%-44q | %s", query, strings.Join(parts, "  "))
}

// ---- BM25 via the real production tool_search ----

var headerRe = regexp.MustCompile(`(?m)^## (\S+)`)

func bm25Execute(ctx context.Context, ts *tools.ToolSearch, query string) []string {
	// ToolSearch.Execute builds its result through tools.NewResult, which REQUIRES
	// a tool-call context (session/callID/runDir/previewCap). A large previewCap
	// keeps the "## name" headers inline (no sidecar spill) so we can parse the
	// real production ranking back out.
	ctx = tools.WithToolCallContext(ctx, "spike-054", "tc-bm25", os.TempDir(), 1<<20)
	raw, _ := json.Marshal(map[string]any{"query": query, "max_results": rankK})
	res, err := ts.Execute(ctx, raw)
	if err != nil {
		logf("BM25", "execute error for %q: %v", query, err)
		return nil
	}
	body := res.Preview
	if res.FullPath != "" {
		if b, rerr := os.ReadFile(res.FullPath); rerr == nil {
			body = string(b)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(body), "no matching tools") {
		return nil
	}
	var names []string
	for _, m := range headerRe.FindAllStringSubmatch(body, -1) {
		names = append(names, m[1])
	}
	return names
}

// ---- scoring ----

func goldMatch(name, gold string) bool {
	return name == gold || strings.HasSuffix(name, "__"+gold) || strings.HasSuffix(name, gold)
}

func score(r *rankerResult, c queryCase, ranked []string) {
	rank := 0
	for i, n := range ranked {
		if i >= rankK {
			break
		}
		if goldMatch(n, c.gold) {
			rank = i + 1
			break
		}
	}
	r.GoldRanks[c.q] = rank
	if rank == 0 {
		r.PerQuery[c.q] = "absent"
		return
	}
	winner := "?"
	if len(ranked) > 0 {
		winner = ranked[0]
	}
	r.PerQuery[c.q] = fmt.Sprintf("rank %d (top=%s)", rank, winner)
	if rank == 1 {
		r.Top1++
	}
	if rank <= 3 {
		r.Recall3++
	}
	r.MRRSum += 1.0 / float64(rank)
}

func finalize(r *rankerResult) { r.MRR = r.MRRSum / float64(len(cases)) }

// ---- helpers ----

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

func sourceBreakdown(corpus []entry) string {
	m := map[string]int{}
	for _, c := range corpus {
		m[c.source]++
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
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
