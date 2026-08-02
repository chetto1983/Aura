package filecard

import (
	"fmt"
	"strings"
	"testing"
)

// The caps bound what the builder READS, and every cap that bites has to be
// stated in the card. These tests exercise the capped paths, because a cap that
// silently truncates is how a card starts lying about its file.

func TestBigTableIsSampledAndSaysSo(t *testing.T) {
	var b strings.Builder
	b.WriteString("Codice,Citta,Importo\n")
	for i := range maxRowsScanned + headerProbeRows + 50 {
		city := "TORINO"
		if i%3 == 0 {
			city = "GENOVA"
		}
		fmt.Fprintf(&b, "K%06d,%s,%d\n", i, city, i)
	}
	card := build(t, writeFile(t, "grande.csv", b.String()), "grande.csv")
	rendered := card.Render()

	if card.Sheets[0].RowsScanned != maxRowsScanned+headerProbeRows-1 {
		t.Fatalf("rows scanned = %d, want the read cap minus the header row", card.Sheets[0].RowsScanned)
	}
	if !strings.Contains(rendered, "was sampled") {
		t.Errorf("a capped read presents itself as complete:\n%s", rendered)
	}
	// Value tracking is capped per column, and the count past it is a floor.
	if !strings.Contains(rendered, fmt.Sprintf("%d+ distinct values", maxTrackedValues)) {
		t.Errorf("the distinct-value cap is not reported as a floor:\n%s", rendered)
	}
	// Frequencies of values seen before the ceiling stay exact, so the skewed
	// head of the city column survives the cap.
	if !strings.Contains(rendered, "TORINO") || !strings.Contains(rendered, "GENOVA") {
		t.Errorf("the column head was lost to the cap:\n%s", rendered)
	}
	if len(rendered) > MaxCardChars {
		t.Errorf("card is %d chars, cap is %d", len(rendered), MaxCardChars)
	}
	if !strings.HasSuffix(rendered, cardTrailer) {
		t.Errorf("the trailer was truncated away:\n%s", rendered)
	}
}

// Rows are sampled on a doubling stride so the examples come from the whole
// file. Taking the first five would describe a sorted spreadsheet by its first
// page and nothing else.
func TestSampledRowsAreSpreadAcrossTheFile(t *testing.T) {
	var b strings.Builder
	b.WriteString("Codice,Reparto\n")
	for i := range 2000 {
		fmt.Fprintf(&b, "K%04d,R%d\n", i, i/500)
	}
	card := build(t, writeFile(t, "spread.csv", b.String()), "spread.csv")
	samples := card.Sheets[0].Samples
	if len(samples) != sampleRows {
		t.Fatalf("samples = %d, want %d", len(samples), sampleRows)
	}
	first, last := samples[0][0].Value, samples[len(samples)-1][0].Value
	if first >= last {
		t.Fatalf("samples %q..%q are not ordered across the file", first, last)
	}
	if last < "K1000" {
		t.Errorf("every sample came from the head of the file: %q", last)
	}
}

func TestWorkbookBeyondTheSheetCapNamesTheSheetsItSkipped(t *testing.T) {
	parts := map[string]string{
		"xl/sharedStrings.xml": sharedStringsXML,
	}
	var sheets, rels strings.Builder
	total := maxSheetsScanned + 2
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&sheets, `<sheet name="Foglio%d" sheetId="%d" r:id="rId%d"/>`, i, i, i)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
		parts[fmt.Sprintf("xl/worksheets/sheet%d.xml", i)] = sheetXML
	}
	parts["xl/workbook.xml"] = "<workbook><sheets>" + sheets.String() + "</sheets></workbook>"
	parts["xl/_rels/workbook.xml.rels"] = "<Relationships>" + rels.String() + "</Relationships>"

	card := build(t, writeZip(t, "molti.xlsx", parts), "molti.xlsx")
	rendered := card.Render()
	if !strings.Contains(rendered, fmt.Sprintf("Only the first %d of %d sheets", maxSheetsScanned, total)) {
		t.Errorf("the sheet cap is not stated:\n%s", rendered)
	}
	if !strings.Contains(rendered, fmt.Sprintf("Foglio%d", total)) {
		t.Errorf("a skipped sheet is not even named:\n%s", rendered)
	}
	// The budget is split between sheets, so the last sheet read is described
	// rather than starved by the first.
	if !strings.Contains(rendered, fmt.Sprintf(`Sheet "Foglio%d"`, maxSheetsScanned)) {
		t.Errorf("later sheets were starved of budget:\n%s", rendered)
	}
	if len(rendered) > MaxCardChars {
		t.Errorf("card is %d chars, cap is %d", len(rendered), MaxCardChars)
	}
}

