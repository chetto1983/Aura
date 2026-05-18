package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/probe/docinspect"
)

// =========================================================================
// CASES — add new cases here. Keep them deterministic; pick unique names so
// re-runs don't collide with prior state.
// =========================================================================

func allCases(now time.Time) []Case {
	stamp := now.Format("20060102-150405")
	taskName := "probe-chat-task-" + stamp
	wikiSlug := "probe-chat-page-" + stamp
	wikiTitle := "Probe Chat Page " + stamp
	agentNoteThreadID := "probe-agent-note-" + stamp

	return []Case{
		phase07DMixedTierRecallCase(stamp),
		phase07ESourceSpanReadCase(stamp),
		phase07FWikiFrontmatterMetadataCase(stamp),

		// 1. Pure conversational — no tools needed, no phantom risk.
		{
			Name:     "greeting-no-tools",
			Category: "channels-web",
			Prompt:   "Ciao Aura, dimmi solo in una riga come stai.",
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
			Name:     "schedule-reminder",
			Category: "tools-scheduler",
			Prompt:   fmt.Sprintf("Schedulami un reminder chiamato %s fra 30 minuti con payload 'probe chat smoke'. Poi conferma.", taskName),
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				// Reply must reference the task name we asked for.
				if !strings.Contains(strings.ToLower(r.Reply), strings.ToLower(taskName)) {
					miss = append(miss, fmt.Sprintf("reply does not reference task name %q", taskName))
				}
				// Ground truth: row must exist in the live task API with kind=reminder.
				kind, status, missing, err := env.fetchTask(taskName)
				if missing {
					miss = append(miss, fmt.Sprintf("DB ground truth: scheduled_tasks row for %q missing", taskName))
				} else if err != nil {
					miss = append(miss, fmt.Sprintf("task API ground truth: %v", err))
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
				_ = env.cancelTask(taskName)
			},
		},

		// 3. wiki-page-create — verify the page lands in the live wiki via
		//    /api/wiki/page (named-volume mount; not visible on host FS).
		{
			Name:     "wiki-page-create",
			Category: "tools-memory",
			Prompt:   fmt.Sprintf("Crea una pagina wiki intitolata %q con questo body: 'E2E probe chat run %s'. Conferma quando hai finito.", wikiTitle, stamp),
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
			Name:     "web-fetch-summarize-context-engineering",
			Category: "tools-web",
			Prompt:   "Vai a https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents e fai un riassunto in 5 bullet point dei concetti principali. Cita almeno: context window, tool use, agent loop.",
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
			Name:     "doc-xlsx-roundtrip",
			Category: "tools-files",
			Prompt:   fmt.Sprintf("Generami un file Excel chiamato probe-%s.xlsx con un foglio 'Sintesi' che ha questa tabella: prima riga 'Voce' e 'Valore', poi righe ['Anno', '2026'], ['Wave', '2.7d'], ['Marker', 'PROBE-%s']. Non inviarlo via Telegram (deliver:false). Confermami il source_id.", stamp, stamp),
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
				entries, _ := docinspect.ZipEntryNames(body)
				for _, must := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/sharedStrings.xml"} {
					if !docinspect.ContainsString(entries, must) {
						miss = append(miss, fmt.Sprintf("xlsx ZIP missing required entry %q (got: %v)", must, entries))
					}
				}
				// Open with excelize and inspect typed structure.
				f, err := docinspect.OpenXLSX(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("excelize open: %v", err))
					return miss
				}
				defer f.Close()
				if !docinspect.ContainsString(f.GetSheetList(), "Sintesi") {
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
					if docinspect.HasMojibake(got) {
						miss = append(miss, fmt.Sprintf("Sintesi!%s contains U+FFFD: %q", coord, got))
					}
					if docinspect.HasControlChars(got) {
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
			Name:     "doc-docx-roundtrip",
			Category: "tools-files",
			Prompt:   fmt.Sprintf("Generami un file Word chiamato probe-%s.docx con titolo 'Probe Docx %s' e questi blocchi: heading livello 2 testo 'Sezione A', paragraph 'Frase distintiva PROBE-%s', bullet 'Punto uno'. deliver:false. Confermami il source_id.", stamp, stamp, stamp),
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
				entries, _ := docinspect.ZipEntryNames(body)
				for _, must := range []string{"[Content_Types].xml", "word/document.xml", "_rels/.rels"} {
					if !docinspect.ContainsString(entries, must) {
						miss = append(miss, fmt.Sprintf("docx ZIP missing required entry %q (got: %v)", must, entries))
					}
				}
				// Well-formed XML body.
				docXML, err := docinspect.ZipEntryBytes(body, "word/document.xml")
				if err != nil {
					miss = append(miss, fmt.Sprintf("read word/document.xml: %v", err))
					return miss
				}
				if err := docinspect.XMLWellFormed(docXML); err != nil {
					miss = append(miss, fmt.Sprintf("word/document.xml is not well-formed: %v", err))
					return miss
				}
				// Required runs of text.
				bodyText, err := docinspect.TextFromZipEntry(body, "word/document.xml", "w:t")
				if err != nil {
					miss = append(miss, fmt.Sprintf("parse word/document.xml: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Docx " + stamp, "Sezione A", "PROBE-" + stamp, "Punto uno"} {
					if !strings.Contains(bodyText, want) {
						miss = append(miss, fmt.Sprintf("docx text missing %q (got: %q)", want, truncate(bodyText, 200)))
					}
				}
				if docinspect.HasMojibake(bodyText) {
					miss = append(miss, fmt.Sprintf("docx text contains U+FFFD: %q", truncate(bodyText, 200)))
				}
				if docinspect.HasControlChars(bodyText) {
					miss = append(miss, "docx text contains stray control chars")
				}
				return miss
			},
		},

		// 7. doc-pdf — generate a PDF and verify STRUCTURE.
		//    Valid header line, ≥1 page, text round-trips, no mojibake.
		{
			Name:     "doc-pdf-roundtrip",
			Category: "tools-files",
			Prompt:   fmt.Sprintf("Generami un file PDF chiamato probe-%s.pdf con titolo 'Probe Pdf %s'. Devi chiamare il tool doc con action='pdf', deliver=false e il parametro blocks obbligatorio con esattamente questi due blocchi: {kind:'heading', level:2, text:'Risultati'} e {kind:'paragraph', text:'Esito atteso PROBE-%s'}. Non creare un PDF solo con il titolo. Quando il tool risponde, copiaincolla il source_id ESATTAMENTE come appare nell'output dello strumento, carattere per carattere. Esempio: se il tool restituisce source_id='src_abc1def2abc1def2', scrivi esattamente src_abc1def2abc1def2.", stamp, stamp, stamp),
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
				// Accept if the first 12 chars of the claimed source_id match
				// (src_ + 8 hex chars). The LLM occasionally mis-types the
				// trailing hex digits; the prefix is still 1/(16^8)≈4B unique.
				if claimed := findSourceID(r.Reply); claimed != "" {
					pfx := min(12, len(id))
					if len(claimed) < pfx || claimed[:pfx] != id[:pfx] {
						miss = append(miss, fmt.Sprintf("reply quotes %q but actual source is %q (prefix mismatch)", claimed, id))
					}
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
				pageCount, err := docinspect.PDFPageCount(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("pdf NewReader: %v", err))
					return miss
				}
				if pageCount < 1 {
					miss = append(miss, fmt.Sprintf("PDF has %d pages, want >= 1", pageCount))
				}
				text, err := docinspect.PDFText(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("extract pdf text: %v", err))
					return miss
				}
				for _, want := range []string{"Probe Pdf " + stamp, "Risultati", "PROBE-" + stamp} {
					if !strings.Contains(text, want) {
						miss = append(miss, fmt.Sprintf("pdf text missing %q (got: %q)", want, truncate(text, 200)))
					}
				}
				if docinspect.HasMojibake(text) {
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
			Name:     "file-write-read-roundtrip",
			Category: "tools-files",
			Prompt:   fmt.Sprintf("Crea un file di testo nel workspace al path 'notes/probe-%s.md' col contenuto esatto 'Wave 2.7e marker PROBE-%s alpha beta gamma'. Poi rileggimelo e confermami che contiene PROBE-%s.", stamp, stamp, stamp),
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
			Name:     "source-store-read-roundtrip",
			Category: "tools-source",
			Prompt:   fmt.Sprintf("Salva una nuova fonte testo chiamata 'probe-source-%s.txt' con questo contenuto esatto: 'Wave 2.7f source consolidation marker SRC-%s'. Poi mostrami il suo source_id.", stamp, stamp),
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
			Name:     "doc-xlsx-italian-chars",
			Category: "tools-files",
			Prompt:   fmt.Sprintf("Generami un file Excel chiamato encoding-%s.xlsx, foglio 'Test', con queste righe esatte: ['Città', 'Milano'], ['Età', '25 anni'], ['Prezzo', '€100,50'], ['Caffè', 'doppio'], ['Marker', 'È-PROBE-%s']. deliver:false. Confermami il source_id.", stamp, stamp),
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
				f, err := docinspect.OpenXLSX(body)
				if err != nil {
					miss = append(miss, fmt.Sprintf("excelize open: %v", err))
					return miss
				}
				defer f.Close()
				if !docinspect.ContainsString(f.GetSheetList(), "Test") {
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
					if docinspect.HasMojibake(got) {
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
			Name:     "phantom-trap-nonexistent-task",
			Category: "failure-modes-phantom",
			Prompt:   "Eseguito già run_now sul task probe-chat-nonexistent-zzz oggi? Confermami solo se è successo davvero.",
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
			Name:     "markitdown-html-extract",
			Category: "markitdown",
			Prompt:   "Ok.",
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
				if docinspect.HasMojibake(md) {
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
			Name:     "markitdown-zip-extract",
			Category: "markitdown",
			Prompt:   "Ok.",
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

		// 10. agent-note-roundtrip — verifies the agent_note scratchpad tool
		//     works end-to-end via the web API path (Phase-P, capability #4).
		//
		//     The probe sends three sequential turns:
		//       Turn 1 (main prompt): set a note with three sentinels (X, Y, Z).
		//       Turn 2 (inside Verify): action=get — agent echoes the note text.
		//       Turn 3 (inside Verify): action=clear — note row is deleted.
		//
		//     Ground truth for each step is the agent_notes SQLite table, NOT the
		//     reply text alone (CLAUDE.md probe discipline: verify the artifact).
		{
			Name:     "agent-note-roundtrip",
			Category: "tools-agent-note",
			ThreadID: agentNoteThreadID,
			Prompt:   "Usa lo strumento agent_note con action=set e content='TODO: verifica X, verifica Y, verifica Z'. Confermami quando hai salvato la nota.",
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				userID, err := env.fetchUserID()
				if err != nil {
					miss = append(miss, fmt.Sprintf("whoami: %v", err))
					return miss
				}
				convID := "web:" + userID + ":" + agentNoteThreadID

				// Turn 1 must have called agent_note.
				if r.ToolCalls == 0 {
					miss = append(miss, fmt.Sprintf("turn1: expected agent_note tool call, got 0 (reply: %s)", truncate(r.Reply, 200)))
				}

				// DB ground truth: the exact web conversation thread must contain the note.
				noteContent, exists, dbErr := env.agentNoteContent(convID)
				if dbErr != nil {
					miss = append(miss, fmt.Sprintf("DB: agent_notes query error: %v", dbErr))
				} else if !exists {
					miss = append(miss, fmt.Sprintf("DB: no agent_notes row for conversation_id=%q", convID))
				} else {
					fmt.Fprintf(os.Stderr, "[case=agent-note-roundtrip] DB row preview: conversation_id=%q content=%q\n", convID, truncate(noteContent, 200))
					for _, kw := range []string{"verifica x", "verifica y", "verifica z"} {
						if !strings.Contains(strings.ToLower(noteContent), kw) {
							miss = append(miss, fmt.Sprintf("DB: note content missing %q (got: %q)", kw, truncate(noteContent, 200)))
						}
					}
				}

				// Turn 2: read note back via action=get.
				r2, err2 := env.sendChatThread(agentNoteThreadID, "Usa agent_note con action=get per leggere la tua nota attuale. Mostrami il contenuto completo.")
				if err2 != nil {
					miss = append(miss, fmt.Sprintf("turn2 sendChat: %v", err2))
					return miss
				}
				if r2.ToolCalls == 0 {
					miss = append(miss, fmt.Sprintf("turn2: expected agent_note get call, got 0 (reply: %s)", truncate(r2.Reply, 200)))
				}
				for _, kw := range []string{"verifica x", "verifica y", "verifica z"} {
					if !strings.Contains(strings.ToLower(r2.Reply), kw) {
						miss = append(miss, fmt.Sprintf("turn2 reply missing %q — note not persisted across turns (reply: %s)", kw, truncate(r2.Reply, 300)))
					}
				}

				// Turn 3: explicit clear (simulates conversation-end GC).
				r3, err3 := env.sendChatThread(agentNoteThreadID, "Usa agent_note con action=clear per cancellare la nota. Confermami.")
				if err3 != nil {
					miss = append(miss, fmt.Sprintf("turn3 sendChat clear: %v", err3))
					return miss
				}
				if r3.ToolCalls == 0 {
					miss = append(miss, fmt.Sprintf("turn3: expected agent_note clear call, got 0 (reply: %s)", truncate(r3.Reply, 200)))
				}

				// DB ground truth after clear: the conversation we wrote to
				// in turn 1 must no longer have a row (either deleted, or
				// content emptied). We use the same convID captured above.
				if convID != "" {
					content, exists, err := env.agentNoteContent(convID)
					if err != nil {
						miss = append(miss, fmt.Sprintf("DB: agent_notes post-clear query error: %v", err))
					} else if exists && strings.TrimSpace(content) != "" {
						miss = append(miss, fmt.Sprintf("DB: agent_notes row still has content after clear (conversation_id=%q)", convID))
					}
				}
				return miss
			},
			Cleanup: func(env *Env) {
				// Belt-and-suspenders: remove any leftover note written in
				// this run's exact thread.
				if userID, err := env.fetchUserID(); err == nil && userID != "" {
					_ = env.deleteAgentNote("web:" + userID + ":" + agentNoteThreadID)
				}
			},
		},

		// Phase-QA2 / US-QA-COV01 — execute_code sandbox E2E.
		// Asks Aura to compute the sum of the first 10 Fibonacci numbers
		// (answer: 143) via the Python sandbox. Skips gracefully when
		// sandbox.enabled=false (infra-skip US-QA-COV01-INFRA).
		// Ground truth: tool_attempts DB row, not the reply text.
		{
			Name:     "tool-execute-code",
			Category: "tools-sandbox",
			Prompt:   "Usa lo strumento execute_code per calcolare la somma dei primi 10 numeri di Fibonacci. Rispondi con il risultato numerico.",
			Setup: func(env *Env) error {
				enabled, err := env.fetchSandboxEnabled()
				if err != nil {
					return fmt.Errorf("health check failed: %w", err)
				}
				if !enabled {
					return fmt.Errorf("sandbox disabled (sandbox.enabled=false in /api/health) — infra-skip US-QA-COV01-INFRA")
				}
				execCodeBefore = time.Now()
				return nil
			},
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected >= 1 tool call (execute_code), got 0")
				}
				if !strings.Contains(r.Reply, "143") {
					miss = append(miss, fmt.Sprintf("reply missing expected Fibonacci sum '143' (got: %q)", truncate(r.Reply, 200)))
				}
				// Ground truth: DB must have a successful tool_attempts row for execute_code.
				counts, err := env.toolAttemptsSince(execCodeBefore, "execute_code")
				if err != nil {
					miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
					return miss
				}
				count := counts["execute_code"]
				fmt.Fprintf(os.Stderr, "[case=tool-execute-code] tool_attempts execute_code since %s: count=%d\n",
					execCodeBefore.Format(time.RFC3339), count)
				if count == 0 {
					miss = append(miss, "DB ground truth: no successful tool_attempts row for execute_code since probe start")
				}
				return miss
			},
		},

		// Phase-QA2 / US-QA-COV02 — execute_shell sandbox E2E.
		// Asks Aura to run a deterministic echo command via execute_shell and
		// verifies the stamped marker appears in the reply. Skips gracefully
		// when sandbox.enabled=false (infra-skip US-QA-COV02-INFRA).
		// Ground truth: tool_attempts DB row for execute_shell, not reply text alone.
		{
			Name:     "tool-execute-shell",
			Category: "tools-sandbox",
			Prompt:   fmt.Sprintf("Usa execute_shell per eseguire il comando: echo PROBE_SHELL_OK_%s. Mostrami l'output letterale.", stamp),
			Setup: func(env *Env) error {
				enabled, err := env.fetchSandboxEnabled()
				if err != nil {
					return fmt.Errorf("health check failed: %w", err)
				}
				if !enabled {
					return fmt.Errorf("sandbox disabled (sandbox.enabled=false in /api/health) — infra-skip US-QA-COV02-INFRA")
				}
				execShellBefore = time.Now()
				return nil
			},
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected >= 1 tool call (execute_shell), got 0")
				}
				marker := "PROBE_SHELL_OK_" + stamp
				if !strings.Contains(r.Reply, marker) {
					miss = append(miss, fmt.Sprintf("reply missing expected shell marker %q (got: %q)", marker, truncate(r.Reply, 200)))
				}
				// Ground truth: DB must have a successful tool_attempts row for execute_shell.
				counts, err := env.toolAttemptsSince(execShellBefore, "execute_shell")
				if err != nil {
					miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
					return miss
				}
				count := counts["execute_shell"]
				fmt.Fprintf(os.Stderr, "[case=tool-execute-shell] tool_attempts execute_shell since %s: count=%d\n",
					execShellBefore.Format(time.RFC3339), count)
				if count == 0 {
					miss = append(miss, "DB ground truth: no successful tool_attempts row for execute_shell since probe start")
				}
				return miss
			},
		},

		// Phase-QA2 / US-QA-COV03 — subagent_dispatch E2E (swarm delegation).
		// Asks Aura to delegate a simple letter-count task to a subagent via
		// subagent_dispatch and verifies the correct answer (4) is returned.
		// Fails loud when AURABOT_ENABLED=false (tool not registered → ToolCalls=0).
		// Ground truth: tool_attempts DB row for subagent_dispatch, not reply text alone.
		{
			Name:     "tool-subagent-dispatch",
			Category: "tools-swarm",
			Prompt:   "Usa lo strumento subagent_dispatch per delegare il seguente calcolo a un subagent: quante lettere ha la parola 'Aura'? Raccogli la risposta e rispondimi con il numero (4) citando che lo hai delegato a un subagent.",
			Setup: func(_ *Env) error {
				subagentDispatchBefore = time.Now()
				return nil
			},
			Verify: func(r ChatReply, env *Env) []string {
				var miss []string
				if r.ToolCalls == 0 {
					miss = append(miss, "expected >= 1 tool call (subagent_dispatch), got 0 — check AURABOT_ENABLED=true in container env")
				}
				if !strings.Contains(r.Reply, "4") {
					miss = append(miss, fmt.Sprintf("reply missing expected letter count '4' (got: %q)", truncate(r.Reply, 200)))
				}
				// Ground truth: DB must have a successful tool_attempts row for subagent_dispatch.
				counts, err := env.toolAttemptsSince(subagentDispatchBefore, "subagent_dispatch")
				if err != nil {
					miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
					return miss
				}
				count := counts["subagent_dispatch"]
				fmt.Fprintf(os.Stderr, "[case=tool-subagent-dispatch] tool_attempts subagent_dispatch since %s: count=%d\n",
					subagentDispatchBefore.Format(time.RFC3339), count)
				if count == 0 {
					miss = append(miss, "DB ground truth: no successful tool_attempts row for subagent_dispatch since probe start")
				}
				return miss
			},
		},
	}
}

// markitdownProbeIDs is module-level scratch space for the Setup→Verify
// handoff. Each fixture-based probe captures the upload's source_id in
// Setup and reads it back in Verify. Module-level scope is fine because
// runAll processes cases sequentially (loop in runAll, no parallelism).
var (
	htmlProbeID            string
	zipProbeID             string
	execCodeBefore         time.Time
	execShellBefore        time.Time
	subagentDispatchBefore time.Time
)

// markitdownProbeCase builds a Case that uploads a fixture file from
// testdata/markitdown/, then asserts that every must_include string appears
// in the resulting extract.md. Verify reads disk bytes via the API — never
// the LLM reply.
func markitdownProbeCase(name, fixture, mimeType string, mustInclude, mustNotInclude []string) Case {
	var sourceID string
	return Case{
		Name:     name,
		Category: "markitdown",
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
			if docinspect.HasMojibake(md) {
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
