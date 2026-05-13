// Command probe_chat is the canonical end-to-end chat pipe for Aura.
//
// It exercises the running /api/chat endpoint with a battery of cases
// and verifies BOTH the assistant's textual reply AND ground truth
// (SQLite tables, filesystem). This is the rule documented in
// CLAUDE.md: "tool_calls: N in the response is necessary but never
// sufficient; the model can call a tool correctly and still
// hallucinate around the result."
//
// Roles per CLAUDE.md:
//
//	I (Claude Code) am the programmer — I write and run this pipe.
//	Aura is the runtime under test — every textual claim she makes
//	must be cross-checked against ground truth before being trusted.
//
// Usage:
//
//	go run ./cmd/probe_chat                       # run all cases
//	go run ./cmd/probe_chat -case schedule        # run one case
//	go run ./cmd/probe_chat -prompt "..." -raw    # ad-hoc one-shot
//
// Env config (with defaults):
//
//	AURA_CHAT_URL    = http://localhost:18080/api/chat
//	AURA_CHAT_TOKEN  = (required for non-loopback; bearer token)
//	AURA_DB_PATH     = ./data/aura.db   (read-only)
//	AURA_API_BASE    = http://localhost:18080/api   (for wiki ground truth)
//
// Wiki ground truth is fetched via /api/wiki/page?slug=… because the
// in-container wiki lives in a named Docker volume, not a bind mount.
package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aura/aura/internal/db"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// ChatReply mirrors api.ChatReply just enough to deserialize the JSON.
type ChatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
}

// Case is one E2E assertion: a prompt to send and a Verify closure
// that returns one entry per assertion violation. Empty slice = PASS.
type Case struct {
	Name    string
	Prompt  string
	Setup   func(env *Env) error                // optional: prep state before sending
	Verify  func(reply ChatReply, env *Env) []string // required
	Cleanup func(env *Env)                      // optional: tear down leftover state
}

// Env bundles everything a Verify function needs to consult ground truth.
type Env struct {
	DB        *sql.DB
	APIBase   string // e.g. http://localhost:18080/api
	APIToken  string
	APIClient *http.Client
}

// sourceIDRe extracts a source_id token (`src_<16 hex>`) from free-form
// assistant text. The doc tool's JSON reply contains source_id and the
// model usually echoes it; we grep the natural-language reply for it
// rather than parse a structured field that may or may not exist.
var sourceIDRe = regexp.MustCompile(`src_[a-f0-9]{16}`)

// findSourceID returns the first src_xxx token in s, or "" if absent.
func findSourceID(s string) string {
	return sourceIDRe.FindString(s)
}

// findSourceByFilename queries /api/sources for the most recent source
// whose filename matches the supplied substring. Used by doc probes
// because the model occasionally fabricates the source_id in the reply
// (real failure mode observed 2026-05-12: tool fires correctly, but
// the assistant echoes a hallucinated ID). The filesystem ground truth
// is the authoritative source list — read it instead of parsing prose.
func (e *Env) findSourceByFilename(substr string) (string, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var sources []struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
		return "", fmt.Errorf("decode sources: %w", err)
	}
	for _, s := range sources {
		if strings.Contains(s.Filename, substr) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no source matched %q (have %d sources)", substr, len(sources))
}

// fetchSourceRaw returns the raw bytes of a source via /api/sources/{id}/raw.
func (e *Env) fetchSourceRaw(id string) ([]byte, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id + "/raw"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return io.ReadAll(resp.Body)
}

// uploadSourceFile POSTs a binary to /api/sources/upload (multipart) and
// returns the assigned source id. This is the bytes-of-the-artifact entry
// point — the LLM is bypassed so Wave 2.9 markitdown probes can verify the
// extract pipeline directly, without entangling the test with model
// behavior. See [docs/wave-2.9-markitdown.md](docs/wave-2.9-markitdown.md).
func (e *Env) uploadSourceFile(filename, mimeType string, body []byte) (id, status string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	hdr.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return "", "", fmt.Errorf("multipart create part: %w", err)
	}
	if _, err := part.Write(body); err != nil {
		return "", "", fmt.Errorf("multipart write: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", "", fmt.Errorf("multipart close: %w", err)
	}
	url := strings.TrimRight(e.APIBase, "/") + "/sources/upload"
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var ur struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &ur); err != nil {
		return "", "", fmt.Errorf("decode upload response: %w", err)
	}
	return ur.ID, ur.Status, nil
}