func TestLongCellValuesAreTruncatedNotDropped(t *testing.T) {
	long := strings.Repeat("descrizione ", 40)
	path := writeFile(t, "lungo.csv", "Codice,Nota\nA1,"+long+"\nA2,"+long+"\n")
	rendered := build(t, path, "lungo.csv").Render()
	if !strings.Contains(rendered, "…") {
		t.Errorf("a long value was not truncated:\n%s", rendered)
	}
	if !strings.Contains(rendered, "descrizione") {
		t.Errorf("a long value was dropped entirely:\n%s", rendered)
	}
}

func TestHeadlineReportsSizeInHumanUnits(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{size: 0, want: "file"},
		{size: 900, want: "900 bytes"},
		{size: 4096, want: "4 KB"},
		{size: 5 << 20, want: "5 MB"},
	} {
		card := Card{FileName: "x.bin", Kind: KindFile, SizeBytes: tc.size}
		if !strings.Contains(card.Render(), tc.want) {
			t.Errorf("size %d rendered as %q, want %q", tc.size, card.headline(), tc.want)
		}
	}
}

const epubOPF = `<package><metadata>
<dc:title>Diario ultimo</dc:title>
<dc:creator>Daniele Imperi</dc:creator>
<dc:subject>Narrativa</dc:subject>
<dc:subject>Sopravvivenza</dc:subject>
<dc:description>Un racconto ispirato al Survival Blog</dc:description>
</metadata><manifest>
<item id="cover" href="text/cover.xhtml" media-type="application/xhtml+xml"/>
<item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
</manifest><spine><itemref idref="ch1"/><itemref idref="cover"/></spine></package>`

func TestEPUBCardTakesPackageMetadataAndTheFirstSpineDocument(t *testing.T) {
	path := writeZip(t, "diario.epub", map[string]string{
		"META-INF/container.xml": `<container><rootfiles>` +
			`<rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>` +
			`</rootfiles></container>`,
		"OEBPS/content.opf": epubOPF,
		// The reading order starts at ch1, not at the first manifest entry.
		"OEBPS/text/ch1.xhtml": `<html><head><style>@page {margin:0}</style></head>` +
			`<body><h1>Il primo giorno</h1><p>La radio taceva da tre settimane.</p></body></html>`,
		"OEBPS/text/cover.xhtml": `<html><body><p>Cover</p></body></html>`,
	})
	rendered := build(t, path, "diario.epub").Render()
	for _, want := range []string{
		"Diario ultimo",
		"Author: Daniele Imperi",
		"Narrativa, Sopravvivenza",
		"La radio taceva",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("epub card is missing %q\n%s", want, rendered)
		}
	}
	// A stylesheet is machinery, not the opening of the book.
	if strings.Contains(rendered, "@page") {
		t.Errorf("CSS leaked into the card:\n%s", rendered)
	}
}

func TestEPUBWithoutAPackageDocumentSaysSo(t *testing.T) {
	path := writeZip(t, "vuoto.epub", map[string]string{"mimetype": "application/epub+zip"})
	rendered := build(t, path, "vuoto.epub").Render()
	if !strings.Contains(rendered, "declares no package document") {
		t.Errorf("a broken epub pretends to be described:\n%s", rendered)
	}
}

