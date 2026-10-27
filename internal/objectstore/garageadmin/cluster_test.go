package garageadmin

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The layouts below are trimmed from GetClusterLayout and GetClusterHealth answered by
// dxflrs/garage:v2.4.1 on 2026-10-27: one node at replication_factor = 1, and two nodes at
// replication_factor = 2.
const (
	layoutOneNode = `{"version":1,"roles":[{"id":"0f2f","zone":"dc1","tags":[],"capacity":1000000000,
		"storedPartitions":256,"usableCapacity":1000000000}],"parameters":{"zoneRedundancy":"maximum"},
		"partitionSize":3906250,"stagedRoleChanges":[],"stagedParameters":null}`
	layoutTwoNodes = `{"version":1,"roles":[
		{"id":"9d88","zone":"dc2","tags":[],"capacity":1000000000,"storedPartitions":256,"usableCapacity":1000000000},
		{"id":"9fd2","zone":"dc1","tags":[],"capacity":1000000000,"storedPartitions":256,"usableCapacity":1000000000}],
		"parameters":{"zoneRedundancy":"maximum"},"partitionSize":3906250,"stagedRoleChanges":[],"stagedParameters":null}`
	// A fresh node before any layout is applied, which is also what a layout Garage refused
	// to compute (fewer nodes than the replication factor) leaves.
	layoutNone = `{"version":0,"roles":[],"parameters":{"zoneRedundancy":"maximum"},"partitionSize":0,
		"stagedRoleChanges":[],"stagedParameters":null}`
	// A gateway node has a role but stores nothing: capacity and storedPartitions are null.
	layoutWithGateway = `{"version":2,"roles":[
		{"id":"0f2f","zone":"dc1","tags":[],"capacity":1000000000,"storedPartitions":256,"usableCapacity":1000000000},
		{"id":"gw01","zone":"dc1","tags":[],"capacity":null,"storedPartitions":null,"usableCapacity":null}],
		"parameters":{"zoneRedundancy":"maximum"},"partitionSize":3906250,"stagedRoleChanges":[],"stagedParameters":null}`
	healthAll = `{"status":"healthy","knownNodes":1,"connectedNodes":1,"storageNodes":1,"storageNodesUp":1,
		"partitions":256,"partitionsQuorum":256,"partitionsAllOk":256}`
)

func clusterMock(t *testing.T, layout, health string) *Client {
	t.Helper()
	c, _ := newMock(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/GetClusterLayout":
			_, _ = w.Write([]byte(layout))
		case "/v2/GetClusterHealth":
			_, _ = w.Write([]byte(health))
		default:
			t.Errorf("unexpected admin call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return c
}

func TestPlacedReplicasCountsTheCopiesTheLayoutStores(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout string
		want   int
	}{
		{"one node at replication_factor 1", layoutOneNode, 1},
		{"two nodes at replication_factor 2", layoutTwoNodes, 2},
		{"no layout applied", layoutNone, 0},
		{"a gateway node stores nothing", layoutWithGateway, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clusterMock(t, tc.layout, healthAll).PlacedReplicas(context.Background())
			if err != nil {
				t.Fatalf("PlacedReplicas: %v", err)
			}
			if got != tc.want {
				t.Errorf("PlacedReplicas = %d, want %d", got, tc.want)
			}
		})
	}
}

// A layout in transition can hold a partition on more nodes than another; the count is the
// copies every partition is guaranteed to have, so a remainder rounds down, never up.
func TestPlacedReplicasRoundsAnUnevenLayoutDown(t *testing.T) {
	uneven := strings.Replace(layoutTwoNodes, `"storedPartitions":256,"usableCapacity":1000000000},
		{"id":"9fd2"`, `"storedPartitions":200,"usableCapacity":1000000000},
		{"id":"9fd2"`, 1)
	got, err := clusterMock(t, uneven, healthAll).PlacedReplicas(context.Background())
	if err != nil {
		t.Fatalf("PlacedReplicas: %v", err)
	}
	if got != 1 {
		t.Errorf("PlacedReplicas over 456 stored of 256 partitions = %d, want 1", got)
	}
}

func TestPlacedReplicasRefusesWhatItCannotMeasure(t *testing.T) {
	for _, tc := range []struct {
		name, layout, health, want string
	}{
		{"health reports no partitions", layoutOneNode, `{"status":"unavailable","partitions":0}`, "no partitions"},
		{"layout is not JSON", `<html>`, healthAll, "decode GetClusterLayout"},
		{"health is not JSON", layoutOneNode, `<html>`, "decode GetClusterHealth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := clusterMock(t, tc.layout, tc.health).PlacedReplicas(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PlacedReplicas error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestPlacedReplicasSurfacesAnAdminRefusal(t *testing.T) {
	c, _ := newMock(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"AccessDenied"}`))
	})
	_, err := c.PlacedReplicas(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GetClusterLayout returned status 403") {
		t.Fatalf("PlacedReplicas error = %v, want the 403 named", err)
	}
}
