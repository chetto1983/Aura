package docfields

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/chetto1983/aura/internal/documents/filecard"
)

func TestFileTypeQueryValidation(t *testing.T) {
	for _, kind := range []string{"", "all", "documents", "pdf", "word", "spreadsheet", "presentation", "text", "image", "video", "audio", "code"} {
		t.Run(kind, func(t *testing.T) {
			values := url.Values{"file_type": {kind}}
			want := kind
			if want == "" {
				want = "documents"
			}
			if q, err := ParseListQuery(values); err != nil || q.FileType != want {
				t.Fatalf("query %q: %v", kind, err)
			}
			view := SavedView{Name: "Filtered", Version: 1, Display: "table", Query: values.Encode()}
			if err := validateView(&view); err != nil {
				t.Fatalf("saved query %q: %v", kind, err)
			}
		})
	}
	if q, err := ParseListQuery(url.Values{}); err != nil || q.FileType != "documents" {
		t.Fatalf("omitted file type=%q %v", q.FileType, err)
	}
	for _, kind := range []string{"other", "PDF", "pictures", "documents' OR 1=1", " documents"} {
		if _, err := ParseListQuery(url.Values{"file_type": {kind}}); !errors.Is(err, ErrInvalid) {
			t.Errorf("file type %q: %v", kind, err)
		}
	}
	view := SavedView{Name: "Duplicated", Version: 1, Display: "table", Query: "file_type=pdf&file_type=audio"}
	if err := validateView(&view); !errors.Is(err, ErrInvalid) {
		t.Errorf("repeated saved file type: %v", err)
	}
}

func TestFileTypeIncludesEveryIngestSourceExtension(t *testing.T) {
	for _, extension := range filecard.SourceExtensions() {
		if got := fileTypeFromName("source" + extension); got != "code" {
			t.Errorf("source %s classified as %s", extension, got)
		}
	}
}

func TestDocumentRowInfersFileTypeFromFinalExtension(t *testing.T) {
	for _, c := range []struct{ filename, want string }{
		{"report.final.PDF", "pdf"}, {"folder.pdf/scan.PNG", "image"},
		{"letter.DoCx", "word"}, {"letter.docm", "word"}, {"letter.dotx", "word"}, {"letter.dotm", "word"}, {"letter.dot", "word"}, {"letter.odt", "word"}, {"letter.ott", "word"}, {"letter.rtf", "word"},
		{"budget.XLSX", "spreadsheet"}, {"budget.xls", "spreadsheet"}, {"budget.xlsm", "spreadsheet"}, {"budget.xlsb", "spreadsheet"}, {"budget.xlt", "spreadsheet"}, {"budget.xltx", "spreadsheet"}, {"budget.xltm", "spreadsheet"}, {"budget.ods", "spreadsheet"}, {"budget.ots", "spreadsheet"}, {"budget.csv", "spreadsheet"}, {"budget.tsv", "spreadsheet"},
		{"slides.PPTX", "presentation"}, {"slides.ppt", "presentation"}, {"slides.pptm", "presentation"}, {"slides.pps", "presentation"}, {"slides.ppsx", "presentation"}, {"slides.ppsm", "presentation"}, {"slides.pot", "presentation"}, {"slides.potx", "presentation"}, {"slides.potm", "presentation"}, {"slides.odp", "presentation"}, {"slides.otp", "presentation"},
		{"notes.TXT", "text"}, {"notes.md", "text"}, {"notes.markdown", "text"}, {"notes.log", "text"}, {"notes.rst", "text"},
		{"data.json", "code"}, {"data.xml", "code"}, {"data.yaml", "code"}, {"data.yml", "code"}, {"data.toml", "code"}, {"page.html", "code"}, {"page.htm", "code"},
		{"main.GO", "code"}, {"main.py", "code"}, {"app.JS", "code"}, {"app.jsx", "code"}, {"app.ts", "code"}, {"app.tsx", "code"}, {"main.rs", "code"}, {"App.java", "code"}, {"main.c", "code"}, {"main.h", "code"}, {"main.cpp", "code"}, {"App.cs", "code"}, {"run.sh", "code"}, {"run.ps1", "code"}, {"query.sql", "code"}, {"styles.css", "code"},
		{"photo.JPEG", "image"}, {"photo.jpg", "image"}, {"photo.gif", "image"}, {"photo.webp", "image"}, {"photo.tif", "image"}, {"photo.tiff", "image"}, {"photo.bmp", "image"}, {"photo.svg", "image"}, {"photo.avif", "image"}, {"photo.heic", "image"}, {"photo.heif", "image"}, {"photo.ico", "image"},
		{"film.MP4", "video"}, {"film.webm", "video"}, {"film.mkv", "video"}, {"film.mov", "video"}, {"film.avi", "video"}, {"film.m4v", "video"}, {"film.mpeg", "video"}, {"film.mpg", "video"}, {"film.ogv", "video"}, {"film.wmv", "video"}, {"film.3gp", "video"},
		{"recording.MP3", "audio"}, {"recording.wav", "audio"}, {"recording.flac", "audio"}, {"recording.ogg", "audio"}, {"recording.opus", "audio"}, {"recording.m4a", "audio"}, {"recording.aac", "audio"}, {"recording.aiff", "audio"}, {"recording.aif", "audio"}, {"recording.wma", "audio"}, {"recording.amr", "audio"},
		{"report.pdf.exe", "other"}, {"archive.tar.gz", "other"}, {"binary.bin", "other"}, {"README", "other"}, {"", "other"}, {"report.pdf ", "other"},
	} {
		t.Run(c.filename, func(t *testing.T) {
			rows := decodeRows([]map[string]any{{"file_name": c.filename}})
			data, err := json.Marshal(rows[0])
			if err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err = json.Unmarshal(data, &row); err != nil {
				t.Fatal(err)
			}
			if row["file_type"] != c.want {
				t.Fatalf("%q file_type=%v, want %s", c.filename, row["file_type"], c.want)
			}
		})
	}
}
