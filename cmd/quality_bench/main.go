// Command quality_bench runs the Aura wiki retrieval Q&A benchmark.
//
// It uploads each fixture in docs/quality-bench/fixtures/, waits for ingest,
// then runs each query through /api/chat. It records pass/fail, p95 latency,
// and average tool-calls per query into a JSON run file, and prints a one-line
// summary for the snapshot doc.
//
//	go run ./cmd/quality_bench \
//	  --base-url http://localhost:18080/api \
//	  --token "$AURA_API_TOKEN" \
//	  --label post-wave-a
//
// See docs/quality-bench/README.md for full procedure.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Query struct {
	ID                string   `json:"id"`
	Text              string   `json:"text"`
	ExpectedSubstring string   `json:"expected_substring"`
	ExpectedSlug      string   `json:"expected_slug,omitempty"`
	ExpectedSlugs     []string `json:"expected_slugs,omitempty"`
}

type Fixture struct {
	ID       string  `json:"id"`
	File     string  `json:"file"`
	Format   string  `json:"format"`
	Pipeline string  `json:"pipeline"`
	Queries  []Query `json:"queries"`
}

type Bench struct {
	Meta     map[string]any `json:"_meta"`
	Fixtures []Fixture      `json:"fixtures"`
}

type ChatReply struct {
	Reply     string   `json:"reply"`
	ToolCalls int      `json:"tool_calls"`
	Tokens    int      `json:"tokens"`
	LLMCalls  int      `json:"llm_calls"`
	ToolsUsed []string `json:"tools_used,omitempty"`
}

type WikiSearchHit struct {
	Rank        int      `json:"rank"`
	Kind        string   `json:"kind"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Snippet     string   `json:"snippet,omitempty"`
	Score       float32  `json:"score"`
	ScoreExact  float32  `json:"score_exact,omitempty"`
	ScoreFTS    float32  `json:"score_fts,omitempty"`
	ScoreVector float32  `json:"score_vector,omitempty"`
	FilePath    string   `json:"file_path,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Sources     []string `json:"sources,omitempty"`
}

type WikiSearchReply struct {
	Query     string          `json:"query"`
	TopK      int             `json:"top_k"`
	Indexed   bool            `json:"indexed"`
	ElapsedMS int64           `json:"elapsed_ms"`
	Results   []WikiSearchHit `json:"results"`
}

type QueryResult struct {
	FixtureID         string          `json:"fixture_id"`
	QueryID           string          `json:"query_id"`
	ThreadID          string          `json:"thread_id,omitempty"`
	Text              string          `json:"text"`
	ExpectedSubstring string          `json:"expected_substring"`
	ExpectedSlugs     []string        `json:"expected_slugs,omitempty"`
	Reply             string          `json:"reply,omitempty"`
	Pass              bool            `json:"pass"`
	LatencyMS         int64           `json:"latency_ms"`
	ToolCalls         int             `json:"tool_calls"`
	ToolsUsed         []string        `json:"tools_used,omitempty"`
	RecallAt5         *bool           `json:"recall_at5,omitempty"`
	SearchLatencyMS   int64           `json:"search_latency_ms,omitempty"`
	SearchResults     []WikiSearchHit `json:"search_results,omitempty"`
	SearchError       string          `json:"search_error,omitempty"`
	Error             string          `json:"error,omitempty"`
}

type FixtureResult struct {
	FixtureID string        `json:"fixture_id"`
	File      string        `json:"file"`
	Format    string        `json:"format"`
	SourceID  string        `json:"source_id,omitempty"`
	WikiPages []string      `json:"wiki_pages,omitempty"`
	Ingested  bool          `json:"ingested"`
	Queries   []QueryResult `json:"queries"`
	Error     string        `json:"error,omitempty"`
}

type RunSummary struct {
	Date               string          `json:"date"`
	Label              string          `json:"label"`
	BaseURL            string          `json:"base_url"`
	TotalQueries       int             `json:"total_queries"`
	Passes             int             `json:"passes"`
	PassRate           float64         `json:"pass_rate"`
	RecallQueries      int             `json:"recall_queries"`
	RecallHits         int             `json:"recall_hits"`
	RecallAt5          float64         `json:"recall_at5"`
	P95LatencyMS       int64           `json:"p95_latency_ms"`
	P95SearchLatencyMS int64           `json:"p95_search_latency_ms"`
	AvgToolCalls       float64         `json:"avg_tool_calls"`
	Gate               *GateResult     `json:"gate,omitempty"`
	Fixtures           []FixtureResult `json:"fixtures"`
}

