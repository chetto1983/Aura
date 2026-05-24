package main

import (
	"context"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/llm/pockettts"
)

type webVoiceSynthesizer struct {
	client *pockettts.Client
}

func (s webVoiceSynthesizer) Synthesize(ctx context.Context, text string) (api.ChatAudio, error) {
	result, err := s.client.Synthesize(ctx, text, "")
	if err != nil {
		return api.ChatAudio{}, err
	}
	return api.ChatAudio{
		Bytes:       result.Audio,
		ContentType: result.MimeType,
	}, nil
}
