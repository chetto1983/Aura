// serve_objectstore_preflight.go — the boot-time half of the server_production durability
// gate (PROF-06/F-018). config.gateReplication, which must stay free of I/O like the rest of
// Config.ValidateProfile, has already required the garage backend and its admin credential.
// This reads what those make reachable: the copies Garage's cluster layout stores. Until
// 2026-10-27 the gate trusted AURA_OBJECTSTORE_REPLICATION_FACTOR, a number the operator
// typed and nothing compared with Garage's own replication_factor = 1, so setting it to 2
// passed the gate and changed no durability.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

const (
	minProductionReplicas             = 2
	objectStoreReplicationBootTimeout = 30 * time.Second
)

type replicaCounter interface {
	PlacedReplicas(ctx context.Context) (int, error)
}

// objectStoreReplicationPreflight refuses to boot server_production unless the object store
// keeps at least two copies of the data. Every other profile returns nil at once.
func objectStoreReplicationPreflight(ctx context.Context, cfg *config.Config) error {
	if cfg == nil || cfg.Profile != config.ProfileServerProduction {
		return nil
	}
	client, err := garageadmin.New(cfg.GarageAdminEndpoint, cfg.GarageAdminToken)
	if err != nil {
		return fmt.Errorf("config: server_production cannot read the object store's replication: %w", err)
	}
	return checkPlacedReplicas(ctx, client)
}

func checkPlacedReplicas(ctx context.Context, counter replicaCounter) error {
	cctx, cancel := context.WithTimeout(ctx, objectStoreReplicationBootTimeout)
	defer cancel()
	copies, err := counter.PlacedReplicas(cctx)
	if err != nil {
		return fmt.Errorf("config: server_production could not read the object store's replication "+
			"from the Garage admin API (%w), and refuses to boot on a durability it cannot see", err)
	}
	if copies < minProductionReplicas {
		noun := "copies"
		if copies == 1 {
			noun = "copy"
		}
		return fmt.Errorf("config: Garage's cluster layout stores %d %s of the data, and server_production "+
			"needs at least %d. Set replication_factor = %d or more in garage.toml on every node, give "+
			"the layout that many storage nodes, and apply it (garage layout apply)",
			copies, noun, minProductionReplicas, minProductionReplicas)
	}
	slog.Info("aura serve: object store replication verified from the Garage layout", "copies", copies)
	return nil
}
