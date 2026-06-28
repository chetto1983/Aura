package documents

import (
	"path/filepath"
	"strings"
)

// supportedDocumentExt is the single source-of-truth allowlist of file
// extensions Aura ingests. Every entry is readable by the markitdown sidecar;
// the sidecar's generic-markdown fallback covers any other markitdown-readable
// format, so this set is the gate the CLI, Telegram, and asset paths share.
var supportedDocumentExt = map[string]struct{}{
	".pdf":      {},
	".docx":     {},
	".pptx":     {},
	".xlsx":     {},
	".xlsm":     {},
	".html":     {},
	".htm":      {},
	".csv":      {},
	".md":       {},
	".markdown": {},
	".txt":      {},
	".json":     {},
	".xml":      {},
	".epub":     {},
	".png":      {},
	".jpg":      {},
	".jpeg":     {},
	".gif":      {},
	".webp":     {},
}

// isSupportedDocument reports whether fileName carries an extension Aura
// ingests. The match is case-insensitive over the file extension; a name with
// no extension is unsupported.
func isSupportedDocument(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		return false
	}
	_, ok := supportedDocumentExt[ext]
	return ok
}
