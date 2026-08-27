package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mediaKind string

const (
	mediaKindImage mediaKind = "image"
	mediaKindAudio mediaKind = "audio"
)

type mediaRuntime struct {
	vision *multimodal.VisionClient
	stt    *multimodal.STTClient
}

func newMediaRuntime(cfg *config.Config, client *http.Client) mediaRuntime {
	visionCfg := multimodal.VisionConfigFrom(cfg)
	visionCfg.HTTPClient = client
	sttCfg := multimodal.STTConfigFrom(cfg)
	sttCfg.HTTPClient = client
	return mediaRuntime{
		vision: multimodal.NewVisionClient(visionCfg),
		stt:    multimodal.NewSTTClient(sttCfg),
	}
}

func (r mediaRuntime) derive(
	ctx context.Context, kind mediaKind, path, fileName string,
) (string, error) {
	// #nosec G304 -- this CLI intentionally reads the operator-selected document path.
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	switch kind {
	case mediaKindImage:
		return assets.DescribeImageForRetrieval(ctx, r.vision, raw, mediaType(path, fileName, raw))
	case mediaKindAudio:
		mimeType := mediaType(path, fileName, raw)
		return assets.TranscribeAudioForRetrieval(
			ctx, r.stt, raw, fileName, assets.AudioFormat(mimeType), nil,
		)
	default:
		return "", fmt.Errorf("unsupported media kind %q", kind)
	}
}

func mediaType(path, fileName string, raw []byte) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		ext = filepath.Ext(path)
	}
	if detected := mime.TypeByExtension(ext); detected != "" {
		if base, _, found := strings.Cut(detected, ";"); found {
			return base
		}
		return detected
	}
	return http.DetectContentType(raw)
}

type routeFingerprint struct {
	Revision    int    `json:"revision"`
	VisionMode  string `json:"vision_mode"`
	VisionBase  string `json:"vision_base"`
	VisionModel string `json:"vision_model"`
	STTMode     string `json:"stt_mode"`
	STTBase     string `json:"stt_base"`
	STTModel    string `json:"stt_model"`
	STTLanguage string `json:"stt_language"`
}

func mediaConfigFingerprint(cfg *config.Config) string {
	signature := routeFingerprint{Revision: 1}
	if cfg.VisionCloud {
		signature.VisionMode = "primary"
		signature.VisionBase = cfg.LLM.BaseURL
		signature.VisionModel = cfg.LLM.Model
	} else {
		signature.VisionMode = "local"
		signature.VisionBase = cfg.MultimodalBaseURL
		signature.VisionModel = cfg.MultimodalModel
	}
	if cfg.STTCloudModel != "" {
		signature.STTMode = "cloud"
		signature.STTBase = cfg.LLM.BaseURL
		signature.STTModel = cfg.STTCloudModel
	} else {
		signature.STTMode = "local"
		signature.STTBase = cfg.STTBaseURL
		signature.STTModel = cfg.STTModel
		signature.STTLanguage = cfg.STTLanguage
	}
	payload, err := json.Marshal(signature)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func loadEffectiveConfig(ctx context.Context) (*config.Config, error) {
	if dbURL := strings.TrimSpace(os.Getenv("AURA_DB_URL")); dbURL != "" {
		pool, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			return nil, fmt.Errorf("settings database: %w", err)
		}
		defer pool.Close()
		if err = settings.OverlayEnv(ctx, settings.NewStore(pool)); err != nil {
			return nil, fmt.Errorf("settings overlay: %w", err)
		}
	}
	return config.LoadServe()
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("aura-media-index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fingerprint := fs.Bool("fingerprint", false, "print the effective non-secret media route fingerprint")
	kind := fs.String("kind", "", "media kind: image or audio")
	name := fs.String("name", "", "original file name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadEffectiveConfig(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if *fingerprint {
		if _, err = fmt.Fprintln(stdout, mediaConfigFingerprint(cfg)); err != nil {
			return 1
		}
		return 0
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: aura-media-index -kind image|audio [-name NAME] PATH")
		return 2
	}
	fileName := *name
	if fileName == "" {
		fileName = filepath.Base(fs.Arg(0))
	}
	text, err := newMediaRuntime(cfg, nil).derive(ctx, mediaKind(*kind), fs.Arg(0), fileName)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err = fmt.Fprint(stdout, text); err != nil {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