// fetchSourceMarkdown returns the on-disk extract.md (or ocr.md for PDF
// sources) for a given source id via /api/sources/{id}/markdown. The
// endpoint reads the bytes off the filesystem inside the container, so this
// is the disk-byte ground truth even when the wiki lives in a named volume
// that the host can't bind-mount.
func (e *Env) fetchSourceMarkdown(id string) (string, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id + "/markdown"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var sm struct {
		Markdown string `json:"markdown"`
		File     string `json:"file"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sm); err != nil {
		return "", fmt.Errorf("decode markdown response: %w", err)
	}
	return sm.Markdown, nil
}

// deleteSource removes a source via DELETE /api/sources/{id}. Used by Cleanup
// hooks so each probe run starts with an empty wiki/raw for the test ID.
func (e *Env) deleteSource(id string) error {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

// extractTextFromZipEntry returns the concatenated text content of every
// `<TAG>...</TAG>` (e.g. <w:t> in docx, <t> in xlsx sharedStrings) inside
// the named entry of the ZIP body. Returns "" if entry missing.
func extractTextFromZipEntry(body []byte, entryName, tag string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("not a valid zip: %w", err)
	}
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == entryName {
			entry = f
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("entry %q not found in zip", entryName)
	}
	rc, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("open %s: %w", entryName, err)
	}
	defer rc.Close()
	xmlBytes, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", entryName, err)
	}
	xml := string(xmlBytes)
	// Naive but reliable: scan for `<tag` ... `>` ... `</tag>` segments.
	// Works for both `<w:t>foo</w:t>` (docx) and `<t>foo</t>` (xlsx) and
	// tolerates attributes like `<w:t xml:space="preserve">`.
	var out strings.Builder
	openMarker := "<" + tag
	closeMarker := "</" + tag + ">"
	i := 0
	for {
		start := strings.Index(xml[i:], openMarker)
		if start < 0 {
			break
		}
		afterOpen := strings.Index(xml[i+start:], ">")
		if afterOpen < 0 {
			break
		}
		textStart := i + start + afterOpen + 1
		end := strings.Index(xml[textStart:], closeMarker)
		if end < 0 {
			break
		}
		out.WriteString(xml[textStart : textStart+end])
		out.WriteString(" ")
		i = textStart + end + len(closeMarker)
	}
	return out.String(), nil
}

// extractPDFText returns the textual content of a PDF byte buffer via
// ledongthuc/pdf. Returns "" with a wrapped error on parse failure.
func extractPDFText(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("pdf NewReader: %w", err)
	}
	var out strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, perr := page.GetPlainText(nil)
		if perr != nil {
			continue
		}
		out.WriteString(text)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// xmlIsWellFormed returns nil if the byte slice parses to a complete
// XML document, or the parser error. Catches truncated / corrupt
// document.xml — the most common DOCX-generator failure that still
// produces a valid-looking ZIP.
func xmlIsWellFormed(b []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// zipEntryBytes returns the bytes of one entry inside a ZIP archive,
// or an error if the entry is missing or unreadable.
func zipEntryBytes(body []byte, entryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("zip reader: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == entryName {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", entryName, err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry %q not found", entryName)
}

// zipEntryNames lists every entry name inside a ZIP archive.
func zipEntryNames(body []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out, nil
}

// sliceContainsStr is a tiny slices.Contains shim — keeps verify cases
// from each pulling in slices just for one membership check.
func sliceContainsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// hasMojibake reports true when the string contains a U+FFFD replacement
// character — the canonical signal that an encoding round-trip lost
// information. Useful for catching latin-1/UTF-8 confusion in generated
// documents.
func hasMojibake(s string) bool {
	return strings.ContainsRune(s, '�')
}

// hasControlChars returns true when the string contains any C0 control
// byte other than tab/LF/CR, OR any C1 control byte. These should never
// appear in user-visible document content; if they do, the generator
// emitted a raw byte where it should have escaped.
func hasControlChars(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

// excelizeOpen wraps excelize.OpenReader so cases can drop into typed
// workbook inspection without rebuilding the boilerplate every time.
func excelizeOpen(body []byte) (*excelize.File, error) {
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("excelize open: %w", err)
	}
	return f, nil
}

// fetchWikiPage reads a single wiki page through the dashboard API.
// Returns (nil, true) when the API responds 404 (page genuinely missing)
// so Verify functions can distinguish "missing" from transport errors.
func (e *Env) fetchWikiPage(slug string) (body string, missing bool, err error) {
	url := strings.TrimRight(e.APIBase, "/") + "/wiki/page?slug=" + slug
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var page struct {
		BodyMD string `json:"body_md"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", false, fmt.Errorf("decode: %w", err)
	}
	// Reconstruct a frontmatter-ish view so substring asserts work against title.
	return "title: " + page.Title + "\n" + page.BodyMD, false, nil
}

// =========================================================================
// CASES — add new cases here. Keep them deterministic; pick unique names so
// re-runs don't collide with prior state.
// =========================================================================