// PDF text strings come in three encodings, and getting this wrong renders a
// title as "S i e m e n s" — which is what the raw UTF-16 bytes look like.
func TestPDFTitleDecodesHexAndUTF16(t *testing.T) {
	path := writeFile(t, "hex.pdf", "%PDF-1.7\n1 0 obj<</Title <FEFF00430069006106F>>>endobj\n%%EOF\n")
	if card := build(t, path, "hex.pdf"); card.Title == "" {
		t.Errorf("a hex-encoded title was dropped: %#v", card)
	}
	utf16Title := "%PDF-1.7\n1 0 obj<</Title (\xFE\xFF\x00S\x00i\x00e\x00m\x00e\x00n\x00s)>>endobj\n%%EOF\n"
	card := build(t, writeFile(t, "utf16.pdf", utf16Title), "utf16.pdf")
	if card.Title != "Siemens" {
		t.Errorf("title = %q, want the UTF-16BE string decoded", card.Title)
	}
}

func TestPDFFallsBackToXMPWhenThereIsNoInfoDictionary(t *testing.T) {
	path := writeFile(t, "xmp.pdf", "%PDF-1.5\n1 0 obj<</Type/Metadata>>stream\n"+
		`<x:xmpmeta><rdf:RDF><rdf:Description>`+
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Manuale operativo</rdf:li></rdf:Alt></dc:title>`+
		`<pdf:Keywords>inverter</pdf:Keywords>`+
		`</rdf:Description></rdf:RDF></x:xmpmeta>`+"\nendstream\n%%EOF\n")
	card := build(t, path, "xmp.pdf")
	if card.Title != "Manuale operativo" {
		t.Errorf("title = %q, want the XMP title", card.Title)
	}
	if !strings.Contains(card.Render(), "Keywords: inverter") {
		t.Errorf("XMP keywords missing:\n%s", card.Render())
	}
}

// A linearized PDF states its page count in the clear; otherwise page objects
// are counted, and a producer that packs them into compressed object streams
// makes that count a floor, which the card says out loud.
func TestLinearizedPDFTrustsItsDeclaredPageCount(t *testing.T) {
	path := writeFile(t, "lin.pdf", "%PDF-1.6\n572 0 obj<</Linearized 1/L 478600/O 574/N 96>>endobj\n%%EOF\n")
	rendered := build(t, path, "lin.pdf").Render()
	if !strings.Contains(rendered, "96 pages") {
		t.Errorf("the declared page count was ignored:\n%s", rendered)
	}
}

func TestColumnReferencesPlaceValuesInTheirOwnColumn(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want int
	}{
		{ref: "A1", want: 0},
		{ref: "Z9", want: 25},
		{ref: "AA1", want: 26},
		{ref: "AB12", want: 27},
		{ref: "$C$3", want: 2},
		{ref: "12", want: -1},
		{ref: "", want: -1},
	} {
		if got := columnIndex(tc.ref); got != tc.want {
			t.Errorf("columnIndex(%q) = %d, want %d", tc.ref, got, tc.want)
		}
	}
}

// A gap in the middle of a row must not shift every later value one column left.
func TestSparseRowKeepsItsColumnAlignment(t *testing.T) {
	parts := map[string]string{
		"xl/workbook.xml": `<workbook><sheets>` +
			`<sheet name="Sparsa" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships>` +
			`<Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet><dimension ref="A1:C3"/><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Codice</t></is></c>` +
			`<c r="B1" t="inlineStr"><is><t>Nota</t></is></c>` +
			`<c r="C1" t="inlineStr"><is><t>Citta</t></is></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>A1</t></is></c>` +
			`<c r="C2" t="inlineStr"><is><t>TORINO</t></is></c></row>` +
			`<row r="3"><c r="A3" t="inlineStr"><is><t>A2</t></is></c>` +
			`<c r="C3" t="inlineStr"><is><t>TORINO</t></is></c></row>` +
			`</sheetData></worksheet>`,
	}
	card := build(t, writeZip(t, "sparsa.xlsx", parts), "sparsa.xlsx")
	rendered := card.Render()
	if !strings.Contains(rendered, "Citta: text, 1 distinct value, most common TORINO (2)") {
		t.Errorf("a skipped cell shifted the row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Citta: TORINO") {
		t.Errorf("sampled row lost its alignment:\n%s", rendered)
	}
}
