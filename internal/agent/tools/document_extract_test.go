package tools

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// read_file's document extraction is pure: bytes in, text out, no box and no daemon. The
// container-gated tier contributes ZERO coverage (CLAUDE.md), so everything provable here has
// to be proved here or it is not proved at all — and this is the surface that decides whether
// an .xlsx reads as a spreadsheet or as a binary refusal.

// zipOf builds an in-memory OOXML package from name->content pairs.
func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestIsExtractableDocument(t *testing.T) {
	for _, ok := range []string{"/workspace/a.docx", "/workspace/a.xlsx", "/workspace/a.ipynb", "/w/A.DOCX"} {
		if !isExtractableDocument(ok) {
			t.Errorf("%q should be extractable", ok)
		}
	}
	// A format this build cannot render must NOT claim it can: read_file then falls through to
	// the binary refusal that names what to use instead, rather than returning empty "text".
	for _, no := range []string{"/workspace/a.pdf", "/workspace/a.doc", "/workspace/a.png", "/workspace/a.txt"} {
		if isExtractableDocument(no) {
			t.Errorf("%q must not be treated as extractable", no)
		}
	}
}

func TestExtractDocxRendersParagraphsTabsAndBreaks(t *testing.T) {
	doc := `<?xml version="1.0"?>
<w:document xmlns:w="` + wordNS + `"><w:body>
  <w:p><w:r><w:t>Contratto di locazione</w:t></w:r></w:p>
  <w:p><w:r><w:t>canone</w:t><w:tab/><w:t>850 euro</w:t></w:r></w:p>
  <w:p><w:r><w:t>prima</w:t><w:br/><w:t>seconda</w:t></w:r></w:p>
</w:body></w:document>`
	got, err := extractDocumentText("/workspace/x.docx", zipOf(t, map[string]string{"word/document.xml": doc}))
	if err != nil {
		t.Fatalf("extract docx: %v", err)
	}
	want := "Contratto di locazione\ncanone\t850 euro\nprima\nseconda\n"
	if got != want {
		t.Errorf("docx text = %q, want %q", got, want)
	}
}

func TestExtractDocxRefusesWhenThereIsNoText(t *testing.T) {
	// A DOCX whose paragraphs carry no text must FAIL, not return "". read_file distinguishes
	// the two: an error falls back to the binary path, an empty string would be reported as a
	// successfully-read empty document.
	doc := `<w:document xmlns:w="` + wordNS + `"><w:body><w:p><w:r/></w:p></w:body></w:document>`
	if _, err := extractDocumentText("/workspace/x.docx", zipOf(t, map[string]string{"word/document.xml": doc})); err == nil {
		t.Fatal("a textless DOCX must error, not return empty text")
	}
}

func TestExtractDocxRejectsNonZipAndMissingPart(t *testing.T) {
	if _, err := extractDocumentText("/workspace/x.docx", []byte("not a zip at all")); err == nil {
		t.Error("garbage bytes must not parse as DOCX")
	}
	if _, err := extractDocumentText("/workspace/x.docx", zipOf(t, map[string]string{"other.xml": "<a/>"})); err == nil {
		t.Error("a zip without word/document.xml must error")
	}
}

// xlsxPackage assembles the four parts a workbook needs, so each test states only what it varies.
func xlsxPackage(sheetXML, sharedXML, sheetName, state string) map[string]string {
	parts := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="` + spreadsheetNS + `"><sheets>` +
			`<sheet name="` + sheetName + `" state="` + state + `" id="rId1"/>` +
			`</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="` + packageRelNS + `">` +
			`<Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": sheetXML,
	}
	if sharedXML != "" {
		parts["xl/sharedStrings.xml"] = sharedXML
	}
	return parts
}