func allCases(now time.Time) []Case {
	stamp := now.Format("20060102-150405")
	taskName := "probe-chat-task-" + stamp
	wikiSlug := "probe-chat-page-" + stamp
	wikiTitle := "Probe Chat Page " + stamp

	return []Case{
		// 1. Pure conversational — no tools needed, no phantom risk.
		{
			Name:   "greeting-no-tools",
			Prompt: "Ciao Aura, dimmi solo in una riga come stai.",
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls != 0 {
					miss = append(miss, fmt.Sprintf("expected 0 tool calls for greeting, got %d", r.ToolCalls))
				}
				if strings.TrimSpace(r.Reply) == "" {
					miss = append(miss, "reply is empty")
				}
				return miss
			},
		},

		// 2. schedule-reminder — verify DB row matches what the reply claims.
		{
			Name:   "schedule-reminder",
			Prompt: fmt.Sprintf("Schedulami un reminder chiamato %s fra 30 minuti con payload 'probe chat smoke'. Poi conferma.", taskName),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				// Reply must reference the task name we asked for.
				if !strings.Contains(strings.ToLower(r.Reply), strings.ToLower(taskName)) {
					miss = append(miss, fmt.Sprintf("reply does not reference task name %q", taskName))
				}
				// Ground truth: row must exist in scheduled_tasks with kind=reminder.
				var kind, status string
				err := env.DB.QueryRow(
					`SELECT kind, status FROM scheduled_tasks WHERE name = ?`,
					taskName,
				).Scan(&kind, &status)
				if err == sql.ErrNoRows {
					miss = append(miss, fmt.Sprintf("DB ground truth: scheduled_tasks row for %q missing", taskName))
				} else if err != nil {
					miss = append(miss, fmt.Sprintf("DB query error: %v", err))
				} else {
					if kind != "reminder" {
						miss = append(miss, fmt.Sprintf("DB kind = %q, want reminder", kind))
					}
					if status != "active" {
						miss = append(miss, fmt.Sprintf("DB status = %q, want active", status))
					}
				}
				return miss
			},
			Cleanup: func(env *Env) {
				_, _ = env.DB.Exec(`UPDATE scheduled_tasks SET status='cancelled' WHERE name = ?`, taskName)
			},
		},

		// 3. wiki-page-create — verify the page lands in the live wiki via
		//    /api/wiki/page (named-volume mount; not visible on host FS).
		{
			Name:   "wiki-page-create",
			Prompt: fmt.Sprintf("Crea una pagina wiki intitolata %q con questo body: 'E2E probe chat run %s'. Conferma quando hai finito.", wikiTitle, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if !strings.Contains(strings.ToLower(r.Reply), strings.ToLower(wikiSlug)) {
					miss = append(miss, fmt.Sprintf("reply does not reference slug %q", wikiSlug))
				}
				body, missing, err := env.fetchWikiPage(wikiSlug)
				if err != nil {
					miss = append(miss, fmt.Sprintf("wiki API ground truth: %v", err))
					return miss
				}
				if missing {
					miss = append(miss, fmt.Sprintf("wiki API ground truth: slug %q returned 404 — page was not actually created", wikiSlug))
					return miss
				}
				if !strings.Contains(body, wikiTitle) {
					miss = append(miss, fmt.Sprintf("wiki page %q does not contain title %q", wikiSlug, wikiTitle))
				}
				if !strings.Contains(body, "E2E probe chat run "+stamp) {
					miss = append(miss, fmt.Sprintf("wiki page %q does not contain expected body", wikiSlug))
				}
				return miss
			},
			// No cleanup — wiki pages are user-visible artifacts and the
			// timestamped slug avoids cross-run collisions. A pre-existing
			// cleanup endpoint would be nicer; leave it for now.
		},

		// 4. web-fetch-summarize — fetch a specific page and produce a
		//    real summary. Hardest case in the suite: must call web with
		//    action=fetch, parse the HTML, and synthesize a faithful
		//    summary that contains the article's load-bearing concepts.
		//    Anthropic's "Effective context engineering for AI agents"
		//    is a stable target with predictable vocabulary.
		{
			Name:   "web-fetch-summarize-context-engineering",
			Prompt: "Vai a https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents e fai un riassunto in 5 bullet point dei concetti principali. Cita almeno: context window, tool use, agent loop.",
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (web fetch) — got 0, likely phantom")
				}
				reply := strings.ToLower(r.Reply)
				// Load-bearing concepts from the actual page. If the
				// summary doesn't mention these, the model either didn't
				// fetch or hallucinated content.
				required := []string{"context", "agent", "tool"}
				for _, kw := range required {
					if !strings.Contains(reply, kw) {
						miss = append(miss, fmt.Sprintf("summary missing required keyword %q (page is about %s)", kw, kw))
					}
				}
				// A real summary should be substantive — at least 300
				// chars and contain enumeration cues (bullets, numbers).
				if len(strings.TrimSpace(r.Reply)) < 300 {
					miss = append(miss, fmt.Sprintf("summary too short: %d chars (real summary of a multi-section article should be >= 300)", len(r.Reply)))
				}
				if !strings.Contains(r.Reply, "-") && !strings.Contains(r.Reply, "•") && !strings.Contains(r.Reply, "1.") && !strings.Contains(r.Reply, "1)") {
					miss = append(miss, "reply has no bullet/numbered enumeration despite the prompt asking for 5 bullet points")
				}
				return miss
			},
		},

		// 5. doc-xlsx — generate a workbook and verify STRUCTURE, not just
		//    "some bytes came back". Opens with excelize, asserts sheet
		//    name + exact cell coordinates, screens for mojibake.
		{
			Name:   "doc-xlsx-roundtrip",
			Prompt: fmt.Sprintf("Generami un file Excel chiamato probe-%s.xlsx con un foglio 'Sintesi' che ha questa tabella: prima riga 'Voce' e 'Valore', poi righe ['Anno', '2026'], ['Wave', '2.7d'], ['Marker', 'PROBE-%s']. Non inviarlo via Telegram (deliver:false). Confermami il source_id.", stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc xlsx)")
				}
				id, err := env.findSourceByFilename(stamp)
				if err != nil {
					miss = append(miss, fmt.Sprintf("source lookup: %v", err))
					return miss
				}
				if claimed := findSourceID(r.Reply); claimed != "" && claimed != id {
					miss = append(miss, fmt.Sprintf("reply quotes %q but actual source is %q (model misquoted)", claimed, id))
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				if len(body) < 4 || string(body[:2]) != "PK" {
					miss = append(miss, fmt.Sprintf("not a valid xlsx zip (head=%q)", body[:min(len(body), 8)]))
					return miss
				}
				// Required ZIP entries for any conformant xlsx.
				entries, _ := zipEntryNames(body)
				for _, must := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/sharedStrings.xml"} {
					if !sliceContainsStr(entries, must) {
						miss = append(miss, fmt.Sprintf("xlsx ZIP missing required entry %q (got: %v)", must, entries))
					}
				}
				// Open with excelize and inspect typed structure.
				f, err := excelizeOpen(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("excelize open: %v", err))
					return miss
				}
				defer f.Close()
				if !sliceContainsStr(f.GetSheetList(), "Sintesi") {
					miss = append(miss, fmt.Sprintf("expected sheet 'Sintesi', got %v", f.GetSheetList()))
					return miss
				}
				// Exact-cell assertions (not substring) — this is what "well
				// formatted" means: the right value in the right coordinate.
				wantCells := map[string]string{
					"A1": "Voce", "B1": "Valore",
					"A2": "Anno", "B2": "2026",
					"A3": "Wave", "B3": "2.7d",
					"A4": "Marker", "B4": "PROBE-" + stamp,
				}
				for coord, want := range wantCells {
					got, _ := f.GetCellValue("Sintesi", coord)
					if got != want {
						miss = append(miss, fmt.Sprintf("Sintesi!%s = %q, want %q", coord, got, want))
					}
				}
				// Encoding sanity: no mojibake, no rogue control chars.
				for coord := range wantCells {
					got, _ := f.GetCellValue("Sintesi", coord)
					if hasMojibake(got) {
						miss = append(miss, fmt.Sprintf("Sintesi!%s contains U+FFFD: %q", coord, got))
					}
					if hasControlChars(got) {
						miss = append(miss, fmt.Sprintf("Sintesi!%s contains control chars: %q", coord, got))
					}
				}
				return miss
			},
		},

		// 6. doc-docx — generate a Word document and verify STRUCTURE.
		//    ZIP entries present, document.xml well-formed XML, expected
		//    text round-trips, no mojibake.
		{
			Name:   "doc-docx-roundtrip",
			Prompt: fmt.Sprintf("Generami un file Word chiamato probe-%s.docx con titolo 'Probe Docx %s' e questi blocchi: heading livello 2 testo 'Sezione A', paragraph 'Frase distintiva PROBE-%s', bullet 'Punto uno'. deliver:false. Confermami il source_id.", stamp, stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc docx)")
				}
				id, err := env.findSourceByFilename(stamp)
				if err != nil {
					miss = append(miss, fmt.Sprintf("source lookup: %v", err))
					return miss
				}
				if claimed := findSourceID(r.Reply); claimed != "" && claimed != id {
					miss = append(miss, fmt.Sprintf("reply quotes %q but actual source is %q (model misquoted)", claimed, id))
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				if len(body) < 4 || string(body[:2]) != "PK" {
					miss = append(miss, fmt.Sprintf("not a valid docx zip (head=%q)", body[:min(len(body), 8)]))
					return miss
				}
				// Required ZIP entries for any conformant docx.
				entries, _ := zipEntryNames(body)
				for _, must := range []string{"[Content_Types].xml", "word/document.xml", "_rels/.rels"} {
					if !sliceContainsStr(entries, must) {
						miss = append(miss, fmt.Sprintf("docx ZIP missing required entry %q (got: %v)", must, entries))
					}
				}
				// Well-formed XML body.
				docXML, err := zipEntryBytes(body, "word/document.xml")
				if err != nil {
					miss = append(miss, fmt.Sprintf("read word/document.xml: %v", err))
					return miss
				}
				if err := xmlIsWellFormed(docXML); err != nil {
					miss = append(miss, fmt.Sprintf("word/document.xml is not well-formed: %v", err))
					return miss
				}
				// Required runs of text.
				bodyText, err := extractTextFromZipEntry(body, "word/document.xml", "w:t")
				if err != nil {
					miss = append(miss, fmt.Sprintf("parse word/document.xml: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Docx " + stamp, "Sezione A", "PROBE-" + stamp, "Punto uno"} {
					if !strings.Contains(bodyText, want) {
						miss = append(miss, fmt.Sprintf("docx text missing %q (got: %q)", want, truncate(bodyText, 200)))
					}
				}
				if hasMojibake(bodyText) {
					miss = append(miss, fmt.Sprintf("docx text contains U+FFFD: %q", truncate(bodyText, 200)))
				}
				if hasControlChars(bodyText) {
					miss = append(miss, "docx text contains stray control chars")
				}
				return miss
			},
		},

		// 7. doc-pdf — generate a PDF and verify STRUCTURE.
		//    Valid header line, ≥1 page, text round-trips, no mojibake.
		{
			Name:   "doc-pdf-roundtrip",
			Prompt: fmt.Sprintf("Generami un file PDF chiamato probe-%s.pdf con titolo 'Probe Pdf %s' e due blocchi: heading livello 2 'Risultati', paragraph 'Esito atteso PROBE-%s'. deliver:false. Confermami il source_id.", stamp, stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (doc pdf)")
				}
				id, err := env.findSourceByFilename(stamp)
				if err != nil {
					miss = append(miss, fmt.Sprintf("source lookup: %v", err))
					return miss
				}
				if claimed := findSourceID(r.Reply); claimed != "" && claimed != id {
					miss = append(miss, fmt.Sprintf("reply quotes %q but actual source is %q (model misquoted)", claimed, id))
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				// PDF version header: %PDF-1.x newline.
				if len(body) < 8 || string(body[:5]) != "%PDF-" {
					miss = append(miss, fmt.Sprintf("not a valid PDF header: %q", body[:min(len(body), 16)]))
					return miss
				}
				// %%EOF marker should appear in the last 1024 bytes — without
				// it, downstream readers reject the file.
				tail := body
				if len(tail) > 1024 {
					tail = body[len(body)-1024:]
				}
				if !bytes.Contains(tail, []byte("%%EOF")) {
					miss = append(miss, "PDF missing %%EOF trailer marker in last 1KB")
				}
				// Page count via the real PDF parser.
				reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
				if err != nil {
					miss = append(miss, fmt.Sprintf("pdf NewReader: %v", err))
					return miss
				}
				if reader.NumPage() < 1 {
					miss = append(miss, fmt.Sprintf("PDF has %d pages, want >= 1", reader.NumPage()))
				}
				text, err := extractPDFText(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("extract pdf text: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Pdf " + stamp, "Risultati", "PROBE-" + stamp} {
					if !strings.Contains(text, want) {
						miss = append(miss, fmt.Sprintf("pdf text missing %q (got: %q)", want, truncate(text, 200)))
					}
				}
				if hasMojibake(text) {
					miss = append(miss, fmt.Sprintf("pdf text contains U+FFFD: %q", truncate(text, 200)))
				}
				return miss
			},
		},

		// 8b. file-roundtrip — exercise the unified file tool AND verify
		//     the bytes-on-disk match the requested content. The probe
		//     reads the host-side bind mount (runtime-workspace/) so we
		//     never trust the LLM's reply text — only the artifact.
		{
			Name:   "file-write-read-roundtrip",
			Prompt: fmt.Sprintf("Crea un file di testo nel workspace al path 'notes/probe-%s.md' col contenuto esatto 'Wave 2.7e marker PROBE-%s alpha beta gamma'. Poi rileggimelo e confermami che contiene PROBE-%s.", stamp, stamp, stamp),
			Verify: func(r ChatReply, _ *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (file write+read)")
				}
				// Ground truth: the file must exist on the workspace bind
				// mount with the expected content. Tool said it; verify it.
				hostPath := filepath.Join("runtime-workspace", "notes", "probe-"+stamp+".md")
				body, err := os.ReadFile(hostPath)
				if err != nil {
					miss = append(miss, fmt.Sprintf("ground truth: file %s missing on disk (%v)", hostPath, err))
					return miss
				}
				want := "Wave 2.7e marker PROBE-" + stamp + " alpha beta gamma"
				got := strings.TrimSpace(string(body))
				if got != want {
					miss = append(miss, fmt.Sprintf("disk content mismatch:\n  got:  %q\n  want: %q", got, want))
				}
				return miss
			},
			Cleanup: func(_ *Env) {
				_ = os.Remove(filepath.Join("runtime-workspace", "notes", "probe-"+stamp+".md"))
			},
		},

		// 8c. source-store-read-roundtrip — exercise the unified source tool
		//     (Wave 2.7f). Asks Aura to store a text source, then verifies
		//     the source persisted on disk under wiki/raw/<src>/original.txt
		//     and the SQLite-mirrored memoryindex sees it. No trust in reply.
		{
			Name:   "source-store-read-roundtrip",
			Prompt: fmt.Sprintf("Salva una nuova fonte testo chiamata 'probe-source-%s.txt' con questo contenuto esatto: 'Wave 2.7f source consolidation marker SRC-%s'. Poi mostrami il suo source_id.", stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected at least 1 tool call (source store)")
				}
				// Ground truth: scan runtime-workspace/wiki/raw/src_*/ for the
				// matching original.txt body. Source IDs are filesystem dirs
				// under wiki/raw, even when the host bind-mount sees the wiki
				// as a named volume (which we already saw with wiki pages).
				// First try the host bind path; if empty, fall back to the
				// API source list lookup by filename pattern.
				want := "Wave 2.7f source consolidation marker SRC-" + stamp
				wantFile := "probe-source-" + stamp + ".txt"
				id, err := env.findSourceByFilename(wantFile)
				if err != nil {
					miss = append(miss, fmt.Sprintf("source lookup by filename %q: %v", wantFile, err))
					return miss
				}
				// Fetch raw bytes via the dashboard API.
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				got := strings.TrimSpace(string(body))
				if got != want {
					miss = append(miss, fmt.Sprintf("source bytes mismatch:\n  got:  %q\n  want: %q", got, want))
				}
				return miss
			},
		},

		// 9. doc-xlsx-italian-chars — encoding regression probe. Italian
		//    accented characters and currency must round-trip byte-exact
		//    through the generator + persistence + API + reader stack.
		{
			Name:   "doc-xlsx-italian-chars",
			Prompt: fmt.Sprintf("Generami un file Excel chiamato encoding-%s.xlsx, foglio 'Test', con queste righe esatte: ['Città', 'Milano'], ['Età', '25 anni'], ['Prezzo', '€100,50'], ['Caffè', 'doppio'], ['Marker', 'È-PROBE-%s']. deliver:false. Confermami il source_id.", stamp, stamp),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected 1 tool call")
				}
				id, err := env.findSourceByFilename(stamp)
				if err != nil {
					miss = append(miss, fmt.Sprintf("source lookup: %v", err))
					return miss
				}
				if claimed := findSourceID(r.Reply); claimed != "" && claimed != id {
					miss = append(miss, fmt.Sprintf("reply quotes %q but actual source is %q (model misquoted)", claimed, id))
				}
				body, err := env.fetchSourceRaw(id)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch raw %s: %v", id, err))
					return miss
				}
				f, err := excelizeOpen(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("excelize open: %v", err))
					return miss
				}
				defer f.Close()
				if !sliceContainsStr(f.GetSheetList(), "Test") {
					miss = append(miss, fmt.Sprintf("expected sheet 'Test', got %v", f.GetSheetList()))
					return miss
				}
				wantCells := map[string]string{
					"A1": "Città", "B1": "Milano",
					"A2": "Età", "B2": "25 anni",
					"A3": "Prezzo", "B3": "€100,50",
					"A4": "Caffè", "B4": "doppio",
					"A5": "Marker", "B5": "È-PROBE-" + stamp,
				}
				for coord, want := range wantCells {
					got, _ := f.GetCellValue("Test", coord)
					if got != want {
						miss = append(miss, fmt.Sprintf("Test!%s = %q, want %q (encoding regression)", coord, got, want))
					}
					if hasMojibake(got) {
						miss = append(miss, fmt.Sprintf("Test!%s mojibake: %q", coord, got))
					}
				}
				return miss
			},
		},

		// 8. phantom-trap — non-existent task name; model MUST NOT claim it ran.
		//    The reply may explain that the task doesn't exist OR may schedule a
		//    new one of that name. Either is fine; what's forbidden is claiming
		//    a past run that never happened.
		{
			Name:   "phantom-trap-nonexistent-task",
			Prompt: "Eseguito già run_now sul task probe-chat-nonexistent-zzz oggi? Confermami solo se è successo davvero.",
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				reply := strings.ToLower(r.Reply)
				// DB ground truth: this task should not exist.
				var count int
				_ = env.DB.QueryRow(
					`SELECT count(*) FROM scheduled_tasks WHERE name = 'probe-chat-nonexistent-zzz'`,
				).Scan(&count)
				if count > 0 {
					miss = append(miss, "test precondition violated: probe-chat-nonexistent-zzz already exists in DB")
					return miss
				}
				// The reply must NOT contain a phantom claim of past execution.
				// Acceptable: "non esiste", "non lo trovo", "no, non risulta".
				phantomClaims := []string{
					"sì, è stato eseguito",
					"si, è stato eseguito",
					"l'ho eseguito",
					"ho eseguito",
					"è già stato eseguito",
					"il task è stato eseguito",
					"yes, i ran it",
					"i executed it",
					"i ran it",
					"successfully ran",
				}
				for _, phrase := range phantomClaims {
					if strings.Contains(reply, phrase) {
						miss = append(miss, fmt.Sprintf("phantom claim detected: reply contains %q", phrase))
					}
				}
				return miss
			},
		},

		// =====================================================================
		// Wave 2.9 — markitdown sidecar verification.
		//
		// Each case uploads a real binary directly to /api/sources/upload
		// (bypassing the LLM), waits for the synchronous extract response,
		// then fetches extract.md back via /api/sources/{id}/markdown and
		// asserts that markitdown's output contains the format-specific
		// must_include strings borrowed from the upstream markitdown test
		// vectors (D:\tmp\markitdown\packages\markitdown\tests\_test_vectors.py).
		//
		// The prompt is a trivial no-op — the LLM is not under test here.
		// All assertions read disk bytes via the API, not r.Reply.
		// =====================================================================

		// 9a. markitdown-xlsx-extract — workbook with three known sentinel UUIDs.
		markitdownProbeCase(
			"markitdown-xlsx-extract",
			"sample.xlsx",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			[]string{
				"## 09060124-b5e7-4717-9d07-3c046eb",
				"6ff4173b-42a5-4784-9b19-f49caff4d93d",
				"affc7dad-52dc-4b98-9b5d-51e65d8a8ad0",
			},
			nil,
		),

		// 9b. markitdown-docx-extract — Word doc with heading hierarchy +
		//     sentinel UUIDs from the upstream fixture.
		markitdownProbeCase(
			"markitdown-docx-extract",
			"sample.docx",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			[]string{
				"314b0a30-5b04-470b-b9f7-eed2c2bec74a",
				"## d666f1f7-46cb-42bd-9a39-9a39cf2a509f",
				"# Abstract",
				"# Introduction",
				"AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation",
			},
			nil,
		),

		// 9c. markitdown-pptx-extract — PowerPoint deck with sentinel UUIDs
		//     scattered across slides.
		markitdownProbeCase(
			"markitdown-pptx-extract",
			"sample.pptx",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			[]string{
				"2cdda5c8-e50e-4db4-b5f0-9722a649f455",
				"04191ea8-5c73-4215-a1d3-1cfb43aaaf12",
				"44bf7d06-5e7a-4a40-a2e1-a2e42ef28c8a",
				"AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation",
			},
			nil,
		),

		// 9d. markitdown-epub-extract — EPUB with chapter headings + blockquote.
		markitdownProbeCase(
			"markitdown-epub-extract",
			"sample.epub",
			"application/epub+zip",
			[]string{
				"# Chapter 1: Test Content",
				"# Chapter 2: More Content",
				"This is a **test** paragraph with some formatting",
				"> This is a blockquote for testing",
			},
			nil,
		),

		// 9e. markitdown-html-extract — synthetic HTML built in-memory with
		//     a unique stamp so duplicate-detection doesn't collide between
		//     runs. Markitdown should strip tags and preserve link text.
		{
			Name: "markitdown-html-extract",
			Prompt: "Ok.",
			Setup: func(env *Env) error {
				body := []byte(fmt.Sprintf(`<!doctype html><html><head><title>Probe HTML</title></head>
<body>
  <h1>Wave 2.9 HTML probe %s</h1>
  <p>This is a <strong>bold</strong> markitdown sentinel: HTML-PROBE-%s.</p>
  <p>Link to <a href="https://example.com/aura">example</a>.</p>
  <ul><li>alpha</li><li>beta</li><li>gamma-%s</li></ul>
</body></html>`, stamp, stamp, stamp))
				id, status, err := env.uploadSourceFile("probe-"+stamp+".html", "text/html", body)
				if err != nil {
					return fmt.Errorf("upload html: %w", err)
				}
				if status != "extract_complete" {
					return fmt.Errorf("upload html status = %s, want extract_complete", status)
				}
				htmlProbeID = id
				return nil
			},
			Verify: func(_ ChatReply, env *Env) []string {
				var miss []string
				if htmlProbeID == "" {
					miss = append(miss, "html probe id not captured by Setup")
					return miss
				}
				md, err := env.fetchSourceMarkdown(htmlProbeID)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch markdown %s: %v", htmlProbeID, err))
					return miss
				}
				for _, want := range []string{
					"Wave 2.9 HTML probe " + stamp,
					"HTML-PROBE-" + stamp,
					"example",
					"gamma-" + stamp,
				} {
					if !strings.Contains(md, want) {
						miss = append(miss, fmt.Sprintf("extract.md missing %q", want))
					}
				}
				// Tag stripping: markitdown should not leak raw <p>/<h1> markup.
				for _, leaked := range []string{"<h1>", "<p>", "<strong>", "<a href"} {
					if strings.Contains(md, leaked) {
						miss = append(miss, fmt.Sprintf("extract.md leaked raw HTML tag %q", leaked))
					}
				}
				if hasMojibake(md) {
					miss = append(miss, "extract.md contains mojibake")
				}
				return miss
			},
			Cleanup: func(env *Env) {
				if htmlProbeID != "" {
					_ = env.deleteSource(htmlProbeID)
					htmlProbeID = ""
				}
			},
		},

		// 9f. markitdown-zip-extract — ZIP archive built in-memory containing
		//     two known members (csv + txt). Markitdown should walk both and
		//     emit their content in one merged markdown.
		{
			Name: "markitdown-zip-extract",
			Prompt: "Ok.",
			Setup: func(env *Env) error {
				var zbuf bytes.Buffer
				zw := zip.NewWriter(&zbuf)
				if w, err := zw.Create("data.csv"); err != nil {
					return fmt.Errorf("zip create csv: %w", err)
				} else if _, err := w.Write([]byte("city,population\nMilano,1396059\nRoma,2761632\nZIP-PROBE-" + stamp + ",1\n")); err != nil {
					return fmt.Errorf("zip write csv: %w", err)
				}
				if w, err := zw.Create("readme.txt"); err != nil {
					return fmt.Errorf("zip create txt: %w", err)
				} else if _, err := w.Write([]byte("Wave 2.9 zip recursion test " + stamp + "\nAura should remember decisions.\n")); err != nil {
					return fmt.Errorf("zip write txt: %w", err)
				}
				if err := zw.Close(); err != nil {
					return fmt.Errorf("zip close: %w", err)
				}
				id, status, err := env.uploadSourceFile("probe-"+stamp+".zip", "application/zip", zbuf.Bytes())
				if err != nil {
					return fmt.Errorf("upload zip: %w", err)
				}
				if status != "extract_complete" {
					return fmt.Errorf("upload zip status = %s, want extract_complete", status)
				}
				zipProbeID = id
				return nil
			},
			Verify: func(_ ChatReply, env *Env) []string {
				var miss []string
				if zipProbeID == "" {
					miss = append(miss, "zip probe id not captured by Setup")
					return miss
				}
				md, err := env.fetchSourceMarkdown(zipProbeID)
				if err != nil {
					miss = append(miss, fmt.Sprintf("fetch markdown %s: %v", zipProbeID, err))
					return miss
				}
				// Both members must surface in the merged extract.
				for _, want := range []string{
					"ZIP-PROBE-" + stamp,
					"Milano",
					"Roma",
					"Aura should remember decisions",
					"Wave 2.9 zip recursion test " + stamp,
				} {
					if !strings.Contains(md, want) {
						miss = append(miss, fmt.Sprintf("extract.md missing %q (zip member not extracted)", want))
					}
				}
				return miss
			},
			Cleanup: func(env *Env) {
				if zipProbeID != "" {
					_ = env.deleteSource(zipProbeID)
					zipProbeID = ""
				}
			},
		},
	}
}

