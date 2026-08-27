package filecard

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile puts a fixture on disk under the test's own directory.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeZip builds an OOXML-shaped fixture: a zip of named XML parts.
func writeZip(t *testing.T, name string, parts map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.Create(path) //nolint:gosec // path is the test's own temp dir.
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for entry, body := range parts {
		part, err := writer.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func build(t *testing.T, path, name string) Card {
	t.Helper()
	card, err := Build(Request{Path: path, FileName: name})
	if err != nil {
		t.Fatalf("Build(%s) error = %v", name, err)
	}
	return card
}

const sharedStringsXML = `<sst count="10" uniqueCount="8">
<si><t>Area</t></si><si><t>Localita</t></si><si><t>Cliente</t></si>
<si><t>NORD</t></si><si><t>TORINO</t></si><si><t>ROSSI SPA</t></si>
<si><t>GENOVA</t></si><si><t>BIANCHI SRL</t></si></sst>`

// sheetXML lays out three data rows under a header row, with a numeric column
// so the type split is exercised.
const sheetXML = `<worksheet><dimension ref="A1:D4"/><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="inlineStr"><is><t>Fatturato</t></is></c></row>
<row r="2"><c r="A2" t="s"><v>3</v></c><c r="B2" t="s"><v>4</v></c><c r="C2" t="s"><v>5</v></c><c r="D2"><v>1200.5</v></c></row>
<row r="3"><c r="A3" t="s"><v>3</v></c><c r="B3" t="s"><v>4</v></c><c r="C3" t="s"><v>7</v></c><c r="D3"><v>800</v></c></row>
<row r="4"><c r="A4" t="s"><v>3</v></c><c r="B4" t="s"><v>6</v></c><c r="C4" t="s"><v>5</v></c><c r="D4"><v>9000</v></c></row>
</sheetData></worksheet>`

func workbookFixture(t *testing.T, name string) string {
	t.Helper()
	return writeZip(t, name, map[string]string{
		"xl/workbook.xml": `<workbook><sheets>` +
			`<sheet name="Clienti" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships>` +
			`<Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": sheetXML,
		"xl/sharedStrings.xml":     sharedStringsXML,
		"docProps/core.xml":        `<coreProperties><dc:title>Anagrafica clienti</dc:title></coreProperties>`,
	})
}

func TestWorkbookCardCarriesStructureValuesAndSampledRows(t *testing.T) {
	card := build(t, workbookFixture(t, "clienti.xlsx"), "clienti.xlsx")
	rendered := card.Render()

	for _, want := range []string{
		"clienti.xlsx",                           // the file's own name
		"spreadsheet",                            // its kind
		"Anagrafica clienti",                     // the title the file declares
		`Sheet "Clienti"`,                        // the sheet name, which the zip entry name does not give
		"3 rows x 4 columns",                     // the dimension minus the header row
		"Area, Localita, Cliente",                // column narration
		"Fatturato",                              // an inline string, not a shared one
		"most common TORINO (2)",                 // frequency selection, not alphabetical
		"from 800 to 9000",                       // the numeric tail sketch
		"Localita: TORINO",                       // a sampled row as header: value
		"describes the file and is not the file", // the card never claims to BE the file
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("card is missing %q\n%s", want, rendered)
		}
	}
}

// A value that never repeats is not a category. Without the floor a column of
// 5,000 company names contributes six arbitrary names, which is the alphabetical
// cap this card was built to replace.
func TestColumnNarrationDropsValuesThatDoNotRepeat(t *testing.T) {
	card := build(t, workbookFixture(t, "clienti.xlsx"), "clienti.xlsx")
	var cliente Column
	for _, col := range card.Sheets[0].Columns {
		if col.Header == "Cliente" {
			cliente = col
		}
	}
	if cliente.Distinct != 2 {
		t.Fatalf("Cliente distinct = %d, want 2", cliente.Distinct)
	}
	if len(cliente.Common) != 1 || cliente.Common[0].Text != "ROSSI SPA" {
		t.Fatalf("Cliente common = %#v, want only the repeated value", cliente.Common)
	}
}

func TestWorkbookWithoutCellDataSaysSoInsteadOfLookingEmpty(t *testing.T) {
	path := writeZip(t, "drawings.xlsx", map[string]string{
		"xl/workbook.xml": `<workbook><sheets>` +
			`<sheet name="Main" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships>` +
			`<Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet><dimension ref="A1"/><sheetData/></worksheet>`,
	})
	rendered := build(t, path, "drawings.xlsx").Render()
	if !strings.Contains(rendered, "no cell data") {
		t.Errorf("card hides that the workbook is empty:\n%s", rendered)
	}
}

func TestCSVCardSniffsSemicolonsAndItalianDecimals(t *testing.T) {
	path := writeFile(t, "vendite.csv", strings.Join([]string{
		"Prodotto;Citta;Importo",
		"Cavo;TORINO;1.200,50",
		"Cavo;TORINO;800,00",
		"Quadro;GENOVA;9.000,00",
	}, "\n"))
	card := build(t, path, "vendite.csv")
	rendered := card.Render()

	if !strings.Contains(rendered, "Prodotto, Citta, Importo") {
		t.Errorf("semicolons were not detected as the delimiter:\n%s", rendered)
	}
	if !strings.Contains(rendered, "from 800 to 9000") {
		t.Errorf("Italian decimals were read as text:\n%s", rendered)
	}
	if !strings.Contains(rendered, "most common TORINO (2)") {
		t.Errorf("frequent values are missing:\n%s", rendered)
	}
}

// A banner above the table is the normal shape of a real spreadsheet: fewer than
// 3% carry a predefined data model. Taking row 1 on faith mislabels every column.
func TestHeaderIsFoundBelowABannerRow(t *testing.T) {
	path := writeFile(t, "listino.csv", strings.Join([]string{
		"Listino fornitori 2026",
		"",
		"Codice,Fornitore,Prezzo",
		"A1,ACME SPA,10",
		"A2,ACME SPA,20",
	}, "\n"))
	rendered := build(t, path, "listino.csv").Render()
	if !strings.Contains(rendered, "Codice, Fornitore, Prezzo") {
		t.Errorf("the banner row was taken as the header:\n%s", rendered)
	}
}

func TestWorkbookRowCountUsesPhysicalHeaderPosition(t *testing.T) {
	path := writeZip(t, "listino.xlsx", map[string]string{
		"xl/workbook.xml": `<workbook><sheets>` +
			`<sheet name="Listino" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships>` +
			`<Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet><dimension ref="A1:B4"/><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Listino 2026</t></is></c></row>` +
			`<row r="3"><c r="A3" t="inlineStr"><is><t>Codice</t></is></c>` +
			`<c r="B3" t="inlineStr"><is><t>Prezzo</t></is></c></row>` +
			`<row r="4"><c r="A4" t="inlineStr"><is><t>A1</t></is></c><c r="B4"><v>10</v></c></row>` +
			`</sheetData></worksheet>`,
	})

	sheet := build(t, path, "listino.xlsx").Sheets[0]
	if sheet.Rows != 1 {
		t.Fatalf("rows after physical header row = %d, want 1", sheet.Rows)
	}
}

// When nothing reads as labels the sheet is headerless, and the first data row
// must survive as data rather than being consumed as column names.
func TestHeaderlessTableKeepsItsFirstRow(t *testing.T) {
	path := writeFile(t, "fornitori.csv", strings.Join([]string{
		"SIEMENS SPA,573",
		"SCHNEIDER SPA,639",
		"ABB SPA,7112",
	}, "\n"))
	card := build(t, path, "fornitori.csv")
	rendered := card.Render()
	if !strings.Contains(rendered, "no header row") {
		t.Errorf("a data row was taken as the header:\n%s", rendered)
	}
	if card.Sheets[0].RowsScanned != 3 {
		t.Errorf("rows scanned = %d, want all 3", card.Sheets[0].RowsScanned)
	}
	if !strings.Contains(rendered, "SIEMENS SPA") {
		t.Errorf("the first row was eaten as a header:\n%s", rendered)
	}
}

func TestDocxCardCarriesTitleAndOpeningWords(t *testing.T) {
	path := writeZip(t, "corso.docx", map[string]string{
		"docProps/core.xml": `<coreProperties><dc:title>Corso Base Robot</dc:title>` +
			`<dc:creator>Davide</dc:creator></coreProperties>`,
		"word/document.xml": `<document><body>` +
			`<p><r><t>Corso Base Robot</t></r></p>` +
			`<p><r><t>I giunti del robot e i sistemi di riferimento.</t></r></p>` +
			`</body></document>`,
	})
	rendered := build(t, path, "corso.docx").Render()
	for _, want := range []string{"Corso Base Robot", "Author: Davide", "giunti del robot"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("docx card is missing %q\n%s", want, rendered)
		}
	}
}

func TestPptxCardListsSlideTitlesInSlideOrder(t *testing.T) {
	slide := func(title string) string {
		return `<sld><cSld><spTree><sp><txBody><a:p><a:r><a:t>` + title + `</a:t></a:r></a:p></txBody></sp></spTree></cSld></sld>`
	}
	parts := map[string]string{
		"ppt/slides/slide1.xml":  slide("Il Robot"),
		"ppt/slides/slide2.xml":  slide("Scara"),
		"ppt/slides/slide10.xml": slide("La singolarita"),
	}
	rendered := build(t, writeZip(t, "robot.pptx", parts), "robot.pptx").Render()
	if !strings.Contains(rendered, "3 slides") {
		t.Errorf("slide count missing:\n%s", rendered)
	}
	// Lexical order would put slide10 second; a deck's tenth slide is not its second.
	if !strings.Contains(rendered, "Slides: Il Robot; Scara; La singolarita") {
		t.Errorf("slides are out of order:\n%s", rendered)
	}
}

func TestHTMLCardKeepsHeadingsAndDropsNavigation(t *testing.T) {
	path := writeFile(t, "voce.html", `<html><head><title>Apprendimento statistico</title></head>`+
		`<body><nav><ul><li>Vai al contenuto</li><li>Menu principale</li></ul></nav>`+
		`<h1>Apprendimento statistico</h1><h2>Storia</h2>`+
		`<p>Sottoinsieme dell'intelligenza artificiale.</p></body></html>`)
	rendered := build(t, path, "voce.html").Render()
	if !strings.Contains(rendered, "Headings: Apprendimento statistico; Storia") {
		t.Errorf("headings missing:\n%s", rendered)
	}
	if strings.Contains(rendered, "Menu principale") {
		t.Errorf("site chrome leaked into the card:\n%s", rendered)
	}
}

func TestMarkdownCardKeepsItsHeadingOutline(t *testing.T) {
	path := writeFile(t, "note.md", "# Verbale riunione\n\nPresenti: tre persone.\n\n## Decisioni\n\nSi delibera.\n")
	card := build(t, path, "note.md")
	if card.Title != "Verbale riunione" {
		t.Errorf("title = %q, want the first heading", card.Title)
	}
	if !strings.Contains(card.Render(), "Decisioni") {
		t.Errorf("heading outline missing:\n%s", card.Render())
	}
}

// A PDF's words are glyph indices behind a font encoding, so the card takes what
// the file states in the clear and SAYS that the text was not read. A degraded
// card that looks complete is worse than none.
func TestPDFCardTakesMetadataAndAdmitsTheTextIsUnread(t *testing.T) {
	path := writeFile(t, "manuale.pdf", "%PDF-1.7\n"+
		"1 0 obj<</Title (SINAMICS G220 converter)/Author (Siemens)>>endobj\n"+
		"2 0 obj<</Type /Page>>endobj\n3 0 obj<</Type /Page>>endobj\n"+
		"4 0 obj<</Type /Pages /Count 2>>endobj\n%%EOF\n")
	card := build(t, path, "manuale.pdf")
	rendered := card.Render()
	if card.Title != "SINAMICS G220 converter" {
		t.Errorf("title = %q, want the Info dictionary title", card.Title)
	}
	for _, want := range []string{"Author: Siemens", "2 pages", "did not read this PDF's text"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("pdf card is missing %q\n%s", want, rendered)
		}
	}
}

func TestPDFWithoutMetadataSaysOnlyTheNameDescribesIt(t *testing.T) {
	path := writeFile(t, "decreto.pdf", "%PDF-1.7\n5 0 obj<</Filter/FlateDecode>>stream\nx\x9c\nendstream\n%%EOF\n")
	rendered := build(t, path, "decreto.pdf").Render()
	if !strings.Contains(rendered, "only its file name says what it is") {
		t.Errorf("a metadata-less PDF pretends to be described:\n%s", rendered)
	}
}

func TestImageCardPointsAtSeparatelyIndexedVisualContent(t *testing.T) {
	path := writeFile(t, "foto.png", "not really a png")
	rendered := build(t, path, "foto.png").Render()
	if !strings.Contains(rendered, "visual content is indexed in separate retrieval passages") {
		t.Errorf("image card hides the media retrieval path:\n%s", rendered)
	}
	if strings.Contains(rendered, "did not look at this image") {
		t.Errorf("image card contradicts media ingest:\n%s", rendered)
	}
}

func TestAudioCardPointsAtSeparatelyIndexedTranscript(t *testing.T) {
	path := writeFile(t, "nota.wav", "not really audio")
	card := build(t, path, "nota.wav")
	if card.Kind != KindAudio {
		t.Errorf("kind = %q, want audio", card.Kind)
	}
	if !strings.Contains(card.Render(), "transcript is indexed in separate retrieval passages") {
		t.Errorf("audio card hides the media retrieval path:\n%s", card.Render())
	}
}

func TestUnreadableFileStillProducesACardAndReportsWhy(t *testing.T) {
	path := writeFile(t, "rotto.xlsx", "this is not a zip")
	card, err := Build(Request{Path: path, FileName: "rotto.xlsx"})
	if err == nil {
		t.Fatal("Build error = nil, want the read failure surfaced to the caller")
	}
	rendered := card.Render()
	if !strings.Contains(rendered, "could not read this file at ingest") {
		t.Errorf("a failed read is not stated in the card:\n%s", rendered)
	}
	if !strings.Contains(rendered, "rotto.xlsx") {
		t.Errorf("card lost the file name:\n%s", rendered)
	}
}

func TestUnknownExtensionGetsAnHonestCard(t *testing.T) {
	path := writeFile(t, "archivio.bin", "binary")
	card := build(t, path, "archivio.bin")
	if card.Kind != KindFile {
		t.Errorf("kind = %q, want the generic file kind", card.Kind)
	}
	if !strings.Contains(card.Render(), "did not read this file at ingest") {
		t.Errorf("unknown type oversells itself:\n%s", card.Render())
	}
}
