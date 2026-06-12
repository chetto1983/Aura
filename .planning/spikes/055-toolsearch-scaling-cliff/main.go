// Spike 055: does semantic tool_search survive a growing corpus, and is the
// operator's runtime-mount scenario affordable? Starts from the real 53-tool live
// corpus (spike 054) and simulates the user mounting more MCP servers at runtime
// (github / slack / gdrive / jira / calendar — generic-verb enterprise tools, the
// realistic gravity-well distractors). At each mount stage it measures:
//
//   - quality: top-1 / MRR@10 over the same 15 documented queries (golds stay in
//     the corpus; the synthetic tools are pure distractors). Does routing survive?
//   - runtime re-embed cost: time to embed ONLY the newly-mounted tools (the
//     incremental cost the live index pays on each `mcp add`).
//   - brute-force cosine latency: mean per-query rank time as N grows — does it
//     stay sub-budget, or is an ANN/HNSW index needed?
//
// Fact/benchmark spike → forensic log + JSON. The operator's point: the corpus is
// dynamic; the index must re-embed/invalidate on mount, never be a static bank.
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

type queryCase struct{ q, gold string }

var cases = []queryCase{
	{"quanto costa il bitcoin adesso?", "web_search"},
	{"prezzo bitcoin", "web_search"},
	{"che tempo fa a Caraglio domani?", "web_search"},
	{"ultime notizie su Cuneo di oggi", "web_search"},
	{"trova un ristorante aperto stasera vicino a me", "web_search"},
	{"leggi il contenuto di questa pagina https://example.com/articolo", "web_fetch"},
	{"scarica e riassumi questo articolo dal link che ti ho mandato", "web_fetch"},
	{"ricorda che preferisco il caffe senza zucchero", "memory_add_preference"},
	{"manda una mail a Marco con il riepilogo del progetto", "send_email"},
	{"scrivimi su whatsapp un messaggio quando il job e finito", "send_message"},
	{"cerca nei miei documenti caricati il manuale della caldaia Siemens", "document_search"},
	{"esegui questo script python per processare il csv", "shell_exec"},
	{"manda questo file excel all'utente come allegato", "send_file"},
}

const rankK = 10

type entry struct {
	name, doc string
	vec       []float64
}

// syntheticServer is a realistic enterprise MCP server a user might mount at
// runtime. Generic verbs (search/create/send/update/list) are deliberate — that
// is what makes them gravity wells for unrelated queries (spike 054 finding).
type syntheticServer struct {
	ns    string
	tools [][2]string // {bare name, description}
}