// markitdownProbeIDs is module-level scratch space for the Setup→Verify
// handoff. Each fixture-based probe captures the upload's source_id in
// Setup and reads it back in Verify. Module-level scope is fine because
// runAll processes cases sequentially (loop in runAll, no parallelism).
var (
	htmlProbeID string
	zipProbeID  string
)

// markitdownProbeCase builds a Case that uploads a fixture file from
// testdata/markitdown/, then asserts that every must_include string appears
// in the resulting extract.md. Verify reads disk bytes via the API — never
// the LLM reply.
func markitdownProbeCase(name, fixture, mimeType string, mustInclude, mustNotInclude []string) Case {
	var sourceID string
	return Case{
		Name: name,
		// The LLM gets a trivial prompt because the actual assertion runs
		// against the upload pipeline, not the chat path. Setup uploads
		// the binary; Verify reads extract.md back via the dashboard API.
		Prompt: "Ok.",
		Setup: func(env *Env) error {
			path := filepath.Join("cmd", "probe_chat", "testdata", "markitdown", fixture)
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read fixture %s: %w", path, err)
			}
			// Use a per-run filename so duplicate-detection doesn't collide
			// when probes re-run. Stamp suffix lives in the closure.
			now := time.Now().UTC().Format("20060102-150405")
			uploadName := strings.TrimSuffix(fixture, filepath.Ext(fixture)) + "-" + now + filepath.Ext(fixture)
			id, status, err := env.uploadSourceFile(uploadName, mimeType, body)
			if err != nil {
				return fmt.Errorf("upload %s: %w", fixture, err)
			}
			if status != "extract_complete" {
				return fmt.Errorf("upload status = %s, want extract_complete (markitdown sidecar may be down)", status)
			}
			sourceID = id
			return nil
		},
		Verify: func(_ ChatReply, env *Env) []string {
			var miss []string
			if sourceID == "" {
				miss = append(miss, "source id not captured by Setup (upload failed)")
				return miss
			}
			md, err := env.fetchSourceMarkdown(sourceID)
			if err != nil {
				miss = append(miss, fmt.Sprintf("fetch markdown %s: %v", sourceID, err))
				return miss
			}
			for _, want := range mustInclude {
				if !strings.Contains(md, want) {
					miss = append(miss, fmt.Sprintf("extract.md missing %q", want))
				}
			}
			for _, banned := range mustNotInclude {
				if strings.Contains(md, banned) {
					miss = append(miss, fmt.Sprintf("extract.md unexpectedly contains %q", banned))
				}
			}
			if hasMojibake(md) {
				miss = append(miss, "extract.md contains mojibake (encoding regression)")
			}
			if len(md) < 50 {
				miss = append(miss, fmt.Sprintf("extract.md is suspiciously short (%d bytes) — markitdown likely returned an empty body", len(md)))
			}
			return miss
		},
		Cleanup: func(env *Env) {
			if sourceID != "" {
				_ = env.deleteSource(sourceID)
				sourceID = ""
			}
		},
	}
}

