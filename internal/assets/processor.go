//nolint:revive // Internal processor interfaces are exported for composition roots.
package assets

import "context"

type Processor interface {
	ProcessAsset(ctx context.Context, asset Asset) (Result, error)
}

type ProcessorSet struct {
	Document Processor
	Image    Processor
	Audio    Processor
}

func (p ProcessorSet) For(modality Modality) Processor {
	switch modality {
	case ModalityDocument:
		return p.Document
	case ModalityImage:
		return p.Image
	case ModalityAudio:
		return p.Audio
	default:
		return nil
	}
}
