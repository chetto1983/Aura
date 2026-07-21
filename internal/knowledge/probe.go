// Connectivity probe for the Neo4j graph substrate, used by the serve daemon's
// /readyz readiness endpoint (O-05/AP-14). It dials bolt with the native driver,
// forces a real connection via VerifyConnectivity, and closes — no long-lived
// handle, no MCP subprocess. A connectivity failure is returned so the readiness
// endpoint can answer 503 when the required graph backend is unreachable.
package knowledge

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
)

const (
	defaultConnectivityTimeout = 2 * time.Second
	minimumConnectivityTimeout = time.Millisecond
)

// VerifyConnectivity dials Neo4j over bolt and confirms the server is reachable,
// then releases the driver. The supplied ctx bounds the dial+verify (the caller
// passes a short readiness timeout). It does NOT assert a kernel version (that is
// the boot-time Ping's job, ping.go) — readiness is a liveness signal for traffic
// routing, not a version gate.
func VerifyConnectivity(ctx context.Context, cfg *Config) error {
	timeout := connectivityTimeout(ctx, time.Now())
	driver, err := neo4j.NewDriverWithContext(
		cfg.BoltURL,
		neo4j.BasicAuth(cfg.User, cfg.Password, ""),
		func(driverCfg *config.Config) {
			driverCfg.SocketConnectTimeout = timeout
			driverCfg.ConnectionAcquisitionTimeout = timeout
		},
	)
	if err != nil {
		return fmt.Errorf("neo4j driver init: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		_ = driver.Close(closeCtx)
	}()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("neo4j connectivity (%s): %w", cfg.BoltURL, err)
	}
	return nil
}

func connectivityTimeout(ctx context.Context, now time.Time) time.Duration {
	timeout := defaultConnectivityTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, deadline.Sub(now))
	}
	return max(timeout, minimumConnectivityTimeout)
}
