package tools

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
)

// Tool-discovery actions are the frozen strategy IDs accepted by the typed
// adaptive adapter. Static preserves the pre-adaptive semantic plus guarded-BM25
// order.
const (
	ToolDiscoveryBM25     = "bm25"
	ToolDiscoverySemantic = "semantic"
	ToolDiscoveryStatic   = "static"
)

// ToolDiscoveryInput is the privacy-safe projection of one free-text discovery
// point. Candidate IDs are registered tool names; neither query bytes nor a
// query-derived fingerprint crosses the adaptive port.
type ToolDiscoveryInput struct {
	OwnerID         string
	RequestID       string
	PointOrdinal    uint32
	QueryLength     int
	MaxResults      int
	CandidateIDs    []string
	CatalogRevision string
}

// ToolDiscoveryExecutor ranks the already-frozen candidates with one registered
// strategy and returns their registered IDs in exposure order.
type ToolDiscoveryExecutor func(context.Context, string) ([]string, error)

// ToolDiscoveryControl owns schema-2 assignment and delivery persistence. The
// tools package owns this port so the adaptive implementation can depend inward
// without introducing an agent/tools -> adaptive import.
type ToolDiscoveryControl interface {
	OrderToolDiscovery(
		context.Context,
		ToolDiscoveryInput,
		ToolDiscoveryExecutor,
	) ([]string, error)
}

type frozenToolCatalog struct {
	specs           []Spec
	byName          map[string]Tool
	ids             []string
	catalogRevision string
}

func (ts *ToolSearch) freezeDeferredTools() frozenToolCatalog {
	all := ts.Registry.All()
	sort.Slice(all, func(i, j int) bool {
		return all[i].Spec().Name < all[j].Spec().Name
	})
	frozen := frozenToolCatalog{byName: make(map[string]Tool, len(all))}
	for _, tool := range all {
		spec := tool.Spec()
		if !spec.Deferred {
			continue
		}
		frozen.specs = append(frozen.specs, spec)
		frozen.ids = append(frozen.ids, spec.Name)
		frozen.byName[spec.Name] = tool
	}
	frozen.catalogRevision = toolCatalogRevision(frozen.ids)
	return frozen
}

func toolCatalogRevision(ids []string) string {
	payload, _ := json.Marshal(ids)
	sum := sha256.Sum256(payload)
	return "tool-catalog-" + hex.EncodeToString(sum[:6])
}

func adaptivePointOrdinal(ctx context.Context) uint32 {
	sum := sha256.Sum256([]byte(
		RequestIDFromContext(ctx) + "\x00" + ToolCallIDFromContext(ctx),
	))
	return binary.BigEndian.Uint32(sum[:4])
}

func (ts *ToolSearch) orderFreeText(
	ctx context.Context,
	query string,
	limit int,
) ([]Tool, error) {
	frozen := ts.freezeDeferredTools()
	var (
		staticIDs  []string
		staticErr  error
		staticDone bool
	)
	static := func(ctx context.Context) ([]string, error) {
		if !staticDone {
			staticDone = true
			var matches []Tool
			matches, staticErr = ts.rankFrozen(
				ctx, frozen, query, limit, ToolDiscoveryStatic,
			)
			staticIDs = toolIDs(matches)
		}
		return slices.Clone(staticIDs), staticErr
	}
	execute := func(
		ctx context.Context,
		action string,
	) ([]string, error) {
		if action == ToolDiscoveryStatic {
			return static(ctx)
		}
		matches, err := ts.rankFrozen(ctx, frozen, query, limit, action)
		if err != nil {
			return nil, err
		}
		return toolIDs(matches), nil
	}
	if ts.Adaptive == nil {
		ids, err := static(ctx)
		return frozen.tools(ids), err
	}
	ids, err := ts.Adaptive.OrderToolDiscovery(
		ctx,
		ToolDiscoveryInput{
			OwnerID:         ownerFromContext(ctx),
			RequestID:       RequestIDFromContext(ctx),
			PointOrdinal:    adaptivePointOrdinal(ctx),
			QueryLength:     len(query),
			MaxResults:      limit,
			CandidateIDs:    slices.Clone(frozen.ids),
			CatalogRevision: frozen.catalogRevision,
		},
		execute,
	)
	if err != nil || !frozen.validOrderedIDs(ids, limit) {
		ids, err = static(ctx)
	}
	return frozen.tools(ids), err
}

func (frozen frozenToolCatalog) validOrderedIDs(
	ids []string,
	limit int,
) bool {
	if len(ids) > limit {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, registered := frozen.byName[id]; !registered {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func (frozen frozenToolCatalog) tools(ids []string) []Tool {
	out := make([]Tool, 0, len(ids))
	for _, id := range ids {
		if tool, ok := frozen.byName[id]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func toolIDs(matches []Tool) []string {
	ids := make([]string, len(matches))
	for index, tool := range matches {
		ids[index] = tool.Spec().Name
	}
	return ids
}

func (ts *ToolSearch) rankFrozen(
	ctx context.Context,
	frozen frozenToolCatalog,
	query string,
	limit int,
	action string,
) ([]Tool, error) {
	if len(frozen.specs) == 0 {
		return nil, nil
	}
	if action == ToolDiscoveryBM25 {
		index := newBM25Index(frozen.specs)
		ranked := index.rank(query)
		if len(ranked) > limit {
			ranked = ranked[:limit]
		}
		out := make([]Tool, 0, len(ranked))
		for _, score := range ranked {
			out = append(out, frozen.byName[frozen.specs[score.doc].Name])
		}
		return out, nil
	}
	if action != ToolDiscoverySemantic && action != ToolDiscoveryStatic {
		return nil, fmt.Errorf(
			"tool discovery strategy %q is not registered",
			action,
		)
	}
	ranker, bm25Names, err := ts.ensureBank(ctx, query, frozen)
	if err != nil {
		return nil, err
	}
	if ranker == nil {
		return nil, nil
	}
	queryVector, err := ts.embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(queryVector) == 0 {
		return nil, nil
	}
	ranked := ranker.RankVecs(queryVector, limit+fusionGuardTopK)
	if action == ToolDiscoveryStatic {
		ranked = guardedTiebreak(ranked, bm25Names)
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]Tool, 0, len(ranked))
	for _, score := range ranked {
		tool, ok := frozen.byName[score.Label]
		if !ok {
			return nil, errors.New(
				"tool discovery ranker returned an unfrozen candidate",
			)
		}
		out = append(out, tool)
	}
	return out, nil
}
