package arcadedb

import (
	"context"
	"fmt"
)

// An entity stays while a FACT edge holds it, or a MENTIONS edge says another fact's
// statement names it: that edge is the only link the graph has to the name, and nothing
// re-creates the entity (operator's ruling, 2026-11-28). Every other edge is ignored.
// A bare bothE() also counted the TOUCHED edges the runner writes after every memory
// call that names the entity, memory_forget included, so the entity was never an
// orphan to the database while every listing showed it with no facts (lab VM,
// 2026-11-28). Deleting the vertex takes those edges with it: "a vertex is deleted
// with its edges all the same" (ArcadeDB docs, reference/sql/sql-delete.adoc).
const (
	factDegree   = "bothE('" + factEdgeType + "').size()"
	anchorDegree = "bothE('" + factEdgeType + "','" + mentionsEdgeType + "').size()"

	pruneOrphanEntityStatement = "DELETE FROM Entity WHERE name = :name AND " + anchorDegree + " = 0"

	// A MENTIONS edge goes with the fact it was read from, so it never holds a name
	// after that fact is gone.
	deleteFactMentionsStatement = "DELETE FROM " + mentionsEdgeType + " WHERE fact_rid IN :rids"
	factMentionsStatement       = "SELECT outV().name AS source, inV().name AS target FROM " +
		mentionsEdgeType + " WHERE fact_rid IN :rids"
	countFactMentionsStatement = "SELECT count(*) AS mentions FROM " + mentionsEdgeType +
		" WHERE fact_rid IN :rids AND (outV().name = :name OR inV().name = :name)"
)

func (c *Client) pruneOrphans(ctx context.Context, names []string) (int, error) {
	removed := 0
	for _, name := range names {
		rows, err := c.Command(ctx, pruneOrphanEntityStatement, map[string]any{"name": name})
		if err != nil {
			return removed, fmt.Errorf("arcadedb: prune entity %q: %w", name, err)
		}
		removed += countUpdated(rows)
	}
	return removed, nil
}

// mentionEndpoints names the entities the deleted facts' MENTIONS edges touch: once
// those edges go, a name only they held is an orphan too.
func (c *Client) mentionEndpoints(ctx context.Context, rids []string) ([]string, error) {
	if len(rids) == 0 {
		return nil, nil
	}
	rows, err := c.Query(ctx, factMentionsStatement, map[string]any{"rids": rids})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: read mentions of forgotten facts: %w", err)
	}
	names := []string{}
	for _, row := range rows {
		names = appendUnique(names, rowString(row, "source"))
		names = appendUnique(names, rowString(row, "target"))
	}
	return names, nil
}

func (c *Client) deleteFactMentions(ctx context.Context, rids []string) error {
	if len(rids) == 0 {
		return nil
	}
	if _, err := c.Command(ctx, deleteFactMentionsStatement, map[string]any{"rids": rids}); err != nil {
		return fmt.Errorf("arcadedb: forget mentions of forgotten facts: %w", err)
	}
	return nil
}

func (c *Client) countStrandableByRIDs(
	ctx context.Context,
	names []string,
	rids []string,
) (int, error) {
	stranded := 0
	for _, name := range names {
		params := map[string]any{"name": name, "rids": rids}
		total, err := c.Query(ctx,
			"SELECT "+anchorDegree+" AS degree FROM Entity WHERE name = :name", params)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: read degree of %q: %w", name, err)
		}
		going, err := c.Query(ctx,
			"SELECT count(*) AS n FROM "+factEdgeType+
				" WHERE @rid IN :rids AND (outV().name = :name OR inV().name = :name)", params)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: count edges leaving %q: %w", name, err)
		}
		mentions, err := c.Query(ctx, countFactMentionsStatement, params)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: count mentions leaving %q: %w", name, err)
		}
		if len(total) == 1 && len(going) == 1 && len(mentions) == 1 &&
			rowInt(total[0], "degree") == rowInt(going[0], "n")+rowInt(mentions[0], "mentions") {
			stranded++
		}
	}
	return stranded, nil
}
