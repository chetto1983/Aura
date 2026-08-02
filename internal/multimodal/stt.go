package multimodal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
)

// STTConfig selects the speech-to-text route. With CloudModel unset (default) the
// audio bytes are POSTed as multipart/form-data to the local faster-whisper
// sidecar at LocalBaseURL+"/audio/transcriptions" (LocalModel + Language fields,
// no auth) — faster-whisper decodes Opus inline (no ffmpeg, spike-027). Set
// CloudModel to swap to OpenRouter, which REJECTS multipart: the cloud arm sends
// JSON {model, input_audio:{data:<base64>, format}} + the shared Bearer key.
// LocalModel stays the local model id; it is NOT the cloud switch.
type STTConfig struct {
	LocalBaseURL      string
	LocalModel        string
	Language          string
	CloudModel        string
	OpenRouterBaseURL string
	OpenRouterAPIKey  string
	TimeoutSec        int
	HTTPClient        *http.Client // optional; nil → a fresh shared client
}

// STTClient transcribes audio bytes to text. It is a single attempt — retry/
// backoff is the caller's concern (assets.AudioProcessor, the one inbound speech
// path, owns the schedule).
type STTClient struct {
	cfg        STTConfig
	httpClient *http.Client
}

// NewSTTClient builds an STT client over the config.
func NewSTTClient(cfg STTConfig) *STTClient {
	return &STTClient{cfg: cfg, httpClient: resolveClient(cfg.HTTPClient)}
}

// Cloud reports whether this client is configured for the cloud (OpenRouter)
// route. Callers may use it to pick the right base URL guard / error copy.
func (c *STTClient) Cloud() bool { return c.cfg.CloudModel != "" }

// Transcribe sends audio (the raw container bytes — OGG/Opus for telegram voice
// notes, or any sidecar-decodable format for an upload) and returns the
// transcript. fileName labels the multipart part (local route); format is the
// container hint required by the cloud JSON route ("ogg", "mp3", …). A non-2xx
// response is a *StatusError the caller may treat as transient.
func (c *STTClient) Transcribe(ctx context.Context, audio []byte, fileName, format string) (string, error) {
	reqCtx, cancel := TimeoutContext(ctx, c.cfg.TimeoutSec)
	defer cancel()

	req, err := c.buildRequest(reqCtx, audio, fileName, format)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", &StatusError{Endpoint: "stt", StatusCode: resp.StatusCode}
	}
	var decoded struct {
		Text string `json:"text"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("stt: decode transcript: %w", err)
	}
	return decoded.Text, nil
}

// buildRequest builds the cloud JSON request (CloudModel set) or the local
// multipart request (default). The two wire formats are deliberately different:
// OpenRouter's transcription endpoint takes JSON input_audio; the local
// faster-whisper sidecar takes multipart/form-data.
func (c *STTClient) buildRequest(ctx context.Context, audio []byte, fileName, format string) (*http.Request, error) {
	if c.cfg.CloudModel != "" {
		return c.buildCloudRequest(ctx, audio, format)
	}
	return c.buildLocalRequest(ctx, audio, fileName)
}

type sttInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type sttCloudRequest struct {
	Model      string        `json:"model"`
	InputAudio sttInputAudio `json:"input_audio"`
}

func (c *STTClient) buildCloudRequest(ctx context.Context, audio []byte, format string) (*http.Request, error) {
	if format == "" {
		format = "ogg"
	}
	body, err := json.Marshal(sttCloudRequest{
		Model: c.cfg.CloudModel,
		InputAudio: sttInputAudio{
			Data:   base64.StdEncoding.EncodeToString(audio),
			Format: format,
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.OpenRouterBaseURL+"/audio/transcriptions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, c.cfg.OpenRouterAPIKey)
	return req, nil
}

func (c *STTClient) buildLocalRequest(ctx context.Context, audio []byte, fileName string) (*http.Request, error) {
	if fileName == "" {
		fileName = "audio.ogg"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err = fw.Write(audio); err != nil {
		return nil, err
	}
	if c.cfg.LocalModel != "" {
		if err = mw.WriteField("model", c.cfg.LocalModel); err != nil {
			return nil, err
		}
	}
	// Pin the transcription language: whisper auto-detect mis-reads short voice
	// notes (spike-027: a 2.8s IT clip auto-detected as 'ja').
	if c.cfg.Language != "" {
		if err = mw.WriteField("language", c.cfg.Language); err != nil {
			return nil, err
		}
	}
	if err = mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.LocalBaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}
