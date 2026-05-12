// Command probe_doc verifies the byte-level integrity of an already-stored
// source — no LLM invocation, no chat pipe, no tokens. Fetches the raw
// bytes via /api/sources/{id}/raw and runs format-specific structural
// checks: xlsx via excelize, docx via ZIP+XML, pdf via ledongthuc/pdf.
// Detects mojibake, missing required ZIP entries, malformed XML, and
// missing %%EOF.
//
// Usage:
//
//	probe_doc -id src_<hex> -kind xlsx|docx|pdf
//	          [-want "substr1,substr2,..."]   one-shot keyword check
//
// Env:
//
//	AURA_API_BASE   = http://localhost:18080/api
//	AURA_CHAT_TOKEN = bearer token
package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

func main() {
	var (
		id      = flag.String("id", "", "source id (src_<hex>)")
		kind    = flag.String("kind", "", "xlsx | docx | pdf")
		want    = flag.String("want", "", "comma-separated substrings that must appear in extracted text")
		apiBase = flag.String("api", envDefault("AURA_API_BASE", "http://localhost:18080/api"), "dashboard API base")
		token   = flag.String("token", os.Getenv("AURA_CHAT_TOKEN"), "bearer token")
	)
	flag.Parse()
	if *id == "" || *kind == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "probe_doc: -id, -kind, and AURA_CHAT_TOKEN are required")
		os.Exit(2)
	}

	body, err := fetchRaw(*apiBase, *token, *id)
	if err != nil {
		fail(err.Error())
	}
	fmt.Printf("== %s (%d bytes) ==\n", *id, len(body))

	var miss []string
	switch *kind {
	case "xlsx":
		miss = inspectXLSX(body, splitCSV(*want))
	case "docx":
		miss = inspectDOCX(body, splitCSV(*want))
	case "pdf":
		miss = inspectPDF(body, splitCSV(*want))
	default:
		fail("unknown -kind " + *kind + " (want xlsx|docx|pdf)")
	}

	if len(miss) == 0 {
		fmt.Println("PASS")
		return
	}
	fmt.Println("FAIL")
	for _, m := range miss {
		fmt.Println("  -", m)
	}
	os.Exit(1)
}

// =========================================================================
// FORMAT INSPECTORS
// =========================================================================

func inspectXLSX(body []byte, want []string) []string {
	var miss []string
	if len(body) < 2 || string(body[:2]) != "PK" {
		return []string{fmt.Sprintf("not a ZIP: head=%q", body[:min(len(body), 8)])}
	}
	names, _ := zipEntryNames(body)
	for _, must := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/sharedStrings.xml"} {
		if !contains(names, must) {
			miss = append(miss, fmt.Sprintf("missing required entry %q", must))
		}
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		miss = append(miss, fmt.Sprintf("excelize open: %v", err))
		return miss
	}
	defer f.Close()
	fmt.Printf("  sheets:  %v\n", f.GetSheetList())
	for _, sheet := range f.GetSheetList() {
		rows, _ := f.GetRows(sheet)
		fmt.Printf("  %s: %d rows\n", sheet, len(rows))
		for i, row := range rows {
			for j, cell := range row {
				if hasMojibake(cell) {
					miss = append(miss, fmt.Sprintf("%s row %d col %d: mojibake %q", sheet, i+1, j+1, cell))
				}
				if hasControlChars(cell) {
					miss = append(miss, fmt.Sprintf("%s row %d col %d: stray control char %q", sheet, i+1, j+1, cell))
				}
			}
			if i < 5 {
				fmt.Printf("    [%d] %v\n", i+1, row)
			}
		}
	}
	// Want-substrings check against the full text dump.
	textBlob := xlsxText(f)
	for _, w := range want {
		if !strings.Contains(textBlob, w) {
			miss = append(miss, fmt.Sprintf("text missing %q", w))
		}
	}
	return miss
}

