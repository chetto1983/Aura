package arcadedb

import (
	"context"
	"fmt"
)

// A removal that did not name an entity leaves it while a FACT edge holds it, or a
// MENTIONS edge says another fact's statement names it: that edge is the only link the
// graph has to the name, and nothing re-creates the entity (operator's ruling,
// 2026-11-28). A removal that names it removes it once no FACT edge holds it, with its
// MENTIONS edges, and leaves the facts whose statements name it: otherwise the only way
// to clear such a name was to delete the facts that mention it (lab VM, 2026-11-28).
// Every other edge is ignored. A bare bothE() also counted the TOUCHED edges the runner
// writes after every memory call that names the entity, memory_forget included, so the
// entity was never an orphan to the database while every listing showed it with no
// facts. Deleting the vertex takes its edges with it: "a vertex is deleted with its
// edges all the same" (ArcadeDB docs, reference/sql/sql-delete.adoc).
const (
	factDegree   = "bothE('" + factEdgeType + "').size()"
	anchorDegree = "bothE('" + factEdgeType + "','" + mentionsEdgeType + "').size()"

	pruneOrphanEntityStatement = "DELETE FROM Entity WHERE name = :name AND " + anchorDegree + " = 0"
	pruneNamedEntityStatement  = "DELETE FROM Entity WHERE name = :name AND " + factDegree + " = 0"

	// A MENTIONS edge goes with the fact it was read from, so it never holds a name
	// after that fact is gone.
	deleteFactMentionsStatement = "DELETE FROM " + mentionsEdgeType + " WHERE fact_rid IN :rids"
	factMentionsStatement       = "SELECT outV().name AS source, inV().name AS target FROM " +
		mentionsEdgeType + " WHERE fact_rid IN :rids"
	countFactMentionsStatement = "SELECT count(*) AS mentions FROM " + mentionsEdgeType +
		" WHERE fact_rid IN :rids AND (outV().name = :name OR inV().name = :name)"
)

// pruneStatementFor is the prune of name when the removal named the entity named.
func pruneStatementFor(name, named string) string {
	if name == named {
		return pruneNamedEntityStatement
	}
	return pruneOrphanEntityStatement
}

func (c *Client) pruneOrphans(ctx context.Context, names []string, named string) (int, error) {
	removed := 0
	for _, name := range names {
		rows, err := c.Command(ctx, pruneStatementFor(name, named), map[string]any{"name": name})
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

// countStrandableByRIDs counts the names the removal would prune: those whose holding
// edges are exactly the ones leaving with the facts in rids.
func (c *Client) countStrandableByRIDs(
	ctx context.Context,
	names []string,
	rids []string,
	named string,
) (int, error) {
	stranded := 0
	for _, name := range names {
		params := map[string]any{"name": name, "rids": rids}
		degree := anchorDegree
		if name == named {
			degree = factDegree
		}
		total, err := c.Query(ctx, "SELECT "+degree+" AS degree FROM Entity WHERE name = :name", params)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: read degree of %q: %w", name, err)
		}
		going, err := c.Query(ctx,
			"SELECT count(*) AS n FROM "+factEdgeType+
				" WHERE @rid IN :rids AND (outV().name = :name OR inV().name = :name)", params)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: count edges leaving %q: %w", name, err)
		}
		leaving := int64(0)
		if len(going) == 1 {
			leaving = rowInt(going[0], "n")
		}
		if name != named {
			mentions, err := c.Query(ctx, countFactMentionsStatement, params)
			if err != nil {
				return 0, fmt.Errorf("arcadedb: count mentions leaving %q: %w", name, err)
			}
			if len(mentions) != 1 {
				continue
			}
			leaving += rowInt(mentions[0], "mentions")
		}
		if len(total) == 1 && len(going) == 1 && rowInt(total[0], "degree") == leaving {
			stranded++
		}
	}
	return stranded, nil
}
