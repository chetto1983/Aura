// Spike 057: does a cheap, reliable TEACHER exist for the step-3 tool-selection
// self-improvement loop? Spike 053 already validated the LEARNER (async
// centroid-refresh of oracle-labeled examples). The only unproven piece of step 3
// is the oracle: given a turn, what tool SHOULD have been used, and can we detect
// inefficient turns without an LLM round-trip on the hot path?
//
// Candidate (the elegant convergence): the step-2 semantic ranker (spikes 054/056)
// applied to the ORIGINAL user request IS the teacher — free, local, deterministic.
// idea-2 feeds idea-3. This spike measures:
//
//  1. DETECTION — can cheap free signals (shell/fs fallback; used-tool != ranker
//     top-1; low ranker margin) flag the mis-routed turns? precision/recall vs the
//     gold "was this actually a mis-route".
//  2. LABELING — when flagged, is the ranker's top-1 the right tool? (teacher
//     accuracy). And critically the BOOTSTRAPPING CEILING: the free ranker can
//     only teach cases where IT is right; it cannot self-correct its own
//     systematic errors (the verbose-bitcoin class from spike 054) — those need a
//     stronger periodic oracle (the spike-053 Gemma/DeepSeek path), designed here.
//
// Fact spike → forensic log. Free + deterministic (no paid LLM call: see
// feedback_no_unsolicited_paid_runs_batch_calls).
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

// trace is one observed agent turn: the user request, the tool the model ACTUALLY
// used (the trace outcome), and the gold tool that SHOULD have handled it.
// gold==used => an efficient turn; gold!=used => a real mis-route worth learning.
type trace struct {
	q, used, gold string
}

// fallbackTools are the generic "the model improvised" tools — using one of these
// when a dedicated tool existed is the classic inefficiency signal (the bitcoin
// skill->fs_glob->shell_exec chain).
var fallbackTools = map[string]bool{
	"shell_exec": true, "shell_poll": true, "shell_kill": true,
	"fs_read": true, "fs_glob": true, "fs_grep": true, "skill": true,
	"text_response": true,
}

var traces = []trace{
	// documented + plausible mis-routes (gold != used)
	{"quanto costa il bitcoin adesso?", "shell_exec", "web_search"},                              // THE documented chain
	{"che tempo fa a Caraglio domani?", "shell_exec", "web_search"},                              // improvised instead of web_search
	{"ultime notizie su Cuneo di oggi", "text_response", "web_search"},                           // answered from memory, no search
	{"ricorda che preferisco il caffe senza zucchero", "text_response", "memory_add_preference"}, // ack'd, didn't store
	{"cerca nei miei documenti il manuale della caldaia", "shell_exec", "document_search"},       // grepped fs
	{"scarica e riassumi questo articolo dal link", "fs_read", "web_fetch"},                      // tried local read
	{"manda una mail a Marco con il riepilogo", "text_response", "send_email"},                   // wrote text, didn't send
	// efficient turns (gold == used) — the detector must NOT flag these
	{"esegui questo script python per processare il csv", "shell_exec", "shell_exec"},
	{"manda questo file excel all'utente come allegato", "send_file", "send_file"},
	{"scrivimi su whatsapp un messaggio quando il job e finito", "send_message", "send_message"},
	{"fai una web search per le previsioni meteo", "web_search", "web_search"},
	{"trova un ristorante aperto stasera vicino a me", "web_search", "web_search"},
}

const marginFloor = 0.02 // spike 052/053-style low-margin gate (cosine band is narrow)

type confusion struct {
	TP, FP, FN, TN int
}

func (c confusion) precision() float64 {
	if c.TP+c.FP == 0 {
		return 0
	}
	return float64(c.TP) / float64(c.TP+c.FP)
}
func (c confusion) recall() float64 {
	if c.TP+c.FN == 0 {
		return 0
	}
	return float64(c.TP) / float64(c.TP+c.FN)
}

