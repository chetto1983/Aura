package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Turn recall (docs/superpowers/specs/2026-10-06-turn-recall-design.md): the identity's own
// past user turns nearest to the one being read, with the tools they ran read back through
// the reasoning graph. One query vector serves three pools, and each pool is filtered BEFORE
// its top-k: five closer unlabelled copies can never evict a label, teacher rows can never
// crowd out a user label, and turns that ran only unusable tools can never crowd out one
// that ran a usable tool.

const (
	// recallRadius is the cosine distance (1 - cosine) a recalled turn may sit at, on the
	// document template. A candidate from the lab VM on 2026-10-06: repeats of one request
	// sat at cosine 0.91-1.00 while unrelated short prompts reached 0.88-0.90, so the bound
	// is tight and is not, alone, evidence that a reuse is valid.
	recallRadius = 0.10
	// recallNeighbours bounds each pool.
	recallNeighbours = 5
)

// The two decision sources that are reusable labels (migration 0137's CHECK names all six).
const (
	labelSourceUser    = "user"
	labelSourceTeacher = "teacher"
)

// TurnRecallRequest is one read of the identity's past turns for the turn being decided.
type TurnRecallRequest struct {
	IdentityID    string
	Text          string
	ContextKey    string
	RouteKey      string
	PolicyVersion string
	// SourceRef is the turn being read: it is excluded from every pool, because it may already
	// be projected by the time it is read.
	SourceRef string
	// DeferredTools are the registered deferred tools a remembered turn may preload.
	DeferredTools []string
	// IncludeLabels is false when the effort is already fixed and only tools are wanted.
	IncludeLabels bool
}

// RecalledTurn is one past user turn within the radius. Its field order matches
// agent.RecalledTurn: the runner converts one into the other.
type RecalledTurn struct {
	Distance        float64
	SourceRef       string
	Effort          string
	RequestedEffort string
	EffortSource    string
	ContextKey      string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
	// Tools are the names of the turn's successful tool calls, sorted.
	Tools []string
}

// TurnRecall holds each pool nearest first.
type TurnRecall struct {
	UserLabels    []RecalledTurn
	TeacherLabels []RecalledTurn
	ToolTurns     []RecalledTurn
}

// recallTurnSelect is what every pool shares: the identity's live user turns embedded in the
// reader's space, other than the turn being read, with the same prior context. The tool
// traversal starts from @rid because the expanded neighbour rows are projections, and the
// same path written without it reads null.
const recallTurnSelect = "SELECT distance, source_ref, effort, effort_requested, effort_source," +
	" recall_context_key, effort_route_key, effort_policy_version, effort_origin_ref," +
	" @rid.out('" + nextTurnEdgeType + "').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')" +
	"[status = 'succeeded'].tool_name AS tools" +
	" FROM (SELECT expand(`vector.neighbors`('" + conversationTurnType + "[embedding]', :vector, :neighbours," +
	" { filter: (SELECT @rid FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND role = 'user' AND deleted_at IS NULL" + denseSpaceFilter +
	" AND source_ref <> :source_ref AND recall_context_key = :context_key"

// A filter that matches nothing must mean "no candidates", not "no filter": ArcadeDB
// before 26.10.1 confused the two (#8959), which TenantClients.For refuses.
const recallTurnClose = ").@rid, maxDistance: :radius }))) ORDER BY distance"

// A label must also share the route and the policy and carry a complete decision.
const recallLabelStatement = recallTurnSelect +
	" AND effort_route_key = :route_key AND effort_policy_version = :policy_version" +
	" AND effort_source = :effort_source AND effort IS NOT NULL AND effort_requested IS NOT NULL" +
	recallTurnClose

// A tool example must have run at least one currently eligible deferred tool successfully.
// CONTAINS with a condition is ArcadeDB's collection filter (arcadedb-docs
// reference/sql/sql-select.adoc: `races CONTAINS(name in [...])`).
const recallToolStatement = recallTurnSelect +
	" AND out('" + nextTurnEdgeType + "').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')" +
	" CONTAINS (status = 'succeeded' AND tool_name IN :tools)" +
	recallTurnClose

