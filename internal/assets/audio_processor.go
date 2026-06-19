//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/chetto1983/aura/internal/objectstore"
)

type AudioProcessor struct {
	Objects    objectstore.Store
	Config     STTConfig
	HTTPClient *http.Client
}

func NewAudioProcessor(objects objectstore.Store, cfg STTConfig) *AudioProcessor {
	return &AudioProcessor{Objects: objects, Config: cfg, HTTPClient: sidecarHTTPClient()}
}

func (p *AudioProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p == nil || p.Objects == nil {
		return Result{}, fmt.Errorf("audio processor is not configured")
	}
	if p.Config.BaseURL == "" {
		return Result{}, fmt.Errorf("audio processor base URL is not configured")
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, _, err := p.Objects.Get(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = rc.Close() }()
	audioBytes, err := io.ReadAll(rc)
	if err != nil {
		return Result{}, err
	}
	transcript, err := p.postTranscription(ctx, asset.FileName, audioBytes)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:  StatusComplete,
		Summary: transcript,
		Metadata: map[string]any{
			"transcript": transcript,
		},
	}, nil
}

func (p *AudioProcessor) postTranscription(ctx context.Context, fileName string, audio []byte) (string, error) {
	reqCtx, cancel := timeoutContext(ctx, p.Config.TimeoutSec)
	defer cancel()

	if fileName == "" {
		fileName = "audio.bin"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err = fw.Write(audio); err != nil {
		return "", err
	}
	if p.Config.Model != "" {
		if err = mw.WriteField("model", p.Config.Model); err != nil {
			return "", err
		}
	}
	if p.Config.Language != "" {
		if err = mw.WriteField("language", p.Config.Language); err != nil {
			return "", err
		}
	}
	if err = mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.Config.BaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", &sidecarStatusError{endpoint: "stt", statusCode: resp.StatusCode}
	}
	var decoded struct {
		Text string `json:"text"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("asset stt: decode transcript: %w", err)
	}
	return decoded.Text, nil
}

func (p *AudioProcessor) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return sidecarHTTPClient()
}
