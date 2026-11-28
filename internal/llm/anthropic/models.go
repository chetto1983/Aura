package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

const (
	maxCatalogBytes          = 2 << 20
	catalogTimeout           = 20 * time.Second
	catalogConnectTimeoutSec = 10
)

// versionSuffix is the trailing major-minor pair of a Claude id ("-5-5"); a dated id
// keeps its dashes and simply finds no list price.
var versionSuffix = regexp.MustCompile(`-(\d{1,2})-(\d{1,2})$`)

// OpenRouterID is the id OpenRouter's public catalogue lists a Claude model under, the
// only place Claude's list prices are published: claude-opus-5-5 is
// anthropic/claude-opus-5.5 (measured 2026-11-27, spec §5.7).
func OpenRouterID(model string) string {
	return "anthropic/" + versionSuffix.ReplaceAllString(strings.TrimSpace(model), "-${1}.${2}")
}

// FetchModels lists the account's active models in the API's order (newest first), with
// the window, output cap and reasoning levels each declares, reading every page.
func FetchModels(ctx context.Context, key KeySource) ([]llm.ModelCatalogEntry, error) {
	return fetchModels(ctx, key, openai_compat.NewCredentialHTTPClient(catalogConnectTimeoutSec), llm.AnthropicBaseURL)
}

func fetchModels(ctx context.Context, source KeySource, hc *http.Client, baseURL string) ([]llm.ModelCatalogEntry, error) {
	key, err := readHouseKey(ctx, source)
	if err != nil {
		return nil, err
	}
	models, err := listModels(ctx, key, hc, baseURL)
	if err != nil {
		return nil, err
	}
	var entries []llm.ModelCatalogEntry
	for _, model := range models {
		// The wire probe (2026-11-27) measured only the bare list, with no lifecycle query
		// and no lifecycle field recorded, so the list is asked for as measured and a row is
		// dropped only when it names a lifecycle other than active.
		inactive := model.Lifecycle != "" && model.Lifecycle != anthropic.ModelInfoLifecycleActive
		if inactive || strings.TrimSpace(model.ID) == "" {
			continue
		}
		entries = append(entries, catalogEntry(model))
	}
	return entries, nil
}

// listModels reads every page of the account's /v1/models, the one response both the
// model picker and the content capabilities come from.
func listModels(ctx context.Context, key string, hc *http.Client, baseURL string) ([]anthropic.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	sdk := newSDK(key, hc, baseURL)
	pager := sdk.Models.ListAutoPaging(ctx, anthropic.ModelListParams{}, option.WithMiddleware(boundedCatalog))
	var models []anthropic.ModelInfo
	for pager.Next() {
		models = append(models, pager.Current())
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", llm.ErrModelCatalogUnavailable, providerError(err, key))
	}
	return models, nil
}

// contentCapabilities is what the catalogue lets model read natively. Only exact MIME
// types are declared, never the image modality: SupportsMIME would admit every image/*
// type through it, and an image block's source takes only these four, so SVG, BMP and
// TIFF stay reference text. A model the account does not list reads text only, and listed
// reports whether it was found.
func contentCapabilities(models []anthropic.ModelInfo, model string) (caps llm.ProviderContentCapabilities, listed bool) {
	caps = llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true}, MIMETypes: map[string]bool{}}
	for _, info := range models {
		if info.ID != model {
			continue
		}
		listed = true
		if info.Capabilities.ImageInput.Supported {
			for _, mediaType := range imageMIMETypes {
				caps.MIMETypes[string(mediaType)] = true
			}
		}
		if info.Capabilities.PDFInput.Supported {
			caps.MIMETypes[pdfMIMEType] = true
		}
	}
	return caps, listed
}

func catalogEntry(model anthropic.ModelInfo) llm.ModelCatalogEntry {
	return llm.ModelCatalogEntry{
		ID:                        model.ID,
		DisplayName:               model.DisplayName,
		ContextWindow:             int(max(model.MaxInputTokens, 0)),
		MaxOutputTokens:           int(max(model.MaxTokens, 0)),
		SupportedReasoningEfforts: declaredEfforts(model.Capabilities.Effort),
		ReasoningMandatory:        !model.Capabilities.Thinking.Types.Disabled.Supported,
	}
}

// declaredEfforts keeps the levels the model marks supported, lowest first, nil when it
// declares none.
func declaredEfforts(effort anthropic.EffortCapability) []llm.ReasoningEffort {
	if !effort.Supported {
		return nil
	}
	var declared []llm.ReasoningEffort
	for _, level := range []struct {
		effort    llm.ReasoningEffort
		supported bool
	}{
		{llm.ReasoningEffortLow, effort.Low.Supported},
		{llm.ReasoningEffortMedium, effort.Medium.Supported},
		{llm.ReasoningEffortHigh, effort.High.Supported},
		{llm.ReasoningEffortXHigh, effort.Xhigh.Supported},
		{llm.ReasoningEffortMax, effort.Max.Supported},
	} {
		if level.supported {
			declared = append(declared, level.effort)
		}
	}
	return declared
}

// boundedCatalog refuses a catalogue page larger than maxCatalogBytes: the SDK reads a
// response whole before decoding it.
func boundedCatalog(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	response, err := next(req)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = http.MaxBytesReader(nil, response.Body, maxCatalogBytes)
	return response, nil
}