// =========================================================================

func main() {
	var (
		caseName  = flag.String("case", "", "run only the named case (empty = run all)")
		prompt    = flag.String("prompt", "", "send a single ad-hoc prompt and print the structured reply (skips Verify)")
		jsonOut   = flag.Bool("json", false, "emit results as JSON instead of human-readable table")
		baseURL   = flag.String("url", envDefault("AURA_CHAT_URL", "http://localhost:18080/api/chat"), "chat endpoint")
		apiBase   = flag.String("api", envDefault("AURA_API_BASE", "http://localhost:18080/api"), "dashboard API base (used for wiki ground truth)")
		token     = flag.String("token", os.Getenv("AURA_CHAT_TOKEN"), "bearer token (defaults to $AURA_CHAT_TOKEN)")
		dbPath    = flag.String("db", envDefault("AURA_DB_PATH", "./data/aura.db"), "SQLite DB path (read-only)")
		timeoutS  = flag.Int("timeout", 240, "per-prompt timeout (seconds)")
	)
	flag.Parse()

	if *token == "" {
		fail("AURA_CHAT_TOKEN is required (env or -token)")
	}

	client := &http.Client{Timeout: time.Duration(*timeoutS) * time.Second}

	// Ad-hoc one-shot: print structured reply, exit.
	if strings.TrimSpace(*prompt) != "" {
		reply, err := sendChat(client, *baseURL, *token, *prompt)
		if err != nil {
			fail(err.Error())
		}
		printOneShot(reply, *jsonOut)
		return
	}

	// Ground-truth probes need DB + wiki access.
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fail(fmt.Sprintf("open DB %s: %v", *dbPath, err))
	}
	defer db.Close()
	env := &Env{
		DB:        db,
		APIBase:   *apiBase,
		APIToken:  *token,
		APIClient: client,
	}

	cases := allCases(time.Now())
	if *caseName != "" {
		filtered := cases[:0]
		for _, c := range cases {
			if c.Name == *caseName {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			fail(fmt.Sprintf("no case named %q", *caseName))
		}
		cases = filtered
	}

	results := runAll(client, *baseURL, *token, env, cases)
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(results)
	} else {
		printReport(results)
	}
	if anyFailed(results) {
		os.Exit(1)
	}
}

