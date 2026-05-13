package source

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type UploadFormat struct {
	Kind         Kind
	MimeType     string
	OriginalName string
}

var formatsByExt = map[string]UploadFormat{
	".pdf":   {Kind: KindPDF, MimeType: "application/pdf", OriginalName: "original.pdf"},
	".txt":   {Kind: KindText, MimeType: "text/plain; charset=utf-8", OriginalName: "original.txt"},
	".md":    {Kind: KindMarkdown, MimeType: "text/markdown; charset=utf-8", OriginalName: "original.md"},
	".json":  {Kind: KindJSON, MimeType: "application/json", OriginalName: "original.json"},
	".csv":   {Kind: KindCSV, MimeType: "text/csv; charset=utf-8", OriginalName: "original.csv"},
	".xlsx":  {Kind: KindXLSX, MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", OriginalName: "original.xlsx"},
	".docx":  {Kind: KindDOCX, MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", OriginalName: "original.docx"},
	".pptx":  {Kind: KindPPTX, MimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", OriginalName: "original.pptx"},
	".epub":  {Kind: KindEPUB, MimeType: "application/epub+zip", OriginalName: "original.epub"},
	".html":  {Kind: KindHTML, MimeType: "text/html; charset=utf-8", OriginalName: "original.html"},
	".htm":   {Kind: KindHTML, MimeType: "text/html; charset=utf-8", OriginalName: "original.html"},
	".zip":   {Kind: KindZIP, MimeType: "application/zip", OriginalName: "original.zip"},
	".png":   {Kind: KindImage, MimeType: "image/png", OriginalName: "original.png"},
	".jpg":   {Kind: KindImage, MimeType: "image/jpeg", OriginalName: "original.jpg"},
	".jpeg":  {Kind: KindImage, MimeType: "image/jpeg", OriginalName: "original.jpg"},
	".webp":  {Kind: KindImage, MimeType: "image/webp", OriginalName: "original.webp"},
}

func DetectUploadFormat(filename, mimeType string) (UploadFormat, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	format, ok := formatsByExt[ext]
	if !ok {
		return UploadFormat{}, fmt.Errorf("unsupported file type %q; supported: %s", ext, strings.Join(supportedExts(), ", "))
	}
	// Image uploads need an OCR backend selected. Wave 2.9 ships markitdown
	// without LLM/OCR; image text extraction lands in Wave 2.9.5 with the
	// OCRBackend abstraction. Until then, reject the upload at the boundary
	// rather than ingesting a file that markitdown would silently
	// approximate (or skip entirely).
	if format.Kind == KindImage {
		return UploadFormat{}, fmt.Errorf("image uploads not supported yet (Wave 2.9.5)")
	}
	if mt := strings.TrimSpace(mimeType); mt != "" {
		format.MimeType = mt
	}
	return format, nil
}

func SupportedUploadAccept() string {
	// Image extensions intentionally absent — see DetectUploadFormat for
	// the Wave 2.9.5 gate. They are still registered in formatsByExt so
	// 2.9.5 can flip the gate without touching the table.
	return strings.Join([]string{
		".pdf", ".txt", ".md", ".json", ".csv",
		".xlsx", ".docx", ".pptx",
		".epub", ".html", ".htm", ".zip",
		"application/pdf",
		"text/plain", "text/markdown", "text/html", "text/csv",
		"application/json", "application/zip", "application/epub+zip",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}, ",")
}

func ValidStatus(s Status) bool {
	switch s {
	case StatusStored, StatusExtracting, StatusOCRComplete, StatusExtractComplete, StatusIngested, StatusFailed:
		return true
	default:
		return false
	}
}

func supportedExts() []string {
	exts := make([]string, 0, len(formatsByExt))
	for ext := range formatsByExt {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}