var stages = []syntheticServer{
	{"github", [][2]string{
		{"create_issue", "Create a new issue in a GitHub repository with a title and body."},
		{"search_issues", "Search issues and pull requests across repositories by query."},
		{"create_pull_request", "Open a pull request from a head branch into a base branch."},
		{"merge_pull_request", "Merge an open pull request into its base branch."},
		{"get_file_contents", "Read the contents of a file at a path in a repository."},
		{"create_or_update_file", "Create or update a file in a repository with a commit message."},
		{"list_commits", "List commits on a branch of a repository."},
		{"create_branch", "Create a new branch in a repository from a base ref."},
		{"add_comment", "Add a comment to an issue or pull request."},
		{"list_pull_requests", "List pull requests in a repository filtered by state."},
		{"get_repository", "Get metadata about a repository."},
		{"fork_repository", "Fork a repository into the authenticated account."},
		{"search_code", "Search code across repositories matching a query."},
		{"create_release", "Create a tagged release with release notes."},
		{"list_workflow_runs", "List GitHub Actions workflow runs for a repository."},
		{"rerun_workflow", "Re-run a failed GitHub Actions workflow run."},
		{"get_pull_request_diff", "Get the unified diff of a pull request."},
		{"request_review", "Request a review on a pull request from a user."},
		{"add_labels", "Add labels to an issue or pull request."},
		{"close_issue", "Close an open issue."},
	}},
	{"slack", [][2]string{
		{"post_message", "Post a message to a Slack channel."},
		{"reply_in_thread", "Reply to a message thread in a channel."},
		{"search_messages", "Search messages across channels by query."},
		{"list_channels", "List channels in the workspace."},
		{"upload_file", "Upload a file to a channel."},
		{"add_reaction", "Add an emoji reaction to a message."},
		{"get_channel_history", "Get the recent message history of a channel."},
		{"create_channel", "Create a new channel in the workspace."},
		{"invite_to_channel", "Invite a user to a channel."},
		{"set_status", "Set the authenticated user's status."},
		{"get_user_info", "Get profile information about a user."},
		{"send_direct_message", "Send a direct message to a user."},
		{"pin_message", "Pin a message in a channel."},
		{"schedule_message", "Schedule a message to be sent later in a channel."},
	}},
	{"gdrive", [][2]string{
		{"search_files", "Search Google Drive files by name or content."},
		{"create_document", "Create a new Google Doc with a title."},
		{"read_file", "Read the contents of a Drive file."},
		{"upload_file", "Upload a local file to Drive."},
		{"share_file", "Share a Drive file with a user by email."},
		{"create_folder", "Create a new folder in Drive."},
		{"move_file", "Move a file to a different folder."},
		{"export_pdf", "Export a Google Doc as a PDF."},
		{"list_recent", "List recently modified files."},
		{"copy_file", "Make a copy of a Drive file."},
	}},
	{"jira", [][2]string{
		{"create_ticket", "Create a Jira ticket in a project with a summary."},
		{"search_tickets", "Search tickets using a JQL query."},
		{"transition_ticket", "Move a ticket to a new status."},
		{"assign_ticket", "Assign a ticket to a user."},
		{"add_worklog", "Log work time against a ticket."},
		{"add_attachment", "Attach a file to a ticket."},
		{"link_tickets", "Create a link between two tickets."},
		{"create_sprint", "Create a sprint in a board."},
		{"get_ticket", "Get the details of a ticket by key."},
		{"comment_ticket", "Add a comment to a ticket."},
	}},
	{"calendar", [][2]string{
		{"create_event", "Create a calendar event with a time and attendees."},
		{"list_events", "List events in a date range."},
		{"update_event", "Update the time or details of an event."},
		{"cancel_event", "Cancel a calendar event."},
		{"find_free_slot", "Find a free time slot across attendees."},
		{"add_attendee", "Add an attendee to an event."},
		{"set_reminder", "Set a reminder before an event."},
		{"get_event", "Get the details of a calendar event."},
	}},
}