type GateResult struct {
	Profile               string   `json:"profile,omitempty"`
	MinPassRate           float64  `json:"min_pass_rate,omitempty"`
	MinRecallAt5          float64  `json:"min_recall_at5,omitempty"`
	MaxP95LatencyMS       int64    `json:"max_p95_latency_ms,omitempty"`
	MaxP95SearchLatencyMS int64    `json:"max_p95_search_latency_ms,omitempty"`
	MaxAvgToolCalls       float64  `json:"max_avg_tool_calls,omitempty"`
	Passed                bool     `json:"passed"`
	Failures              []string `json:"failures,omitempty"`
}

// mimeByFormat maps the queries.json `format` field to a Content-Type header.
// Aura's formats.go uses these MIME strings for routing the upload to the
// correct pipeline (markitdown vs Mistral OCR vs passthrough).
var mimeByFormat = map[string]string{
	"pdf":  "application/pdf",
	"txt":  "text/plain; charset=utf-8",
	"md":   "text/markdown; charset=utf-8",
	"json": "application/json",
	"csv":  "text/csv; charset=utf-8",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"epub": "application/epub+zip",
	"html": "text/html; charset=utf-8",
}

func main() {
	var (
		baseURL             = flag.String("base-url", "http://localhost:18080/api", "Aura API base URL (no trailing slash)")
		token               = flag.String("token", "", "Aura API bearer token (or AURA_API_TOKEN env)")
		queriesFP           = flag.String("queries", "docs/quality-bench/queries.json", "queries.json path")
		fixturesFP          = flag.String("fixtures", "docs/quality-bench/fixtures", "fixtures directory")
		outFP               = flag.String("out", "", "output JSON file (default: docs/quality-bench/runs/<date>-<label>.json)")
		label               = flag.String("label", "post-wave-a", "run label for output filename")
		skipUpload          = flag.Bool("skip-upload", false, "skip upload+ingest, only re-run queries against existing sources")
		timeout             = flag.Duration("timeout", 180*time.Second, "per-request timeout")
		ingestMax           = flag.Duration("ingest-timeout", 5*time.Minute, "max wait for ingest to complete per fixture")
		gateProfile         = flag.String("quality-gate", "", "optional target profile to enforce; supported: closure97")
		minPassRate         = flag.Float64("min-pass-rate", 0, "fail when pass_rate is below this value (0 disables)")
		minRecallAt5        = flag.Float64("min-recall-at5", 0, "fail when recall_at5 is below this value (0 disables)")
		maxP95Latency       = flag.Duration("max-p95-latency", 0, "fail when p95 chat latency is above this duration (0 disables)")
		maxP95SearchLatency = flag.Duration("max-p95-search-latency", 0, "fail when p95 direct wiki search latency is above this duration (0 disables)")
		maxAvgToolCalls     = flag.Float64("max-avg-tool-calls", 0, "fail when avg_tool_calls is above this value (0 disables)")
	)
	flag.Parse()

	if *token == "" {
		*token = os.Getenv("AURA_API_TOKEN")
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --token or AURA_API_TOKEN required")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*queriesFP)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read queries: %v\n", err)
		os.Exit(1)
	}
	var bench Bench
	if err := json.Unmarshal(raw, &bench); err != nil {
		fmt.Fprintf(os.Stderr, "parse queries: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout}
	gate := buildGateConfig(*gateProfile, *minPassRate, *minRecallAt5, *maxP95Latency, *maxP95SearchLatency, *maxAvgToolCalls)

	summary := RunSummary{
		Date:    time.Now().UTC().Format("2006-01-02"),
		Label:   *label,
		BaseURL: *baseURL,
	}
	runThreadPrefix := "quality-bench:" + time.Now().UTC().Format("20060102T150405.000000000Z")

	for _, fx := range bench.Fixtures {
		fmt.Printf("\n=== Fixture: %s (%s) ===\n", fx.ID, fx.File)
		fr := FixtureResult{FixtureID: fx.ID, File: fx.File, Format: fx.Format}

		if !*skipUpload {
			filePath := filepath.Join(*fixturesFP, fx.File)
			data, err := os.ReadFile(filePath)
			if err != nil {
				fr.Error = fmt.Sprintf("read fixture: %v", err)
				fmt.Printf("  ERROR: %s\n", fr.Error)
				summary.Fixtures = append(summary.Fixtures, fr)
				continue
			}
			mime := mimeByFormat[fx.Format]
			if mime == "" {
				mime = "application/octet-stream"
			}
			sid, err := uploadSource(client, *baseURL, *token, fx.File, mime, data)
			if err != nil {
				fr.Error = fmt.Sprintf("upload: %v", err)
				fmt.Printf("  ERROR: %s\n", fr.Error)
				summary.Fixtures = append(summary.Fixtures, fr)
				continue
			}
			fr.SourceID = sid
			fmt.Printf("  uploaded: %s\n", sid)

			if err := triggerIngest(client, *baseURL, *token, sid); err != nil {
				fr.Error = fmt.Sprintf("trigger ingest: %v", err)
				fmt.Printf("  ERROR: %s\n", fr.Error)
				summary.Fixtures = append(summary.Fixtures, fr)
				continue
			}

			if err := waitForIngest(client, *baseURL, *token, sid, *ingestMax); err != nil {
				fr.Error = fmt.Sprintf("wait ingest: %v", err)
				fmt.Printf("  ERROR: %s\n", fr.Error)
				summary.Fixtures = append(summary.Fixtures, fr)
				continue
			}
			fr.Ingested = true
			fmt.Printf("  ingested OK\n")
			if detail, err := fetchSourceDetail(client, *baseURL, *token, sid); err != nil {
				fmt.Printf("  WARN: source detail unavailable for recall ground truth: %v\n", err)
			} else {
				fr.WikiPages = detail.WikiPages
				if len(fr.WikiPages) > 0 {
					fmt.Printf("  wiki pages: %s\n", strings.Join(fr.WikiPages, ","))
				}
			}
		}

		for _, q := range fx.Queries {
			qr := QueryResult{
				FixtureID:         fx.ID,
				QueryID:           q.ID,
				ThreadID:          benchThreadID(runThreadPrefix, fx.ID, q.ID),
				Text:              q.Text,
				ExpectedSubstring: q.ExpectedSubstring,
				ExpectedSlugs:     expectedSlugs(q, fr.WikiPages),
			}

			if len(qr.ExpectedSlugs) > 0 {
				tSearch := time.Now()
				searchReply, err := sendWikiSearch(client, *baseURL, *token, q.Text, 5)
				qr.SearchLatencyMS = time.Since(tSearch).Milliseconds()
				if err != nil {
					qr.SearchError = err.Error()
				} else {
					qr.SearchResults = searchReply.Results
					hit := containsAnySlug(searchReply.Results, qr.ExpectedSlugs)
					qr.RecallAt5 = &hit
				}
			}

			t0 := time.Now()
			reply, err := sendChat(client, *baseURL, *token, q.Text, qr.ThreadID)
			qr.LatencyMS = time.Since(t0).Milliseconds()

			if err != nil {
				qr.Error = err.Error()
				fmt.Printf("  [%s] FAIL: %v (%dms)\n", q.ID, err, qr.LatencyMS)
				fr.Queries = append(fr.Queries, qr)
				continue
			}

			qr.Reply = reply.Reply
			qr.ToolCalls = reply.ToolCalls
			qr.ToolsUsed = reply.ToolsUsed
			qr.Pass = strings.Contains(
				strings.ToLower(reply.Reply),
				strings.ToLower(q.ExpectedSubstring),
			)

			status := "FAIL"
			if qr.Pass {
				status = "PASS"
			}
			toolsStr := "(none)"
			if len(reply.ToolsUsed) > 0 {
				toolsStr = strings.Join(reply.ToolsUsed, ",")
			}
			recallStr := "n/a"
			if qr.RecallAt5 != nil {
				recallStr = fmt.Sprintf("%v", *qr.RecallAt5)
			} else if qr.SearchError != "" {
				recallStr = "error"
			}
			fmt.Printf("  [%s] %s (%dms, %d calls, tools=[%s], recall@5=%s, search=%dms) expected=%q reply=%q\n",
				q.ID, status, qr.LatencyMS, qr.ToolCalls, toolsStr, recallStr, qr.SearchLatencyMS, q.ExpectedSubstring, truncate(reply.Reply, 120))
			fr.Queries = append(fr.Queries, qr)
		}

		summary.Fixtures = append(summary.Fixtures, fr)
	}

	aggregate(&summary)
	if gate != nil {
		summary.Gate = evaluateGate(summary, *gate)
	}

	if *outFP == "" {
		runsDir := filepath.Join(filepath.Dir(*queriesFP), "runs")
		_ = os.MkdirAll(runsDir, 0o755)
		*outFP = filepath.Join(runsDir, fmt.Sprintf("%s-%s.json", summary.Date, *label))
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal output: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outFP, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== SUMMARY (%s) ===\n", *label)
	fmt.Printf("Pass rate:      %d/%d (%.0f%%)\n", summary.Passes, summary.TotalQueries,
		summary.PassRate*100)
	if summary.RecallQueries > 0 {
		fmt.Printf("Recall@5:       %d/%d (%.0f%%)\n", summary.RecallHits, summary.RecallQueries,
			summary.RecallAt5*100)
	} else {
		fmt.Printf("Recall@5:       n/a\n")
	}
	fmt.Printf("p95 latency:    %dms (%.1fs)\n", summary.P95LatencyMS, float64(summary.P95LatencyMS)/1000.0)
	if summary.P95SearchLatencyMS > 0 {
		fmt.Printf("p95 search:     %dms\n", summary.P95SearchLatencyMS)
	}
	fmt.Printf("avg tool-calls: %.1f\n", summary.AvgToolCalls)
	if summary.Gate != nil {
		if summary.Gate.Passed {
			fmt.Printf("quality gate:   PASS")
		} else {
			fmt.Printf("quality gate:   FAIL (%s)", strings.Join(summary.Gate.Failures, "; "))
		}
		if summary.Gate.Profile != "" {
			fmt.Printf(" [%s]", summary.Gate.Profile)
		}
		fmt.Println()
	}
	fmt.Printf("Output:         %s\n", *outFP)
	fmt.Printf("\nSnapshot row:\n")
	recallCell := "n/a"
	if summary.RecallQueries > 0 {
		recallCell = fmt.Sprintf("%.0f%%", summary.RecallAt5*100)
	}
	fmt.Printf("| %s | %s | %s | %d/%d | %s | %.1fs | %.1f | <fill notes> |\n",
		summary.Date, summary.Label, headSHA(), summary.Passes, summary.TotalQueries,
		recallCell, float64(summary.P95LatencyMS)/1000.0, summary.AvgToolCalls)
	if summary.Gate != nil && !summary.Gate.Passed {
		os.Exit(1)
	}
}

func aggregate(s *RunSummary) {
	var allLatencies []int64
	var searchLatencies []int64
	var toolCallSum int
	for _, fr := range s.Fixtures {
		for _, qr := range fr.Queries {
			s.TotalQueries++
			if qr.Pass {
				s.Passes++
			}
			if qr.LatencyMS > 0 {
				allLatencies = append(allLatencies, qr.LatencyMS)
			}
			if qr.SearchLatencyMS > 0 {
				searchLatencies = append(searchLatencies, qr.SearchLatencyMS)
			}
			if qr.RecallAt5 != nil {
				s.RecallQueries++
				if *qr.RecallAt5 {
					s.RecallHits++
				}
			}
			toolCallSum += qr.ToolCalls
		}
	}
	if s.TotalQueries > 0 {
		s.PassRate = float64(s.Passes) / float64(s.TotalQueries)
		s.AvgToolCalls = float64(toolCallSum) / float64(s.TotalQueries)
	}
	if s.RecallQueries > 0 {
		s.RecallAt5 = float64(s.RecallHits) / float64(s.RecallQueries)
	}
	if len(allLatencies) > 0 {
		sort.Slice(allLatencies, func(i, j int) bool { return allLatencies[i] < allLatencies[j] })
		idx := int(float64(len(allLatencies)-1) * 0.95)
		s.P95LatencyMS = allLatencies[idx]
	}
	if len(searchLatencies) > 0 {
		sort.Slice(searchLatencies, func(i, j int) bool { return searchLatencies[i] < searchLatencies[j] })
		idx := int(float64(len(searchLatencies)-1) * 0.95)
		s.P95SearchLatencyMS = searchLatencies[idx]
	}
}

func uploadSource(client *http.Client, baseURL, token, filename, mime string, body []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	hdr.Set("Content-Type", mime)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return "", fmt.Errorf("multipart create part: %w", err)
	}
	if _, err := part.Write(body); err != nil {
		return "", fmt.Errorf("multipart write: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("multipart close: %w", err)
	}
	url := strings.TrimRight(baseURL, "/") + "/sources/upload"
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var ur struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &ur); err != nil {
		return "", fmt.Errorf("decode upload: %w (raw: %s)", err, truncate(string(raw), 200))
	}
	if ur.ID == "" {
		return "", fmt.Errorf("upload response has empty id (raw: %s)", truncate(string(raw), 200))
	}
	return ur.ID, nil
}

func triggerIngest(client *http.Client, baseURL, token, sid string) error {
	url := strings.TrimRight(baseURL, "/") + "/sources/" + sid + "/ingest"
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

func waitForIngest(client *http.Client, baseURL, token, sid string, max time.Duration) error {
	deadline := time.Now().Add(max)
	url := strings.TrimRight(baseURL, "/") + "/sources/" + sid
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("GET %s: %w", url, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
		}
		var sr struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &sr); err != nil {
			return fmt.Errorf("decode source: %w (raw: %s)", err, truncate(string(raw), 200))
		}
		switch sr.Status {
		case "ingested":
			return nil
		case "failed":
			return fmt.Errorf("ingest failed (status=failed)")
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout after %s", max)
}

type sourceDetail struct {
	ID        string   `json:"id"`
	WikiPages []string `json:"wiki_pages,omitempty"`
}

func fetchSourceDetail(client *http.Client, baseURL, token, sid string) (sourceDetail, error) {
	url := strings.TrimRight(baseURL, "/") + "/sources/" + sid
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return sourceDetail{}, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sourceDetail{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return sourceDetail{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var detail sourceDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return sourceDetail{}, fmt.Errorf("decode source detail: %w (raw: %s)", err, truncate(string(body), 200))
	}
	return detail, nil
}

func sendWikiSearch(client *http.Client, baseURL, token, query string, topK int) (WikiSearchReply, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/wiki/search")
	if err != nil {
		return WikiSearchReply{}, fmt.Errorf("parse wiki search URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("top_k", fmt.Sprintf("%d", topK))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return WikiSearchReply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return WikiSearchReply{}, fmt.Errorf("GET %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WikiSearchReply{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return WikiSearchReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var reply WikiSearchReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return WikiSearchReply{}, fmt.Errorf("decode wiki search: %w (raw: %s)", err, truncate(string(body), 200))
	}
	return reply, nil
}

func sendChat(client *http.Client, baseURL, token, message, threadID string) (ChatReply, error) {
	url := strings.TrimRight(baseURL, "/") + "/chat"
	requestBody := map[string]string{"message": message}
	if strings.TrimSpace(threadID) != "" {
		requestBody["thread_id"] = threadID
	}
	payload, _ := json.Marshal(requestBody)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ChatReply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ChatReply{}, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatReply{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ChatReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var reply ChatReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return ChatReply{}, fmt.Errorf("decode reply: %w (raw: %s)", err, truncate(string(body), 200))
	}
	return reply, nil
}

func benchThreadID(runPrefix, fixtureID, queryID string) string {
	return strings.Join([]string{
		cleanThreadPart(runPrefix),
		cleanThreadPart(fixtureID),
		cleanThreadPart(queryID),
	}, ":")
}

func cleanThreadPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		switch {
		case ok:
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ':':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

func expectedSlugs(q Query, fixtureSlugs []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(slug string) {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		out = append(out, slug)
	}
	add(q.ExpectedSlug)
	for _, slug := range q.ExpectedSlugs {
		add(slug)
	}
	if len(out) == 0 {
		for _, slug := range fixtureSlugs {
			add(slug)
		}
	}
	return out
}

func containsAnySlug(results []WikiSearchHit, expected []string) bool {
	want := make(map[string]bool, len(expected))
	for _, slug := range expected {
		slug = strings.TrimSpace(slug)
		if slug != "" {
			want[slug] = true
		}
	}
	for _, hit := range results {
		if want[strings.TrimSpace(hit.Slug)] {
			return true
		}
	}
	return false
}

func buildGateConfig(profile string, minPassRate, minRecallAt5 float64, maxP95Latency, maxP95SearchLatency time.Duration, maxAvgToolCalls float64) *GateResult {
	gate := GateResult{
		Profile:               strings.TrimSpace(profile),
		MinPassRate:           minPassRate,
		MinRecallAt5:          minRecallAt5,
		MaxP95LatencyMS:       maxP95Latency.Milliseconds(),
		MaxP95SearchLatencyMS: maxP95SearchLatency.Milliseconds(),
		MaxAvgToolCalls:       maxAvgToolCalls,
	}
	switch strings.ToLower(gate.Profile) {
	case "":
	case "closure97":
		if gate.MinPassRate == 0 {
			gate.MinPassRate = 0.97
		}
		if gate.MinRecallAt5 == 0 {
			gate.MinRecallAt5 = 0.97
		}
		if gate.MaxP95LatencyMS == 0 {
			gate.MaxP95LatencyMS = (10 * time.Second).Milliseconds()
		}
		if gate.MaxP95SearchLatencyMS == 0 {
			gate.MaxP95SearchLatencyMS = (500 * time.Millisecond).Milliseconds()
		}
		if gate.MaxAvgToolCalls == 0 {
			gate.MaxAvgToolCalls = 2
		}
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown --quality-gate %q (supported: closure97)\n", profile)
		os.Exit(2)
	}
	if gate.Profile == "" && gate.MinPassRate == 0 && gate.MinRecallAt5 == 0 && gate.MaxP95LatencyMS == 0 && gate.MaxP95SearchLatencyMS == 0 && gate.MaxAvgToolCalls == 0 {
		return nil
	}
	return &gate
}

func evaluateGate(s RunSummary, gate GateResult) *GateResult {
	gate.Passed = true
	fail := func(format string, args ...any) {
		gate.Passed = false
		gate.Failures = append(gate.Failures, fmt.Sprintf(format, args...))
	}
	if gate.MinPassRate > 0 && s.PassRate < gate.MinPassRate {
		fail("pass_rate %.2f < %.2f", s.PassRate, gate.MinPassRate)
	}
	if gate.MinRecallAt5 > 0 {
		if s.RecallQueries == 0 {
			fail("recall@5 not measured")
		} else if s.RecallAt5 < gate.MinRecallAt5 {
			fail("recall@5 %.2f < %.2f", s.RecallAt5, gate.MinRecallAt5)
		}
	}
	if gate.MaxP95LatencyMS > 0 && s.P95LatencyMS > gate.MaxP95LatencyMS {
		fail("p95_latency_ms %d > %d", s.P95LatencyMS, gate.MaxP95LatencyMS)
	}
	if gate.MaxP95SearchLatencyMS > 0 {
		if s.P95SearchLatencyMS == 0 {
			fail("p95_search_latency_ms not measured")
		} else if s.P95SearchLatencyMS > gate.MaxP95SearchLatencyMS {
			fail("p95_search_latency_ms %d > %d", s.P95SearchLatencyMS, gate.MaxP95SearchLatencyMS)
		}
	}
	if gate.MaxAvgToolCalls > 0 && s.AvgToolCalls > gate.MaxAvgToolCalls {
		fail("avg_tool_calls %.2f > %.2f", s.AvgToolCalls, gate.MaxAvgToolCalls)
	}
	return &gate
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func headSHA() string {
	data, err := os.ReadFile(".git/HEAD")
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if strings.HasPrefix(head, "ref: ") {
		refPath := strings.TrimPrefix(head, "ref: ")
		raw, err := os.ReadFile(filepath.Join(".git", refPath))
		if err != nil {
			return ""
		}
		sha := strings.TrimSpace(string(raw))
		if len(sha) >= 8 {
			return sha[:8]
		}
		return sha
	}
	if len(head) >= 8 {
		return head[:8]
	}
	return head
}
