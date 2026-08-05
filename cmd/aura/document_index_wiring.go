package main

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

func newRuntimeDocumentIndex(
	cfg *config.Config,
	embedder arcadedb.Embedder,
	allowProvisioning bool,
) (*arcadedb.DocumentIndex, error) {
	if cfg == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return nil, fmt.Errorf("document index requires ArcadeDB configuration")
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		return nil, fmt.Errorf("document index tenant credentials: %w", err)
	}
	var admin *arcadedb.Client
	if allowProvisioning && strings.TrimSpace(cfg.ArcadeDB.AdminUser) != "" {
		admin, err = arcadedb.New(arcadedb.Config{
			BaseURL: cfg.ArcadeDB.BaseURL, Database: cfg.ArcadeDB.Database,
			User: cfg.ArcadeDB.AdminUser, Password: cfg.ArcadeDB.AdminPassword,
		})
		if err != nil {
			return nil, fmt.Errorf("document index ArcadeDB admin: %w", err)
		}
	}
	tenants := arcadedb.NewTenantClients(
		arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}, admin, embedder, credentials,
	)
	index, err := arcadedb.NewDocumentIndex(tenants, arcadedb.DocumentIndexConfig{
		Dimensions:             cfg.Embed.Dimensions,
		MaxCandidatePassages:   cfg.DocumentPipeline.MaxPassagesPerVersion,
		MaxActivePassages:      cfg.DocumentPipeline.MaxActivePassages,
		MaxRetrievalCandidates: cfg.DocumentPipeline.RetrievalCandidates,
	})
	if err != nil {
		return nil, fmt.Errorf("document index: %w", err)
	}
	return index, nil
}
