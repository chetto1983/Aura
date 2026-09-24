package embeddings

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// probeTexts is the fixed batch a route preview embeds. It is synthetic on purpose: the
// preview runs before the operator has confirmed the route, so no stored text may leave for
// it (spec §4).
var probeTexts = []string{
	"The quarterly maintenance report lists three pumps due for inspection before winter.",
	"La riunione di progetto e' spostata a giovedi' mattina nella sala al secondo piano.",
	"Invoice 1042 was paid in full; the remaining balance on the account is zero euros.",
	"Il manuale descrive come sostituire il filtro senza spegnere l'impianto di ventilazione.",
	"Shipping times to the northern warehouses doubled during the week of the storm.",
	"Le tariffe orarie del supporto tecnico cambiano dal primo gennaio del prossimo anno.",
	"A short recipe: boil the pasta for nine minutes, then toss it with oil and garlic.",
	"Il contratto di locazione prevede un preavviso di sei mesi per la disdetta anticipata.",
}

// RouteProbe is what a route preview measures about a route before anything is written.
type RouteProbe struct {
	Space Space // at the width the caller asked for
	// NativeWidth is the width the model answers when no `dimensions` field is sent: wider
	// than the stored width means truncation, valid only for a Matryoshka-trained model.
	NativeWidth    int
	CharsPerSecond float64 // the synthetic batch's characters over the request's wall time
	InputLimit     int     // tokens, from the route's catalogue
	PricePer1M     float64 // pricing.prompt, per million tokens
	HasPrice       bool
}

// ProbeRoute names the space embed produces at dims and sends the synthetic batch through it
// once. A hosted route without a credential sends nothing (ErrNoCredential); a switched-off
// local route is ErrNoRoute.
func ProbeRoute(
	ctx context.Context, client *http.Client, embed config.EmbedConfig, credential string, dims int,
) (RouteProbe, error) {
	base, key, model := config.ResolveEmbedRoute(embed, credential)
	probe := &Client{BaseURL: strings.TrimRight(strings.TrimSpace(base), "/"), Model: model, Client: client}
	if probe.BaseURL == "" {
		return RouteProbe{}, ErrNoRoute
	}
	if config.EmbedRouteKind(embed) != config.EmbedLocal {
		if probe.APIKey = strings.TrimSpace(key); probe.APIKey == "" {
			return RouteProbe{}, ErrNoCredential
		}
	}
	space, err := RouteSpace(ctx, client, embed, dims)
	if err != nil {
		return RouteProbe{}, err
	}
	entry, err := probe.catalogEntry(ctx)
	if err != nil {
		return RouteProbe{}, fmt.Errorf("probe catalogue: %w", err)
	}
	limit, err := inputLimitOf(entry)
	if err != nil {
		return RouteProbe{}, fmt.Errorf("probe catalogue: %w", err)
	}
	width, elapsed, err := probe.nativeWidth(ctx, probeTexts)
	if err != nil {
		return RouteProbe{}, fmt.Errorf("probe embedding: %w", err)
	}
	chars := 0
	for _, text := range probeTexts {
		chars += len(text)
	}
	return RouteProbe{
		Space: space, NativeWidth: width, InputLimit: limit,
		CharsPerSecond: float64(chars) / max(elapsed, time.Millisecond).Seconds(),
		PricePer1M:     entry.Price.InputPer1M, HasPrice: entry.HasPrice,
	}, nil
}

// nativeWidth embeds texts in one request that names no width, and returns the one width
// every vector has and the request's wall time.
func (c *Client) nativeWidth(ctx context.Context, texts []string) (int, time.Duration, error) {
	inputs := make([]any, len(texts))
	for index, text := range texts {
		inputs[index] = text
	}
	var decoded embeddingResponse
	start := time.Now()
	if err := c.postJSON(ctx, endpoint(c.BaseURL), embeddingRequest{Input: inputs, Model: modelOrDefault(c.Model)}, &decoded); err != nil {
		return 0, 0, err
	}
	elapsed := time.Since(start)
	if len(decoded.Data) != len(texts) {
		return 0, 0, fmt.Errorf("endpoint returned %d embeddings for %d inputs", len(decoded.Data), len(texts))
	}
	width := len(decoded.Data[0].Embedding)
	for _, item := range decoded.Data {
		if len(item.Embedding) != width || width == 0 {
			return 0, 0, fmt.Errorf("endpoint returned vectors of widths %d and %d", width, len(item.Embedding))
		}
	}
	return width, elapsed, nil
}
