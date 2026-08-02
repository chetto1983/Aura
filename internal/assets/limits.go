//nolint:revive // Internal asset validation types are exported for service wiring.
package assets

import (
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

// Refusal reasons a caller can act on. A channel has to tell "your file is too big"
// apart from "the object store is down" — the first is the operator's to fix, the
// second is ours — and it cannot do that against an opaque fmt.Errorf.
var (
	ErrAssetTooLarge    = errors.New("asset exceeds the size ceiling")
	ErrAssetUnsupported = errors.New("unsupported asset type")
)

type Limits struct {
	MaxDocumentBytes int64
	MaxImageBytes    int64
	MaxAudioBytes    int64
}

// documentExts is the asset-upload allowlist for the ModalityDocument path. It MUST
// stay in sync with the text-document subset of documents.supportedDocumentExt (the
// ingest allowlist) — image formats live on the ModalityImage path, so they are
// intentionally excluded here. Drift between the two lists silently 400s uploads of
// formats the ingest pipeline supports (the pptx/html regression). limits_sync_test.go
// asserts they match.
var documentExts = map[string]bool{
	".pdf":      true,
	".docx":     true,
	".pptx":     true,
	".xlsx":     true,
	".xlsm":     true,
	".html":     true,
	".htm":      true,
	".csv":      true,
	".md":       true,
	".markdown": true,
	".txt":      true,
	".json":     true,
	".xml":      true,
	".epub":     true,
}

func InferModality(fileName, mimeType string) Modality {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case documentExts[ext]:
		return ModalityDocument
	case strings.HasPrefix(mimeType, "image/"):
		return ModalityImage
	case strings.HasPrefix(mimeType, "audio/"):
		return ModalityAudio
	default:
		if mt := mime.TypeByExtension(ext); strings.HasPrefix(mt, "image/") {
			return ModalityImage
		}
		if mt := mime.TypeByExtension(ext); strings.HasPrefix(mt, "audio/") {
			return ModalityAudio
		}
		return ModalityUnknown
	}
}

func (l Limits) Validate(modality Modality, fileName string, size int64) error {
	if size < 0 {
		return fmt.Errorf("asset size must be non-negative")
	}
	switch modality {
	case ModalityDocument:
		if !documentExts[strings.ToLower(filepath.Ext(fileName))] {
			return fmt.Errorf("%w: document type %q", ErrAssetUnsupported, filepath.Ext(fileName))
		}
		if size > l.MaxDocumentBytes {
			return fmt.Errorf("%w: document exceeds %d bytes", ErrAssetTooLarge, l.MaxDocumentBytes)
		}
	case ModalityImage:
		if size > l.MaxImageBytes {
			return fmt.Errorf("%w: image exceeds %d bytes", ErrAssetTooLarge, l.MaxImageBytes)
		}
	case ModalityAudio:
		if size > l.MaxAudioBytes {
			return fmt.Errorf("%w: audio exceeds %d bytes", ErrAssetTooLarge, l.MaxAudioBytes)
		}
	default:
		return fmt.Errorf("%w: asset modality %q", ErrAssetUnsupported, modality)
	}
	return nil
}
