package main

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

func newRuntimeDocumentIndex(cfg *config.Config, allowProvisioning bool) (*arcadedb.DocumentIndex, error) {
	tenants, err := newRuntimeDocumentTenants(cfg, allowProvisioning)
	if err != nil {
		return nil, fmt.Errorf("document index: %w", err)
	}
	index, err := arcadedb.NewDocumentIndex(tenants, arcadedb.DocumentIndexConfig{
		Dimensions:             cfg.Embed.Dimensions,
		MaxRetrievalCandidates: cfg.DocumentRetrieval.RetrievalCandidates,
		DenseMaxDistance:       cfg.DocumentRetrieval.DenseMaxDistance,
		MinRelevance:           cfg.DocumentRetrieval.MinRelevance,
	})
	if err != nil {
		return nil, fmt.Errorf("document index: %w", err)
	}
	return index, nil
}

// newRuntimeDocumentTenants resolves each identity's own ArcadeDB database, the one ingest writes
// its documents to. allowProvisioning creates a missing one, which needs the admin credential.
func newRuntimeDocumentTenants(cfg *config.Config, allowProvisioning bool) (*arcadedb.TenantClients, error) {
	if cfg == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return nil, fmt.Errorf("documents require ArcadeDB configuration")
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		return nil, fmt.Errorf("tenant credentials: %w", err)
	}
	var admin *arcadedb.Client
	if allowProvisioning && strings.TrimSpace(cfg.ArcadeDB.AdminUser) != "" {
		admin, err = arcadedb.New(arcadedb.Config{
			BaseURL: cfg.ArcadeDB.BaseURL, Database: cfg.ArcadeDB.Database,
			User: cfg.ArcadeDB.AdminUser, Password: cfg.ArcadeDB.AdminPassword,
		})
		if err != nil {
			return nil, fmt.Errorf("ArcadeDB admin: %w", err)
		}
	}
	return arcadedb.NewTenantClients(arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}, admin, nil, credentials), nil
}