// RecallTurns returns the identity's nearest eligible past user turns within recallRadius,
// in three pools. With no embedder, no text or no context key it returns nothing and no
// error, as when memory is off; a failed or refused embedding is an error the agent logs
// once before reading the turn without memory.
func (c *Client) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	if strings.TrimSpace(request.IdentityID) == "" {
		return TurnRecall{}, errors.New("arcadedb: turn recall identity must be non-empty")
	}
	if request.SourceRef == "" {
		return TurnRecall{}, errors.New("arcadedb: turn recall needs the source_ref of the turn being read")
	}
	if strings.TrimSpace(request.Text) == "" || request.ContextKey == "" {
		return TurnRecall{}, nil
	}
	query, reason := c.embedChecked(ctx, taskDocumentPrefix, request.Text, nil)
	switch reason {
	case "":
	case reasonEmbedderNotConfigured:
		return TurnRecall{}, nil
	default:
		return TurnRecall{}, fmt.Errorf("arcadedb: turn recall query: %s", reason)
	}
	params := map[string]any{
		"identity_id": request.IdentityID, "source_ref": request.SourceRef, "context_key": request.ContextKey,
		"neighbours": recallNeighbours, "radius": recallRadius,
	}
	query.bind(params)

	var recall TurnRecall
	var err error
	if request.IncludeLabels && request.RouteKey != "" && request.PolicyVersion != "" {
		if recall.UserLabels, err = c.recallLabels(ctx, params, request, labelSourceUser); err != nil {
			return TurnRecall{}, err
		}
		if recall.TeacherLabels, err = c.recallLabels(ctx, params, request, labelSourceTeacher); err != nil {
			return TurnRecall{}, err
		}
	}
	if len(request.DeferredTools) > 0 {
		if recall.ToolTurns, err = c.recallToolTurns(ctx, params, request); err != nil {
			return TurnRecall{}, err
		}
	}
	return recall, nil
}

func (c *Client) recallLabels(ctx context.Context, base map[string]any, request TurnRecallRequest, source string) ([]RecalledTurn, error) {
	params := maps.Clone(base)
	params["route_key"], params["policy_version"], params["effort_source"] = request.RouteKey, request.PolicyVersion, source
	rows, err := c.Query(ctx, recallLabelStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: recall %s labels: %w", source, err)
	}
	labels := make([]RecalledTurn, 0, len(rows))
	for _, row := range rows {
		turn, ok := recalledTurnFromRow(row)
		if ok && turn.EffortSource == source && turn.Effort != "" && turn.RequestedEffort != "" &&
			turn.RouteKey == request.RouteKey && turn.PolicyVersion == request.PolicyVersion &&
			turn.ContextKey == request.ContextKey && turn.SourceRef != request.SourceRef {
			labels = append(labels, turn)
		}
	}
	return labels, nil
}

func (c *Client) recallToolTurns(ctx context.Context, base map[string]any, request TurnRecallRequest) ([]RecalledTurn, error) {
	params := maps.Clone(base)
	params["tools"] = request.DeferredTools
	rows, err := c.Query(ctx, recallToolStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: recall tool turns: %w", err)
	}
	turns := make([]RecalledTurn, 0, len(rows))
	for _, row := range rows {
		turn, ok := recalledTurnFromRow(row)
		if ok && len(turn.Tools) > 0 && turn.ContextKey == request.ContextKey && turn.SourceRef != request.SourceRef {
			turns = append(turns, turn)
		}
	}
	return turns, nil
}

// recalledTurnFromRow refuses a row without a source or outside the radius: the engine
// enforces both, and a row that slipped past it must not become a label or a preload.
func recalledTurnFromRow(row map[string]any) (RecalledTurn, bool) {
	distance, ok := row["distance"].(float64)
	if !ok || math.IsNaN(distance) || distance < 0 || distance > recallRadius {
		return RecalledTurn{}, false
	}
	turn := RecalledTurn{
		Distance: distance, SourceRef: rowString(row, "source_ref"),
		Effort: rowString(row, "effort"), RequestedEffort: rowString(row, "effort_requested"),
		EffortSource: rowString(row, "effort_source"), ContextKey: rowString(row, "recall_context_key"),
		RouteKey: rowString(row, "effort_route_key"), PolicyVersion: rowString(row, "effort_policy_version"),
		OriginRef: rowString(row, "effort_origin_ref"),
	}
	if tools := rowStrings(row, "tools"); len(tools) > 0 {
		slices.Sort(tools)
		turn.Tools = slices.Compact(tools)
	}
	return turn, turn.SourceRef != ""
}
