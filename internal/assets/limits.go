//nolint:revive // Internal asset validation types are exported for service wiring.
package assets

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

type Limits struct {
	MaxDocumentBytes int64
	MaxImageBytes    int64
	MaxAudioBytes    int64
}

var documentExts = map[string]bool{".pdf": true, ".xlsx": true, ".xlsm": true, ".docx": true}

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
			return fmt.Errorf("unsupported document type %q", filepath.Ext(fileName))
		}
		if size > l.MaxDocumentBytes {
			return fmt.Errorf("document exceeds %d bytes", l.MaxDocumentBytes)
		}
	case ModalityImage:
		if size > l.MaxImageBytes {
			return fmt.Errorf("image exceeds %d bytes", l.MaxImageBytes)
		}
	case ModalityAudio:
		if size > l.MaxAudioBytes {
			return fmt.Errorf("audio exceeds %d bytes", l.MaxAudioBytes)
		}
	default:
		return fmt.Errorf("unsupported asset modality %q", modality)
	}
	return nil
}