type entry struct {
	name, doc string
	vec       []float64
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if os.Getenv("AURA_RUN_DIR") == "" {
		_ = os.Setenv("AURA_RUN_DIR", os.TempDir())
	}

	corpus, closers := buildCorpus(ctx)
	defer func() {
		for _, c := range closers {
			_ = c()
		}
	}()
	logf("CORPUS", "deferred tools = %d", len(corpus))
	if len(corpus) < 40 {
		failf("corpus too small (%d)", len(corpus))
	}

	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteURL(),
		Client:     &http.Client{Timeout: 60 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()
	if err := embedInto(ctx, embedder, corpus); err != nil {
		failf("embed corpus: %v", err)
	}

	// Detection confusion matrices for three free signals + their union.
	var disagree, shellfb, lowmargin, union confusion
	// Labeling stats among ACTUAL mis-routes.
	misroutes, oracleRight, oracleWrongRankerFails := 0, 0, 0
	// Teacher accuracy among flagged-by-union turns.
	flaggedLabeled, flaggedCorrect := 0, 0

	for _, tr := range traces {
		qv, err := embedOne(ctx, embedder, tr.q)
		if err != nil {
			failf("embed query: %v", err)
		}
		top1, _, margin := oracle(qv, corpus)
		isMisroute := !sameTool(tr.used, tr.gold)

		sigDisagree := !sameTool(tr.used, top1)
		sigShell := fallbackTools[tr.used]
		sigMargin := margin < marginFloor
		sigUnion := sigDisagree || sigShell // margin alone is too noisy as a flag; used as a tiebreak

		tally(&disagree, sigDisagree, isMisroute)
		tally(&shellfb, sigShell, isMisroute)
		tally(&lowmargin, sigMargin, isMisroute)
		tally(&union, sigUnion, isMisroute)

		oracleCorrect := goldMatch(top1, tr.gold)
		if isMisroute {
			misroutes++
			if oracleCorrect {
				oracleRight++
			} else {
				oracleWrongRankerFails++
			}
		}
		if sigUnion {
			flaggedLabeled++
			if oracleCorrect {
				flaggedCorrect++
			}
		}

		logf("TRACE", "%-46q used=%-14s gold=%-22s oracle=%-22s margin=%.3f misroute=%v flagged=%v oracleOK=%v",
			tr.q, tr.used, tr.gold, top1, margin, isMisroute, sigUnion, oracleCorrect)
	}

	logf("DETECT", "disagree   P=%.2f R=%.2f (TP%d FP%d FN%d TN%d)", disagree.precision(), disagree.recall(), disagree.TP, disagree.FP, disagree.FN, disagree.TN)
	logf("DETECT", "shell-fb   P=%.2f R=%.2f (TP%d FP%d FN%d TN%d)", shellfb.precision(), shellfb.recall(), shellfb.TP, shellfb.FP, shellfb.FN, shellfb.TN)
	logf("DETECT", "low-margin P=%.2f R=%.2f (TP%d FP%d FN%d TN%d)", lowmargin.precision(), lowmargin.recall(), lowmargin.TP, lowmargin.FP, lowmargin.FN, lowmargin.TN)
	logf("DETECT", "UNION      P=%.2f R=%.2f (TP%d FP%d FN%d TN%d)", union.precision(), union.recall(), union.TP, union.FP, union.FN, union.TN)
	logf("LABEL", "free-oracle teaches %d/%d mis-routes correctly; %d need a stronger oracle (ranker itself wrong)",
		oracleRight, misroutes, oracleWrongRankerFails)
	labelAcc := 0.0
	if flaggedLabeled > 0 {
		labelAcc = float64(flaggedCorrect) / float64(flaggedLabeled)
	}
	logf("LABEL", "among union-flagged turns, free-oracle label accuracy = %d/%d = %.0f%%", flaggedCorrect, flaggedLabeled, labelAcc*100)

	// Verdict: a usable teacher exists IF detection has high recall (catch the
	// mis-routes) at workable precision AND the free oracle labels the majority
	// correctly — with the explicit ceiling that the ranker's own failures need a
	// periodic stronger oracle (spike 053's validated Gemma/DeepSeek escalation).
	verdict := "PARTIAL"
	if union.recall() >= 0.85 && union.precision() >= 0.6 && labelAcc >= 0.6 {
		verdict = "VALIDATED"
	}

	out := map[string]any{
		"spike":                 "057-toolselection-oracle-signal",
		"n_traces":              len(traces),
		"misroutes":             misroutes,
		"detection_union_P":     union.precision(),
		"detection_union_R":     union.recall(),
		"free_oracle_label_acc": labelAcc,
		"oracle_right":          oracleRight,
		"oracle_needs_stronger": oracleWrongRankerFails,
		"verdict":               verdict,
	}
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
	logf("SUMMARY", "verdict=%s | detect UNION P=%.2f R=%.2f | free-oracle label acc=%.0f%% | %d/%d mis-routes need stronger oracle",
		verdict, union.precision(), union.recall(), labelAcc*100, oracleWrongRankerFails, misroutes)
}

// oracle returns the embedding ranker's top-1 tool for the request, plus the
// top-2 cosine margin (the ambiguity signal the active-learning gate keys on).
func oracle(qv []float64, corpus []entry) (top1 string, score float64, margin float64) {
	best, second := -2.0, -2.0
	for _, c := range corpus {
		s := dot(qv, c.vec)
		if s > best {
			second, best, top1 = best, s, c.name
		} else if s > second {
			second = s
		}
	}
	return top1, best, best - second
}

func tally(c *confusion, flagged, isMisroute bool) {
	switch {
	case flagged && isMisroute:
		c.TP++
	case flagged && !isMisroute:
		c.FP++
	case !flagged && isMisroute:
		c.FN++
	default:
		c.TN++
	}
}

func goldMatch(name, gold string) bool {
	return name == gold || strings.HasSuffix(name, "__"+gold) || strings.HasSuffix(name, gold)
}

// sameTool is order-insensitive tool identity: a bare name (send_message) and its
// MCP-namespaced form (whatsapp__send_message) are the same tool either way round.
func sameTool(a, b string) bool {
	return a == b || strings.HasSuffix(a, "__"+b) || strings.HasSuffix(b, "__"+a)
}

// ---- corpus / embedding ----

func buildCorpus(ctx context.Context) ([]entry, []func() error) {
	reg := tools.NewRegistry()
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
	var corpus []entry
	for _, t := range reg.All() {
		s := t.Spec()
		if !s.Deferred {
			continue
		}
		corpus = append(corpus, entry{name: s.Name, doc: ""})
	}
	// embed docs are built from the spec; store them transiently
	for i := range corpus {
		if t, ok := reg.Get(corpus[i].name); ok {
			corpus[i].doc = naturalText(strings.ReplaceAll(corpus[i].name, "_", " "), t.Spec().Summary+". "+t.Spec().Description)
		}
	}
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })
	return corpus, closers
}

func naturalText(name, desc string) string { return strings.TrimSpace(name + ": " + desc) }

func embedInto(ctx context.Context, e *documents.EmbeddingClient, items []entry) error {
	docs := make([]string, len(items))
	for i, it := range items {
		docs[i] = it.doc
	}
	vecs, err := e.Embed(ctx, docs)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].vec = l2(vecs[i])
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
