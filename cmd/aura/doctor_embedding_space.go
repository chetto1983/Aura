package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/identity"
)

// The embedding_space check names every tenant and family served lexically because a vector
// is in another space (spec §10). It warns rather than fails: after a route change every gate
// is closed on purpose until the pass and ingest have re-embedded, and a document that keeps
// failing is named here so the operator can fix or remove it.

var (
	doctorProbeEmbeddingSpace doctorProbe = defaultDoctorProbeEmbeddingSpace
	// doctorEmbeddingReports is a var so the check's own tests need no Postgres or ArcadeDB.
	doctorEmbeddingReports = defaultDoctorEmbeddingReports
)

// stuckNamesShown bounds the file names one line carries; the cockpit card lists them all.
const stuckNamesShown = 3

func defaultDoctorProbeEmbeddingSpace(ctx context.Context, cfg *config.Config) (string, error) {
	space, reports, err := doctorEmbeddingReports(ctx, cfg)
	if err != nil {
		return "", err
	}
	if reports == nil {
		return "not configured (no ArcadeDB memory server)", nil
	}
	var closed []string
	for _, report := range reports {
		for _, state := range report.Families {
			if state.Open {
				continue
			}
			other := 0
			for _, tally := range state.Types {
				other += tally.OtherSpace
			}
			entry := fmt.Sprintf("%s %s (%d vectors in another space", report.IdentityID, state.Family, other)
			if state.Family == "documents" && len(report.StuckDocuments) > 0 {
				names := make([]string, 0, stuckNamesShown)
				for _, stuck := range report.StuckDocuments[:min(stuckNamesShown, len(report.StuckDocuments))] {
					names = append(names, stuck.FileName)
				}
				entry += ": " + strings.Join(names, ", ")
			}
			closed = append(closed, entry+")")
		}
	}
	if len(closed) > 0 {
		return "", fmt.Errorf("%d gate(s) closed, served lexically until re-embedded: %s",
			len(closed), strings.Join(closed, "; "))
	}
	return fmt.Sprintf("%d tenant(s) dense in %s", len(reports), describeSpace(space)), nil
}

// defaultDoctorEmbeddingReports reads every tenant against the spaces the daemon's route
// names. Nil reports and no error mean there is no memory server to read.
func defaultDoctorEmbeddingReports(ctx context.Context, cfg *config.Config) (embeddings.Space, []arcadedb.TenantSpaceReport, error) {
	base := strings.TrimSpace(cfg.ArcadeDB.BaseURL)
	if base == "" {
		return embeddings.Space{}, nil, nil
	}
	client := &http.Client{Timeout: doctorEmbedProbeTimeout}
	memory, err := embeddings.RouteSpace(ctx, client, cfg.Embed, arcadedb.MemoryDimensions)
	if err != nil {
		return embeddings.Space{}, nil, fmt.Errorf("name the memory space: %w", err)
	}
	documents, err := embeddings.RouteSpace(ctx, client, cfg.Embed, cfg.Embed.Dimensions)
	if err != nil {
		return embeddings.Space{}, nil, fmt.Errorf("name the documents space: %w", err)
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		return embeddings.Space{}, nil, err
	}
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return embeddings.Space{}, nil, err
	}
	defer pool.Close()
	walk := arcadedb.NewTenantBackfill(identityRoster{store: identity.New(pool)}, arcadedb.Config{BaseURL: base}, credentials, nil)
	reports, err := walk.SpaceReports(ctx, memory.ID, documents.ID)
	return documents, reports, err
}
