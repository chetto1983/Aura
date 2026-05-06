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
	".pdf":  {Kind: KindPDF, MimeType: "application/pdf", OriginalName: "original.pdf"},
	".txt":  {Kind: KindText, MimeType: "text/plain; charset=utf-8", OriginalName: "original.txt"},
	".md":   {Kind: KindMarkdown, MimeType: "text/markdown; charset=utf-8", OriginalName: "original.md"},
	".json": {Kind: KindJSON, MimeType: "application/json", OriginalName: "original.json"},
	".csv":  {Kind: KindCSV, MimeType: "text/csv; charset=utf-8", OriginalName: "original.csv"},
	".xlsx": {Kind: KindXLSX, MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", OriginalName: "original.xlsx"},
	".docx": {Kind: KindDOCX, MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", OriginalName: "original.docx"},
}

func DetectUploadFormat(filename, mimeType string) (UploadFormat, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	format, ok := formatsByExt[ext]
	if !ok {
		return UploadFormat{}, fmt.Errorf("unsupported file type %q; supported: %s", ext, strings.Join(supportedExts(), ", "))
	}
	if mt := strings.TrimSpace(mimeType); mt != "" {
		format.MimeType = mt
	}
	return format, nil
}

func SupportedUploadAccept() string {
	return ".pdf,.txt,.md,.json,.csv,.xlsx,.docx,application/pdf,text/plain,text/markdown,application/json,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
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