func TestExtractXlsxResolvesSharedStringsAndCellTypes(t *testing.T) {
	sheet := `<worksheet xmlns="` + spreadsheetNS + `"><sheetData>
  <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
  <row r="2"><c r="A2"><v>699</v></c><c r="B2" t="inlineStr"><is><t>TORINO</t></is></c></row>
</sheetData></worksheet>`
	shared := `<sst xmlns="` + spreadsheetNS + `"><si><t>citta</t></si><si><t>conteggio</t></si></sst>`

	got, err := extractDocumentText("/workspace/x.xlsx", zipOf(t, xlsxPackage(sheet, shared, "Clienti", "visible")))
	if err != nil {
		t.Fatalf("extract xlsx: %v", err)
	}
	// A shared-string cell holds an INDEX, not the text: if the table is not consulted the
	// spreadsheet renders as "0" and "1" and every value question answers wrongly.
	for _, want := range []string{"citta", "conteggio", "699", "TORINO", "Clienti"} {
		if !strings.Contains(got, want) {
			t.Errorf("xlsx text is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, ">0<") {
		t.Errorf("a shared-string index leaked as a value:\n%s", got)
	}
}

func TestExtractXlsxSkipsHiddenSheets(t *testing.T) {
	sheet := `<worksheet xmlns="` + spreadsheetNS + `"><sheetData>
  <row r="1"><c r="A1" t="inlineStr"><is><t>SEGRETO</t></is></c></row>
</sheetData></worksheet>`
	got, err := extractDocumentText("/workspace/x.xlsx", zipOf(t, xlsxPackage(sheet, "", "Nascosto", "hidden")))
	// Every sheet hidden means nothing to render; either outcome is acceptable, but the hidden
	// content must never appear.
	if err == nil && strings.Contains(got, "SEGRETO") {
		t.Errorf("a hidden sheet's content was rendered:\n%s", got)
	}
}

func TestExtractXlsxRejectsNonZipAndMissingWorkbook(t *testing.T) {
	if _, err := extractDocumentText("/workspace/x.xlsx", []byte("PK-not-really")); err == nil {
		t.Error("garbage bytes must not parse as XLSX")
	}
	if _, err := extractDocumentText("/workspace/x.xlsx", zipOf(t, map[string]string{"docProps/app.xml": "<a/>"})); err == nil {
		t.Error("a zip without xl/workbook.xml must error")
	}
}

func TestXlsxSheetPartNormalisesRelationshipTargets(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"worksheets/sheet1.xml", "xl/worksheets/sheet1.xml"},
		{"/xl/worksheets/sheet1.xml", "xl/worksheets/sheet1.xml"},
		{"./worksheets/sheet1.xml", "xl/worksheets/sheet1.xml"},
		// A target trying to climb out of xl/ must be flattened, not followed.
		{"../xl/worksheets/sheet1.xml", "xl/worksheets/sheet1.xml"},
	} {
		if got := xlsxSheetPart(tc.in); got != tc.want {
			t.Errorf("xlsxSheetPart(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestColIndexMapsSpreadsheetColumns(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want int
	}{{"A1", 0}, {"B3", 1}, {"Z9", 25}, {"AA1", 26}, {"AB2", 27}} {
		if got := colIndex(tc.ref); got != tc.want {
			t.Errorf("colIndex(%q) = %d, want %d", tc.ref, got, tc.want)
		}
	}
}

func TestExtractNotebookRendersCellsByType(t *testing.T) {
	nb := `{"cells":[
	  {"cell_type":"markdown","source":["# Titolo\n"]},
	  {"cell_type":"code","source":["print('ciao')\n"]},
	  {"cell_type":"raw","source":["grezzo\n"]},
	  {"cell_type":"sconosciuto","source":["ignorami\n"]}
	]}`
	got, err := extractDocumentText("/workspace/n.ipynb", []byte(nb))
	if err != nil {
		t.Fatalf("extract notebook: %v", err)
	}
	for _, want := range []string{"# Titolo", "print('ciao')", "grezzo"} {
		if !strings.Contains(got, want) {
			t.Errorf("notebook text is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ignorami") {
		t.Errorf("an unknown cell type was rendered:\n%s", got)
	}
}

func TestExtractNotebookRejectsBadInput(t *testing.T) {
	if _, err := extractDocumentText("/workspace/n.ipynb", []byte("{not json")); err == nil {
		t.Error("invalid JSON must not parse as a notebook")
	}
	if _, err := extractDocumentText("/workspace/n.ipynb", []byte(`{"cells":[]}`)); err == nil {
		t.Error("a notebook with no cells must error rather than return empty text")
	}
}
