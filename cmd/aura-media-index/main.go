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
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	mediaKindPDF   mediaKind = "pdf"

	maxScannedPDFPages = 20
)

type mediaRuntime struct {
	vision    *multimodal.VisionClient
	stt       *multimodal.STTClient
	renderPDF func(context.Context, string, string) ([]string, error)
}

func newMediaRuntime(cfg *config.Config, client *http.Client) mediaRuntime {
	visionCfg := multimodal.VisionConfigFrom(cfg)
	visionCfg.HTTPClient = client
	sttCfg := multimodal.STTConfigFrom(cfg)
	sttCfg.HTTPClient = client
	return mediaRuntime{
		vision:    multimodal.NewVisionClient(visionCfg),
		stt:       multimodal.NewSTTClient(sttCfg),
		renderPDF: renderScannedPDFPages,
	}
}

func (r mediaRuntime) derive(
	ctx context.Context, kind mediaKind, path, fileName string,
) (string, error) {
	switch kind {
	case mediaKindImage:
		// #nosec G304 -- this CLI intentionally reads the operator-selected document path.
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return assets.DescribeImageForRetrieval(ctx, r.vision, raw, mediaType(path, fileName, raw))
	case mediaKindAudio:
		// #nosec G304 -- this CLI intentionally reads the operator-selected document path.
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		mimeType := mediaType(path, fileName, raw)
		return assets.TranscribeAudioForRetrieval(
			ctx, r.stt, raw, fileName, assets.AudioFormat(mimeType), nil,
		)
	case mediaKindPDF:
		return r.describeScannedPDF(ctx, path)
	default:
		return "", fmt.Errorf("unsupported media kind %q", kind)
	}
}

func (r mediaRuntime) describeScannedPDF(ctx context.Context, path string) (string, error) {
	tmp, err := os.MkdirTemp("", "aura-scanned-pdf-")
	if err != nil {
		return "", fmt.Errorf("create scanned PDF workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	pages, err := r.renderPDF(ctx, path, tmp)
	if err != nil {
		return "", err
	}
	if len(pages) == 0 {
		return "", fmt.Errorf("scanned PDF rendered no pages")
	}
	if len(pages) > maxScannedPDFPages {
		return "", fmt.Errorf("scanned PDF renderer returned %d pages, limit %d",
			len(pages), maxScannedPDFPages)
	}
	sections := make([]string, 0, len(pages))
	for i, page := range pages {
		// #nosec G304 -- page is produced inside the private renderer workspace.
		raw, readErr := os.ReadFile(page)
		if readErr != nil {
			return "", fmt.Errorf("read scanned PDF page %d: %w", i+1, readErr)
		}
		text, visionErr := assets.DescribeImageForRetrieval(ctx, r.vision, raw, "image/png")
		if visionErr != nil {
			return "", fmt.Errorf("describe scanned PDF page %d: %w", i+1, visionErr)
		}
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("describe scanned PDF page %d: empty vision response", i+1)
		}
		sections = append(sections, fmt.Sprintf("PDF page %d:\n%s", i+1, text))
	}
	return strings.Join(sections, "\n\n"), nil
}

func renderScannedPDFPages(ctx context.Context, path, outDir string) ([]string, error) {
	renderCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	prefix := filepath.Join(outDir, "page")
	// #nosec G204 -- pdftoppm receives the operator-selected path as an argument; no shell
	// parses it, output is fenced to the freshly-created private directory, and page count
	// plus wall time are bounded.
	cmd := exec.CommandContext(renderCtx, "pdftoppm", scannedPDFRenderArgs(path, prefix)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if renderCtx.Err() != nil {
			return nil, fmt.Errorf("render scanned PDF: %w", renderCtx.Err())
		}
		return nil, fmt.Errorf("render scanned PDF: %w: %s", err, strings.TrimSpace(string(output)))
	}
	pages, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, fmt.Errorf("list rendered PDF pages: %w", err)
	}
	sort.Slice(pages, func(i, j int) bool {
		return renderedPDFPageNumber(pages[i]) < renderedPDFPageNumber(pages[j])
	})
	return pages, nil
}

func scannedPDFRenderArgs(path, prefix string) []string {
	// Render one page beyond the accepted boundary. Asking pdftoppm for exactly the
	// limit would make a larger document indistinguishable from a complete 20-page
	// document and silently index a truncated original.
	return []string{
		"-png", "-scale-to", "2048", "-f", "1", "-l", strconv.Itoa(maxScannedPDFPages + 1), path, prefix,
	}
}

func renderedPDFPageNumber(path string) int {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	_, suffix, ok := strings.Cut(stem, "-")
	if !ok {
		return 0
	}
	page, _ := strconv.Atoi(suffix)
	return page
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
	signature := routeFingerprint{Revision: 2}
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
	kind := fs.String("kind", "", "media kind: image, audio, or pdf")
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
		_, _ = fmt.Fprintln(stderr, "usage: aura-media-index -kind image|audio|pdf [-name NAME] PATH")
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