func inspectDOCX(body []byte, want []string) []string {
	var miss []string
	if len(body) < 2 || string(body[:2]) != "PK" {
		return []string{fmt.Sprintf("not a ZIP: head=%q", body[:min(len(body), 8)])}
	}
	names, _ := zipEntryNames(body)
	fmt.Printf("  entries: %v\n", names)
	for _, must := range []string{"[Content_Types].xml", "word/document.xml", "_rels/.rels"} {
		if !contains(names, must) {
			miss = append(miss, fmt.Sprintf("missing required entry %q", must))
		}
	}
	docXML, err := zipEntryBytes(body, "word/document.xml")
	if err != nil {
		miss = append(miss, fmt.Sprintf("read word/document.xml: %v", err))
		return miss
	}
	if err := xmlWellFormed(docXML); err != nil {
		miss = append(miss, fmt.Sprintf("word/document.xml not well-formed: %v", err))
	}
	text := xmlInnerText(docXML, "w:t")
	fmt.Printf("  text:    %s\n", truncate(text, 200))
	if hasMojibake(text) {
		miss = append(miss, "body text contains U+FFFD (mojibake)")
	}
	if hasControlChars(text) {
		miss = append(miss, "body text contains stray control chars")
	}
	for _, w := range want {
		if !strings.Contains(text, w) {
			miss = append(miss, fmt.Sprintf("text missing %q", w))
		}
	}
	return miss
}

func inspectPDF(body []byte, want []string) []string {
	var miss []string
	if len(body) < 5 || string(body[:5]) != "%PDF-" {
		return []string{fmt.Sprintf("not a PDF: head=%q", body[:min(len(body), 16)])}
	}
	tail := body
	if len(tail) > 1024 {
		tail = body[len(body)-1024:]
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		miss = append(miss, "missing %%EOF trailer marker in last 1KB")
	}
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		miss = append(miss, fmt.Sprintf("pdf NewReader: %v", err))
		return miss
	}
	fmt.Printf("  version: %s\n", strings.TrimSpace(string(body[:8])))
	fmt.Printf("  pages:   %d\n", reader.NumPage())
	if reader.NumPage() < 1 {
		miss = append(miss, "page count < 1")
	}
	var text strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		t, _ := page.GetPlainText(nil)
		text.WriteString(t)
		text.WriteString("\n")
	}
	out := text.String()
	fmt.Printf("  text:    %s\n", truncate(out, 200))
	if hasMojibake(out) {
		miss = append(miss, "extracted text contains U+FFFD (mojibake)")
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			miss = append(miss, fmt.Sprintf("text missing %q", w))
		}
	}
	return miss
}

// =========================================================================
// HELPERS
// =========================================================================

func fetchRaw(apiBase, token, id string) ([]byte, error) {
	url := strings.TrimRight(apiBase, "/") + "/sources/" + id + "/raw"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

func zipEntryBytes(body []byte, entryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name == entryName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry %q not found", entryName)
}

func xmlWellFormed(b []byte) error {
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

// xmlInnerText returns the concatenated text inside every <TAG>...</TAG>
// pair, tolerating attributes on the open tag (`<w:t xml:space="preserve">`).
func xmlInnerText(b []byte, tag string) string {
	s := string(b)
	var out strings.Builder
	openMarker := "<" + tag
	closeMarker := "</" + tag + ">"
	i := 0
	for {
		start := strings.Index(s[i:], openMarker)
		if start < 0 {
			break
		}
		afterOpen := strings.Index(s[i+start:], ">")
		if afterOpen < 0 {
			break
		}
		textStart := i + start + afterOpen + 1
		end := strings.Index(s[textStart:], closeMarker)
		if end < 0 {
			break
		}
		out.WriteString(s[textStart : textStart+end])
		out.WriteString(" ")
		i = textStart + end + len(closeMarker)
	}
	return out.String()
}

func xlsxText(f *excelize.File) string {
	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, _ := f.GetRows(sheet)
		for _, row := range rows {
			for _, cell := range row {
				sb.WriteString(cell)
				sb.WriteString(" ")
			}
		}
	}
	return sb.String()
}

func hasMojibake(s string) bool { return strings.ContainsRune(s, '�') }

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

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "probe_doc: "+msg)
	os.Exit(2)
}
