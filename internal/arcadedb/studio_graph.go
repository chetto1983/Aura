package arcadedb

import (
	"context"
	"fmt"
	"strings"
)

// MaxStudioGraphLimit is the largest record page Aura permits on the Studio
// serializer read path. Callers may impose smaller limits but cannot widen it.
const MaxStudioGraphLimit = 200

// StudioVertex is ArcadeDB Studio's compact vertex representation. The field
// names are the server wire contract, not Aura abbreviations.
type StudioVertex struct {
	RID        string         `json:"r"`
	Type       string         `json:"t"`
	Properties map[string]any `json:"p"`
	In         int            `json:"i"`
	Out        int            `json:"o"`
}

// Degree is the number of incident edges reported by ArcadeDB.
func (v StudioVertex) Degree() int { return v.In + v.Out }

// StudioEdge is ArcadeDB Studio's compact directed-edge representation: o is
// the outgoing/source RID and i is the incoming/target RID.
type StudioEdge struct {
	RID        string         `json:"r"`
	Type       string         `json:"t"`
	Properties map[string]any `json:"p"`
	In         string         `json:"i"`
	Out        string         `json:"o"`
}

// StudioGraph is the graph response emitted by ArcadeDB's studio serializer.
type StudioGraph struct {
	Vertices []StudioVertex   `json:"vertices"`
	Edges    []StudioEdge     `json:"edges"`
	Records  []map[string]any `json:"records"`
}

// QueryStudioGraph runs a bounded read-only SQL query and requests the graph
// envelope consumed by ArcadeDB Studio. The HTTP API documents "studio" as the
// serializer that adds graph metadata to query results:
// https://docs.arcadedb.com/arcadedb/reference/http-api/http
func (c *Client) QueryStudioGraph(
	ctx context.Context,
	statement string,
	params map[string]any,
	limit int,
) (StudioGraph, error) {
	if strings.TrimSpace(statement) == "" {
		return StudioGraph{}, ErrEmptyStatement
	}
	if limit <= 0 || limit > MaxStudioGraphLimit {
		return StudioGraph{}, fmt.Errorf("arcadedb: studio graph limit must be between 1 and %d", MaxStudioGraphLimit)
	}
	payload := map[string]any{
		"language":   "sql",
		"command":    statement,
		"limit":      limit,
		"serializer": "studio",
	}
	if len(params) > 0 {
		payload["params"] = params
	}
	var decoded struct {
		Result StudioGraph `json:"result"`
	}
	if err := c.executeJSON(ctx, c.queryURL, payload, &decoded); err != nil {
		return StudioGraph{}, err
	}
	if decoded.Result.Vertices == nil {
		decoded.Result.Vertices = []StudioVertex{}
	}
	if decoded.Result.Edges == nil {
		decoded.Result.Edges = []StudioEdge{}
	}
	return decoded.Result, nil
}
