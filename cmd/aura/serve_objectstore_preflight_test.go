package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// garageLayoutServer answers the two admin calls PlacedReplicas makes with a layout that
// stores storedPerNode partitions on each of nodes nodes, out of Garage's 256.
func garageLayoutServer(t *testing.T, nodes, storedPerNode int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/GetClusterLayout":
			roles := make([]string, nodes)
			for i := range roles {
				roles[i] = `{"id":"n","zone":"dc","tags":[],"storedPartitions":` + strconv.Itoa(storedPerNode) + `}`
			}
			_, _ = w.Write([]byte(`{"version":1,"roles":[` + strings.Join(roles, ",") + `]}`))
		case "/v2/GetClusterHealth":
			_, _ = w.Write([]byte(`{"status":"healthy","partitions":256}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func productionGarageConfig(endpoint string) *config.Config {
	return &config.Config{
		Profile:             config.ProfileServerProduction,
		ObjectStoreBackend:  "garage",
		GarageAdminEndpoint: endpoint,
		GarageAdminToken:    "admin-token",
	}
}

func TestObjectStoreReplicationPreflightReadsTheLayout(t *testing.T) {
	two := garageLayoutServer(t, 2, 256)
	if err := objectStoreReplicationPreflight(context.Background(), productionGarageConfig(two.URL)); err != nil {
		t.Fatalf("two copies under server_production = %v, want boot", err)
	}

	one := garageLayoutServer(t, 1, 256)
	err := objectStoreReplicationPreflight(context.Background(), productionGarageConfig(one.URL))
	if err == nil || !strings.Contains(err.Error(), "stores 1 copy of the data") ||
		!strings.Contains(err.Error(), "replication_factor = 2") {
		t.Fatalf("one copy under server_production = %v, want a refusal naming the count and the remedy", err)
	}

	none := garageLayoutServer(t, 0, 0)
	if err := objectStoreReplicationPreflight(context.Background(), productionGarageConfig(none.URL)); err == nil ||
		!strings.Contains(err.Error(), "stores 0 copies") {
		t.Fatalf("no layout under server_production = %v, want a refusal", err)
	}
}

func TestObjectStoreReplicationPreflightRefusesWhatItCannotRead(t *testing.T) {
	cfg := productionGarageConfig("http://127.0.0.1:1")
	if err := objectStoreReplicationPreflight(context.Background(), cfg); err == nil ||
		!strings.Contains(err.Error(), "could not read") {
		t.Fatalf("unreachable admin API = %v, want a refusal", err)
	}
	cfg.GarageAdminToken = ""
	if err := objectStoreReplicationPreflight(context.Background(), cfg); err == nil {
		t.Fatal("no admin token under server_production booted, want a refusal")
	}
}

func TestObjectStoreReplicationPreflightOnlyGatesProduction(t *testing.T) {
	for _, p := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted, config.ProfileSingleUserHardened} {
		cfg := productionGarageConfig("http://127.0.0.1:1")
		cfg.Profile = p
		if err := objectStoreReplicationPreflight(context.Background(), cfg); err != nil {
			t.Errorf("%s = %v, want no replication check", p, err)
		}
	}
	if err := objectStoreReplicationPreflight(context.Background(), nil); err != nil {
		t.Errorf("nil config = %v, want nil", err)
	}
}

type replicaCount struct {
	n   int
	err error
}

func (r replicaCount) PlacedReplicas(context.Context) (int, error) { return r.n, r.err }

func TestCheckPlacedReplicasWordsEachOutcome(t *testing.T) {
	if err := checkPlacedReplicas(context.Background(), replicaCount{n: 3}); err != nil {
		t.Errorf("three copies = %v, want nil", err)
	}
	if err := checkPlacedReplicas(context.Background(), replicaCount{err: errors.New("boom")}); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Errorf("a failed read = %v, want it wrapped", err)
	}
}
