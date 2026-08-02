package filecard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Read caps. They bound what the builder READS, not merely what it keeps: a
// 100 MB spreadsheet must not turn ingest into a batch job, and a zip must not
// be able to inflate into the heap. Every cap that actually bites is stated in
// the card, because a truncated reading that presents itself as complete is
// worse than no card.
const (
	// maxSheetsScanned bounds worksheets read per workbook; the rest are still
	// named in the card, they just carry no column narration.
	maxSheetsScanned = 8
	// maxRowsScanned bounds data rows read per sheet. Postgres samples
	// 300 x statistics_target = 30,000 rows to build most_common_vals; 10,000 is
	// the same order and keeps the whole of every real file in this corpus.
	maxRowsScanned = 10000
	// maxColumnsScanned bounds columns narrated per sheet.
	maxColumnsScanned = 64
	// maxTrackedValues bounds distinct values held per column. Frequencies of the
	// values seen before the ceiling stay exact, and a skewed column's head shows
	// up early, so the top-N by frequency survives the cap; the distinct count
	// past it is reported as a floor, not a number.
	maxTrackedValues = 512
	// maxSharedStringBytes bounds the shared-string table read from a workbook.
	maxSharedStringBytes = 8 << 20
	// maxTextChars bounds prose read from one document part.
	maxTextChars = 200 << 10
	// maxProseWords bounds prose KEPT from a document.
	maxProseWords = 140
	// maxHeadings bounds headings kept from a document.
	maxHeadings = 12
	// mcvPerColumn is the per-column most-common-values list length. Postgres'
	// default is 100 per column for a query planner that must estimate
	// selectivity; a retrieval card is read by an embedder with a 2048-token
	// window, so the head is kept and the tail is left to min/max.
	mcvPerColumn = 6
	// sampleRows is how many whole rows are kept, serialized as `header: value`.
	// Pneuma's default is r=5; Table Serialization Kitchen measures a plateau by
	// ~10 rows and a LOSS past it on short-context embedders.
	sampleRows = 5
)

// Request names the file to describe. FileName is the name the library shows,
// which is not always the name on disk: an asset upload lands in a temp file.
type Request struct {
	Path      string
	FileName  string
	SizeBytes int64
}

// Build describes one file. It always returns a usable card: when the file
// cannot be parsed the card degrades to what is certain — name, kind, size —
// and SAYS so in a caveat. A non-nil error accompanies a degraded card so the
// caller can log the reason; ingest must not fail because a zip was corrupt.
func Build(req Request) (Card, error) {
	req = req.normalize()
	kind, handler := handlerFor(req.ext())
	card := Card{FileName: req.FileName, Kind: kind, SizeBytes: req.SizeBytes}
	if handler == nil {
		card.Caveats = append(card.Caveats,
			"Aura did not read this file at ingest: nothing here describes its contents.")
		return card, nil
	}
	built, err := handler(req, card)
	if err != nil {
		card.Caveats = append(card.Caveats,
			fmt.Sprintf("Aura could not read this file at ingest (%v); only its name and size are described here.", err))
		return card, err
	}
	return built, nil
}

func (r Request) normalize() Request {
	if strings.TrimSpace(r.FileName) == "" {
		r.FileName = filepath.Base(r.Path)
	}
	if r.SizeBytes <= 0 {
		if info, err := os.Stat(r.Path); err == nil {
			r.SizeBytes = info.Size()
		}
	}
	return r
}

func (r Request) ext() string {
	if ext := strings.ToLower(filepath.Ext(r.FileName)); ext != "" {
		return ext
	}
	return strings.ToLower(filepath.Ext(r.Path))
}

type handlerFunc func(req Request, card Card) (Card, error)

// handlerFor maps an extension to its kind and reader. The extension list is the
// ingest allowlist (internal/documents/extensions.go); a type that reaches here
// without a reader gets an honest card rather than a lie.
func handlerFor(ext string) (Kind, handlerFunc) {
	switch ext {
	case ".xlsx", ".xlsm":
		return KindSpreadsheet, buildXLSX
	case ".csv":
		return KindTable, buildCSV
	case ".docx":
		return KindDocument, buildDOCX
	case ".pptx":
		return KindPresentation, buildPPTX
	case ".epub":
		return KindBook, buildEPUB
	case ".html", ".htm":
		return KindPage, buildHTML
	case ".md", ".markdown", ".txt", ".json", ".xml":
		return KindText, buildPlainText
	case ".pdf":
		return KindPDF, buildPDF
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return KindImage, buildImage
	default:
		return KindFile, nil
	}
}
