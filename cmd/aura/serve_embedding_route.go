package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// embeddingProbeTimeout bounds each request of a route preview's probe: a hosted catalogue
// read and one synthetic batch.
const embeddingProbeTimeout = 30 * time.Second

// bootEmbedEnvironment is the embedding route the environment named when the process started.
// Package variables initialise before main, so before the aura.settings overlay rewrites it.
var bootEmbedEnvironment = config.LoadEmbed()

// embeddingRoutes answers the cockpit's embedding endpoints (agui.EmbeddingRoutes) from the
// daemon's own two embedders, the tenant walk the memory pass uses, and the route probe.
type embeddingRoutes struct {
	memory, documents arcadedb.DenseEmbedder
	corpus            *arcadedb.TenantBackfill
	key               func() string
	dims              int
	http              *http.Client
	environment       config.EmbedConfig
}

func (e *embeddingRoutes) Current(ctx context.Context) (embeddings.Space, embeddings.Space, error) {
	if e.memory == nil || e.documents == nil {
		return embeddings.Space{}, embeddings.Space{}, embeddings.ErrNoRoute
	}
	memory, err := e.memory.Space(ctx)
	if err != nil {
		return embeddings.Space{}, embeddings.Space{}, fmt.Errorf("memory space: %w", err)
	}
	documents, err := e.documents.Space(ctx)
	if err != nil {
		return embeddings.Space{}, embeddings.Space{}, fmt.Errorf("documents space: %w", err)
	}
	return memory, documents, nil
}

func (e *embeddingRoutes) Reports(ctx context.Context, memory, documents string) ([]arcadedb.TenantSpaceReport, error) {
	return e.corpus.SpaceReports(ctx, memory, documents)
}

func (e *embeddingRoutes) Work(ctx context.Context, memory, documents string, limitChars int) (arcadedb.CorpusWork, error) {
	return e.corpus.CorpusWork(ctx, memory, documents, limitChars)
}

// Probe measures the route at the documents width with the daemon's own credential. The memory
// family's target is named apart only when its pinned width differs.
func (e *embeddingRoutes) Probe(ctx context.Context, embed config.EmbedConfig) (embeddings.RouteProbe, embeddings.Space, error) {
	probe, err := embeddings.ProbeRoute(ctx, e.http, embed, e.key(), e.dims)
	if err != nil || e.dims == arcadedb.MemoryDimensions {
		return probe, probe.Space, err
	}
	memory, err := embeddings.RouteSpace(ctx, e.http, embed, arcadedb.MemoryDimensions)
	return probe, memory, err
}

func (e *embeddingRoutes) Dimensions() int { return e.dims }

func (e *embeddingRoutes) Environment() config.EmbedConfig { return e.environment }

// wireEmbeddingRoutes wires the three embedding route endpoints; without a memory server they
// stay unwired and answer 503.
func wireEmbeddingRoutes(server *agui.Server, chat *chatEnv) {
	corpus := memoryTenantWalk(chat, "embedding space report", nil)
	if corpus == nil {
		return
	}
	server.SetEmbeddingRoutes(&embeddingRoutes{
		memory: chat.memoryEmbedder, documents: chat.documentEmbedder, corpus: corpus,
		key: liveKey(chat.llmRuntime), dims: chat.cfg.Embed.Dimensions,
		http: &http.Client{Timeout: embeddingProbeTimeout}, environment: bootEmbedEnvironment,
	})
}
