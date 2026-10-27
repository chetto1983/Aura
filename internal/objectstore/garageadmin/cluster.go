package garageadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// clusterLayout is the part of GET /v2/GetClusterLayout this package reads: how many
// partitions the current layout stores on each node. A gateway node stores none, so Garage
// answers null for it.
type clusterLayout struct {
	Roles []struct {
		StoredPartitions *int64 `json:"storedPartitions"`
	} `json:"roles"`
}

// clusterHealth is the part of GET /v2/GetClusterHealth this package reads: the number of
// data partitions, which the Garage v2.4 admin API documents as "currently always 256".
type clusterHealth struct {
	Partitions int64 `json:"partitions"`
}

// PlacedReplicas is how many copies of the data the cluster's current layout places: the
// partitions assigned to every node, summed, over the cluster's partition count. It is read,
// not configured. The Garage v2.4 admin API returns replication_factor only from
// ApplyClusterLayout and PreviewClusterLayoutChanges, which write or stage a layout, while
// a layout cannot hold more copies than that factor nor exist with fewer nodes than it
// (ApplyClusterLayout refuses: "The number of nodes with positive capacity (1) is smaller
// than the replication factor (2)"). Measured on v2.4.1, 2026-10-27: 256/256 at factor 1 on
// one node, 512/256 at factor 2 on two, and 0 before any layout. A remainder, a layout
// between versions, rounds down to the copies every partition is assigned. It reads
// placement: whether a rebalance after a layout change has finished copying is not in it.
func (c *Client) PlacedReplicas(ctx context.Context) (int, error) {
	var layout clusterLayout
	if err := c.getJSON(ctx, "GetClusterLayout", &layout); err != nil {
		return 0, err
	}
	var health clusterHealth
	if err := c.getJSON(ctx, "GetClusterHealth", &health); err != nil {
		return 0, err
	}
	if health.Partitions <= 0 {
		return 0, errors.New("garageadmin: GetClusterHealth reports no partitions")
	}
	var stored int64
	for _, role := range layout.Roles {
		if role.StoredPartitions != nil {
			stored += *role.StoredPartitions
		}
	}
	return int(stored / health.Partitions), nil
}

func (c *Client) getJSON(ctx context.Context, op string, out any) error {
	status, raw, err := c.get(ctx, "/v2/"+op, nil)
	if err != nil {
		return err
	}
	if !is2xx(status) {
		return statusErr(op, status, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("garageadmin: decode %s: %w", op, err)
	}
	return nil
}
