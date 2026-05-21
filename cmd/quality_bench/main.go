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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Query struct {
	ID                string `json:"id"`
	Text              string `json:"text"`
	ExpectedSubstring string `json:"expected_substring"`
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

type QueryResult struct {
	FixtureID         string   `json:"fixture_id"`
	QueryID           string   `json:"query_id"`
	Text              string   `json:"text"`
	ExpectedSubstring string   `json:"expected_substring"`
	Reply             string   `json:"reply,omitempty"`
	Pass              bool     `json:"pass"`
	LatencyMS         int64    `json:"latency_ms"`
	ToolCalls         int      `json:"tool_calls"`
	ToolsUsed         []string `json:"tools_used,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type FixtureResult struct {
	FixtureID string        `json:"fixture_id"`
	File      string        `json:"file"`
	Format    string        `json:"format"`
	SourceID  string        `json:"source_id,omitempty"`
	Ingested  bool          `json:"ingested"`
	Queries   []QueryResult `json:"queries"`
	Error     string        `json:"error,omitempty"`
}

type RunSummary struct {
	Date         string          `json:"date"`
	Label        string          `json:"label"`
	BaseURL      string          `json:"base_url"`
	TotalQueries int             `json:"total_queries"`
	Passes       int             `json:"passes"`
	PassRate     float64         `json:"pass_rate"`
	P95LatencyMS int64           `json:"p95_latency_ms"`
	AvgToolCalls float64         `json:"avg_tool_calls"`
	Fixtures     []FixtureResult `json:"fixtures"`
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
		baseURL    = flag.String("base-url", "http://localhost:18080/api", "Aura API base URL (no trailing slash)")
		token      = flag.String("token", "", "Aura API bearer token (or AURA_API_TOKEN env)")
		queriesFP  = flag.String("queries", "docs/quality-bench/queries.json", "queries.json path")
		fixturesFP = flag.String("fixtures", "docs/quality-bench/fixtures", "fixtures directory")
		outFP      = flag.String("out", "", "output JSON file (default: docs/quality-bench/runs/<date>-<label>.json)")
		label      = flag.String("label", "post-wave-a", "run label for output filename")
		skipUpload = flag.Bool("skip-upload", false, "skip upload+ingest, only re-run queries against existing sources")
		timeout    = flag.Duration("timeout", 180*time.Second, "per-request timeout")
		ingestMax  = flag.Duration("ingest-timeout", 5*time.Minute, "max wait for ingest to complete per fixture")
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

	summary := RunSummary{
		Date:    time.Now().UTC().Format("2006-01-02"),
		Label:   *label,
		BaseURL: *baseURL,
	}

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
		}

		for _, q := range fx.Queries {
			qr := QueryResult{
				FixtureID:         fx.ID,
				QueryID:           q.ID,
				Text:              q.Text,
				ExpectedSubstring: q.ExpectedSubstring,
			}

			t0 := time.Now()
			reply, err := sendChat(client, *baseURL, *token, q.Text)
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
			fmt.Printf("  [%s] %s (%dms, %d calls, tools=[%s]) expected=%q reply=%q\n",
				q.ID, status, qr.LatencyMS, qr.ToolCalls, toolsStr, q.ExpectedSubstring, truncate(reply.Reply, 120))
			fr.Queries = append(fr.Queries, qr)
		}

		summary.Fixtures = append(summary.Fixtures, fr)
	}

	aggregate(&summary)

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
	fmt.Printf("p95 latency:    %dms (%.1fs)\n", summary.P95LatencyMS, float64(summary.P95LatencyMS)/1000.0)
	fmt.Printf("avg tool-calls: %.1f\n", summary.AvgToolCalls)
	fmt.Printf("Output:         %s\n", *outFP)
	fmt.Printf("\nSnapshot row:\n")
	fmt.Printf("| %s | %s | %s | %d/%d | %.0f%% | %.1fs | %.1f | <fill notes> |\n",
		summary.Date, summary.Label, headSHA(), summary.Passes, summary.TotalQueries,
		summary.PassRate*100, float64(summary.P95LatencyMS)/1000.0, summary.AvgToolCalls)
}

func aggregate(s *RunSummary) {
	var allLatencies []int64
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
			toolCallSum += qr.ToolCalls
		}
	}
	if s.TotalQueries > 0 {
		s.PassRate = float64(s.Passes) / float64(s.TotalQueries)
		s.AvgToolCalls = float64(toolCallSum) / float64(s.TotalQueries)
	}
	if len(allLatencies) > 0 {
		sort.Slice(allLatencies, func(i, j int) bool { return allLatencies[i] < allLatencies[j] })
		idx := int(float64(len(allLatencies)-1) * 0.95)
		s.P95LatencyMS = allLatencies[idx]
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

func sendChat(client *http.Client, baseURL, token, message string) (ChatReply, error) {
	url := strings.TrimRight(baseURL, "/") + "/chat"
	payload, _ := json.Marshal(map[string]string{"message": message})
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
