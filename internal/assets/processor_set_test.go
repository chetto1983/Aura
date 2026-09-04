package assets

import (
	"context"
	"testing"
)

type stubProcessor struct{ name string }

func (stubProcessor) ProcessAsset(context.Context, Asset) (Result, error) { return Result{}, nil }

// Routing an asset to the wrong processor is a failure that looks like success: an audio
// file handed to the document processor comes back with an extraction rather than an
// error. So the mapping is pinned per modality, and the unknown case returns nil so the
// caller has to decide, rather than silently getting whichever processor sits first.
func TestProcessorSetRoutesEachModalityAndRefusesTheUnknownOne(t *testing.T) {
	document := stubProcessor{name: "document"}
	image := stubProcessor{name: "image"}
	audio := stubProcessor{name: "audio"}
	set := ProcessorSet{Document: document, Image: image, Audio: audio}

	for _, tc := range []struct {
		modality Modality
		want     Processor
	}{
		{ModalityDocument, document},
		{ModalityImage, image},
		{ModalityAudio, audio},
		{ModalityUnknown, nil},
	} {
		if got := set.For(tc.modality); got != tc.want {
			t.Fatalf("For(%v) = %#v, want %#v", tc.modality, got, tc.want)
		}
	}
}

// An unpopulated set answers nil for every modality instead of panicking: the composition
// root builds it in pieces, and a half-built set must fail where the caller can see it.
func TestProcessorSetAnswersNilBeforeItIsPopulated(t *testing.T) {
	var set ProcessorSet
	for _, modality := range []Modality{ModalityDocument, ModalityImage, ModalityAudio, ModalityUnknown} {
		if got := set.For(modality); got != nil {
			t.Fatalf("For(%v) on an empty set = %#v, want nil", modality, got)
		}
	}
}