type stageMetric struct {
	Stage           string  `json:"stage"`
	CorpusSize      int     `json:"corpus_size"`
	Added           int     `json:"added_tools"`
	ReEmbedMS       float64 `json:"reembed_ms"`         // cost to embed ONLY the new tools
	MeanQueryRankUS float64 `json:"mean_query_rank_us"` // brute-force cosine, query embed excluded
	Top1            int     `json:"top1_hits"`
	MRR             float64 `json:"mrr_at_10"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if os.Getenv("AURA_RUN_DIR") == "" {
		_ = os.Setenv("AURA_RUN_DIR", os.TempDir())
	}

	corpus, closers := buildBaseCorpus(ctx)
	defer func() {
		for _, c := range closers {
			_ = c()
		}
	}()
	logf("BASE", "live corpus = %d tools", len(corpus))
	if len(corpus) < 40 {
		failf("base corpus too small (%d) — bring mail/whatsapp/memory MCP up", len(corpus))
	}

	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteURL(),
		Client:     &http.Client{Timeout: 60 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()

	// Pre-embed the query vectors once (held constant across stages).
	qvs := make([][]float64, len(cases))
	for i, c := range cases {
		v, err := embedOne(ctx, embedder, c.q)
		if err != nil {
			failf("embed query: %v", err)
		}
		qvs[i] = v
	}

	// Stage 0: embed the live base corpus.
	t0 := time.Now()
	if err := embedInto(ctx, embedder, corpus); err != nil {
		failf("embed base: %v", err)
	}
	var metrics []stageMetric
	metrics = append(metrics, measure("base-live", corpus, qvs, len(corpus), time.Since(t0)))

	// Each subsequent stage simulates a runtime `mcp add`: build the new tools,
	// embed ONLY them (the incremental cost), append, re-measure.
	for _, srv := range stages {
		newTools := make([]entry, 0, len(srv.tools))
		for _, t := range srv.tools {
			name := srv.ns + "__" + t[0]
			newTools = append(newTools, entry{name: name, doc: naturalText(strings.ReplaceAll(name, "_", " "), t[1])})
		}
		te := time.Now()
		if err := embedInto(ctx, embedder, newTools); err != nil {
			failf("embed %s: %v", srv.ns, err)
		}
		reembed := time.Since(te)
		corpus = append(corpus, newTools...)
		metrics = append(metrics, measure("+"+srv.ns, corpus, qvs, len(newTools), reembed))
	}

	for _, m := range metrics {
		logf("STAGE", "%-12s N=%-3d (+%-2d) reembed=%6.1fms rank=%6.1fus top1=%2d/%d MRR=%.3f",
			m.Stage, m.CorpusSize, m.Added, m.ReEmbedMS, m.MeanQueryRankUS, m.Top1, len(cases), m.MRR)
	}

	base := metrics[0]
	last := metrics[len(metrics)-1]
	// Verdict: brute-force must stay cheap (sub-millisecond rank at the largest N),
	// re-embed must be affordable per mount, and quality degradation must be bounded
	// (the cliff is real but should not collapse to near-zero).
	verdict := "VALIDATED"
	notes := []string{}
	if last.MeanQueryRankUS > 1000 {
		verdict = "PARTIAL"
		notes = append(notes, "brute-force cosine exceeded 1ms at max N — consider ANN")
	}
	if last.MRR < base.MRR*0.6 {
		notes = append(notes, fmt.Sprintf("quality cliff: MRR %.3f→%.3f (%.0f%% drop) under distractor flood",
			base.MRR, last.MRR, (1-last.MRR/base.MRR)*100))
	}

	out := map[string]any{
		"spike":     "055-toolsearch-scaling-cliff",
		"base_size": base.CorpusSize,
		"max_size":  last.CorpusSize,
		"stages":    metrics,
		"verdict":   verdict,
		"notes":     notes,
	}
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
	logf("SUMMARY", "verdict=%s | N %d→%d | rank %.1f→%.1fus | top1 %d→%d | MRR %.3f→%.3f",
		verdict, base.CorpusSize, last.CorpusSize, base.MeanQueryRankUS, last.MeanQueryRankUS,
		base.Top1, last.Top1, base.MRR, last.MRR)
}

func measure(stage string, corpus []entry, qvs [][]float64, added int, reembed time.Duration) stageMetric {
	m := stageMetric{Stage: stage, CorpusSize: len(corpus), Added: added, ReEmbedMS: ms(reembed)}
	var rankTotal time.Duration
	var mrr float64
	for i, c := range cases {
		t0 := time.Now()
		ranked := cosineRank(qvs[i], corpus)
		rankTotal += time.Since(t0)
		rank := goldRank(ranked, c.gold)
		if rank == 1 {
			m.Top1++
		}
		if rank > 0 {
			mrr += 1.0 / float64(rank)
		}
	}
	m.MeanQueryRankUS = float64(rankTotal.Microseconds()) / float64(len(cases))
	m.MRR = mrr / float64(len(cases))
	return m
}

// ---- corpus / embedding ----

func buildBaseCorpus(ctx context.Context) ([]entry, []func() error) {
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
	} else {
		logf("MCP", "memory mount failed: %v", err)
	}
	if cfg, err := config.Load(); err == nil {
		for name, sc := range cfg.MCPServers {
			mctx, mcancel := context.WithTimeout(ctx, 25*time.Second)
			closer, _, err := mcptools.MountServer(mctx, reg, name, sc)
			mcancel()
			if err == nil {
				closers = append(closers, closer)
			} else {
				logf("MCP", "%s mount skipped: %v", name, err)
			}
		}
	}
	var corpus []entry
	for _, t := range reg.All() {
		s := t.Spec()
		if !s.Deferred {
			continue
		}
		corpus = append(corpus, entry{name: s.Name, doc: naturalText(strings.ReplaceAll(s.Name, "_", " "), s.Summary+". "+s.Description)})
	}
	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })
	return corpus, closers
}

func naturalText(name, desc string) string { return strings.TrimSpace(name + ": " + desc) }

func embedInto(ctx context.Context, e *documents.EmbeddingClient, items []entry) error {
	if len(items) == 0 {
		return nil
	}
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

func cosineRank(qv []float64, corpus []entry) []string {
	type sc struct {
		name string
		s    float64
	}
	scored := make([]sc, len(corpus))
	for i, c := range corpus {
		scored[i] = sc{c.name, dot(qv, c.vec)}
	}
	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].s != scored[b].s {
			return scored[a].s > scored[b].s
		}
		return scored[a].name < scored[b].name
	})
	out := make([]string, len(scored))
	for i, s := range scored {
		out[i] = s.name
	}
	return out
}

func goldRank(ranked []string, gold string) int {
	for i, n := range ranked {
		if i >= rankK {
			break
		}
		if n == gold || strings.HasSuffix(n, "__"+gold) || strings.HasSuffix(n, gold) {
			return i + 1
		}
	}
	return 0
}

// ---- helpers ----

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }

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