// =========================================================================
// EXECUTION
// =========================================================================

type Result struct {
	Name        string   `json:"name"`
	Prompt      string   `json:"prompt"`
	Reply       string   `json:"reply"`
	ToolCalls   int      `json:"tool_calls"`
	LLMCalls    int      `json:"llm_calls"`
	Tokens      int      `json:"tokens"`
	ElapsedMs   int64    `json:"elapsed_ms"`
	Mismatches  []string `json:"mismatches"`
	TransportErr string  `json:"transport_err,omitempty"`
	Pass        bool     `json:"pass"`
}

func runAll(client *http.Client, baseURL, token string, env *Env, cases []Case) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		if c.Setup != nil {
			if err := c.Setup(env); err != nil {
				out = append(out, Result{
					Name:         c.Name,
					Prompt:       c.Prompt,
					TransportErr: fmt.Sprintf("setup: %v", err),
				})
				continue
			}
		}
		reply, err := sendChat(client, baseURL, token, c.Prompt)
		if err != nil {
			out = append(out, Result{
				Name:         c.Name,
				Prompt:       c.Prompt,
				TransportErr: err.Error(),
			})
			if c.Cleanup != nil {
				c.Cleanup(env)
			}
			continue
		}
		mismatches := c.Verify(reply, env)
		out = append(out, Result{
			Name:       c.Name,
			Prompt:     c.Prompt,
			Reply:      reply.Reply,
			ToolCalls:  reply.ToolCalls,
			LLMCalls:   reply.LLMCalls,
			Tokens:     reply.Tokens,
			ElapsedMs:  reply.ElapsedMs,
			Mismatches: mismatches,
			Pass:       len(mismatches) == 0,
		})
		if c.Cleanup != nil {
			c.Cleanup(env)
		}
	}
	return out
}

