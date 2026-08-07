package tools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	pathpkg "path"
	"strings"
)

// Go port of hermes-agent's tools/read_extract.py — document-to-text extraction for read_file.
//
// hermes has two tiers: a stdlib-only path for .ipynb/.docx/.xlsx, and an OPTIONAL richer path
// (the `anydoc` Rust converter) for legacy Office/OpenDocument/RTF/EPUB/PDF. Only the stdlib tier
// is ported here. Aura's ingest sidecar (services/ingest/extract.py, iscc-tika + LibreOffice)
// covers more formats than anydoc and runs faster (measured: 15/16 formats, 18x), but it is wired
// as a CocoIndex pipeline stage over the document CATALOG — invoked per-file during ingest, not as
// a synchronous per-path RPC a box-side tool can call for an arbitrary /workspace file. Building a
// second protocol against it to serve this one call is exactly the bespoke-adapter the "stop before
// bespoke" rule in CLAUDE.md exists to block, so read_file's document support stops at the three
// formats hermes itself decodes without an external converter.
//
// extractableExtensions mirrors read_extract.EXTRACTABLE_EXTENSIONS.
var extractableExtensions = map[string]bool{".ipynb": true, ".docx": true, ".xlsx": true}

// isExtractableDocument reports whether path names a format read_file can decode to text.
func isExtractableDocument(path string) bool {
	return extractableExtensions[strings.ToLower(pathpkg.Ext(path))]
}

// extractionError is returned when a supported-looking document cannot be rendered as text;
// callers fall back to the normal binary-refusal path (mirrors read_extract.ExtractionError).
type extractionError struct{ msg string }

func (e *extractionError) Error() string { return e.msg }

func extractErr(format string, a ...any) error {
	return &extractionError{msg: fmt.Sprintf(format, a...)}
}

// extractDocumentText decodes raw file bytes (as read by boxReadFileRaw, which does NOT refuse
// binary content) into plain text, by extension. Callers must check isExtractableDocument first.
func extractDocumentText(path string, raw []byte) (string, error) {
	switch strings.ToLower(pathpkg.Ext(path)) {
	case ".ipynb":
		return extractNotebook(raw)
	case ".docx":
		return extractDocx(raw)
	case ".xlsx":
		return extractXlsx(raw)
	default:
		return "", extractErr("unsupported document type: %q", path)
	}
}

// ---------------------------------------------------------------------------
// Jupyter notebook (.ipynb) — plain JSON, no zip/XML involved.
// ---------------------------------------------------------------------------

type ipynbCell struct {
	CellType string `json:"cell_type"`
	Source   any    `json:"source"`
}

type ipynbWorksheet struct {
	Cells []ipynbCell `json:"cells"`
}

type ipynbDoc struct {
	Cells      []ipynbCell      `json:"cells"`
	Worksheets []ipynbWorksheet `json:"worksheets"`
}

func sourceText(source any) string {
	switch v := source.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			if s, ok := item.(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func extractNotebook(raw []byte) (string, error) {
	var nb ipynbDoc
	if err := json.Unmarshal(raw, &nb); err != nil {
		return "", extractErr("not a valid notebook: %v", err)
	}
	cells := nb.Cells
	if cells == nil {
		for _, ws := range nb.Worksheets {
			cells = append(cells, ws.Cells...)
		}
	}
	if len(cells) == 0 {
		return "", extractErr("notebook contains no cells")
	}
	labels := map[string]string{"markdown": "Markdown", "code": "Code", "raw": "Raw"}
	counts := map[string]int{}
	var out []string
	for _, cell := range cells {
		label, ok := labels[cell.CellType]
		if !ok {
			continue
		}
		counts[cell.CellType]++
		suffix := fmt.Sprintf(" %d", counts[cell.CellType])
		if cell.CellType == "raw" {
			suffix = ""
		}
		out = append(out, fmt.Sprintf("# ── %s cell%s ──", label, suffix),
			strings.TrimRight(sourceText(cell.Source), "\n"), "")
	}
	if len(out) == 0 {
		return "", extractErr("notebook contains no readable cells")
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", nil
}

// ---------------------------------------------------------------------------
// Minimal XML tree — Go's encoding/xml has no ElementTree-style descendant
// iteration, and docx/xlsx extraction needs it (w:t / s:t can sit several
// levels below the node we're scanning, inside runs/hyperlinks/etc). This
// walks the token stream once into a small in-memory tree, then
// xmlDescendants(node, space, local) mirrors ElementTree.iter(tag).
// ---------------------------------------------------------------------------

type xmlNode struct {
	space, local string
	attrs        map[string]string
	text         string
	children     []*xmlNode
}

func parseXMLTree(data []byte) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{space: t.Name.Space, local: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty or malformed XML")
	}
	return root, nil
}

// xmlDescendants collects n and every descendant whose (space, local) matches, in document order —
// the Go equivalent of ElementTree's elem.iter(tag).
func xmlDescendants(n *xmlNode, space, local string) []*xmlNode {
	var out []*xmlNode
	var walk func(*xmlNode)
	walk = func(node *xmlNode) {
		if node.space == space && node.local == local {
			out = append(out, node)
		}
		for _, c := range node.children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func readZipXML(zr *zip.Reader, name string) (*xmlNode, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, extractErr("missing %s", name)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, extractErr("reading %s: %v", name, err)
	}
	root, err := parseXMLTree(data)
	if err != nil {
		return nil, extractErr("malformed XML in %s: %v", name, err)
	}
	return root, nil
}

// ---------------------------------------------------------------------------
// Word document (.docx)
// ---------------------------------------------------------------------------

const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

func extractDocx(raw []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", extractErr("not a valid DOCX: %v", err)
	}
	root, err := readZipXML(zr, "word/document.xml")
	if err != nil {
		return "", err
	}

	var lines []string
	for _, para := range xmlDescendants(root, wordNS, "p") {
		var buf strings.Builder
		var walk func(*xmlNode)
		walk = func(n *xmlNode) {
			switch {
			case n.space == wordNS && n.local == "t":
				buf.WriteString(n.text)
			case n.space == wordNS && n.local == "tab":
				buf.WriteByte('\t')
			case n.space == wordNS && (n.local == "br" || n.local == "cr"):
				buf.WriteByte('\n')
			}
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(para)
		lines = append(lines, strings.Split(buf.String(), "\n")...)
	}

	anyText := false
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			anyText = true
			break
		}
	}
	if !anyText {
		return "", extractErr("DOCX contains no extractable text")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n", nil
}

// colIndex converts a spreadsheet cell reference's leading letters ("B3" -> "B") to a 0-based
// column index (A=0, B=1, ..., Z=25, AA=26, ...).
func colIndex(ref string) int {
	idx := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' {
			if ch >= 'a' && ch <= 'z' {
				ch -= 'a' - 'A'
			} else {
				break
			}
		}
		idx = idx*26 + int(ch-'A'+1)
	}
	if idx == 0 {
		return 0
	}
	return idx - 1
}