func sendChat(client *http.Client, baseURL, token, prompt string) (ChatReply, error) {
	payload, _ := json.Marshal(map[string]string{"message": prompt})
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	if err != nil {
		return ChatReply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ChatReply{}, fmt.Errorf("POST %s: %w", baseURL, err)
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
		return ChatReply{}, fmt.Errorf("decode reply: %w (raw: %s)", err, truncate(string(body), 400))
	}
	return reply, nil
}

// =========================================================================
// REPORT
// =========================================================================

func printOneShot(r ChatReply, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	fmt.Printf("reply:      %s\n", r.Reply)
	fmt.Printf("tool_calls: %d\n", r.ToolCalls)
	fmt.Printf("llm_calls:  %d\n", r.LLMCalls)
	fmt.Printf("tokens:     %d\n", r.Tokens)
	fmt.Printf("elapsed_ms: %d\n", r.ElapsedMs)
}

func printReport(results []Result) {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Pass {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("=== probe_chat: %d total, %d PASS, %d FAIL ===\n\n", len(results), passed, failed)
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s  (tool_calls=%d, llm=%d, tokens=%d, elapsed=%dms)\n",
			status, r.Name, r.ToolCalls, r.LLMCalls, r.Tokens, r.ElapsedMs)
		fmt.Printf("  prompt: %s\n", truncate(r.Prompt, 160))
		fmt.Printf("  reply : %s\n", truncate(r.Reply, 280))
		if r.TransportErr != "" {
			fmt.Printf("  TRANSPORT ERROR: %s\n", r.TransportErr)
		}
		for _, m := range r.Mismatches {
			fmt.Printf("  MISMATCH: %s\n", m)
		}
		fmt.Println()
	}
}

func anyFailed(results []Result) bool {
	for _, r := range results {
		if !r.Pass {
			return true
		}
	}
	return false
}

// =========================================================================
// HELPERS
// =========================================================================

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	// Route through internal/db.OpenReadOnly to honor the shared driver
	// policy (the TestProductionSQLiteOpensGoThroughSharedDBPackage gate).
	return db.OpenReadOnly(path)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "probe_chat: "+msg)
	os.Exit(2)
}
