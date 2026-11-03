package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ExampleType holds what a person confirmed, as vectors: the store every domain that learns from
// the person shares (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md). It is not in
// memorySpaceTypes, because the space pass re-embeds a row from its text and an example keeps
// none: an example from another space is left out, never re-embedded.
const ExampleType = "ConfirmedExample"

// Example is one thing a person confirmed: in which domain, whose it is (Owner, whose removal
// forgets it), which item it was made from (Ref), the label, and the vector in its space.
type Example struct {
	Domain, Owner, Ref, Label string
	Vector                    []float64
	Space                     string
	CreatedAt                 time.Time
}

// exampleSchemaStatements declare the type. A bank holds tens of rows and is read whole into a
// brute-force classifier, so there is no vector index.
func exampleSchemaStatements() []string {
	return append([]string{
		"CREATE VERTEX TYPE " + ExampleType + " IF NOT EXISTS",
		"CREATE PROPERTY " + ExampleType + ".domain IF NOT EXISTS STRING",
		"CREATE PROPERTY " + ExampleType + ".owner_ref IF NOT EXISTS STRING",
		"CREATE PROPERTY " + ExampleType + ".item_ref IF NOT EXISTS STRING",
		"CREATE PROPERTY " + ExampleType + ".label IF NOT EXISTS STRING",
		"CREATE PROPERTY " + ExampleType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE PROPERTY " + ExampleType + ".created_at IF NOT EXISTS DATETIME",
		"CREATE INDEX IF NOT EXISTS ON " + ExampleType + " (domain, owner_ref, item_ref) UNIQUE",
	}, spaceStampStatements(ExampleType)...)
}

const putExampleStatement = "UPDATE " + ExampleType +
	" SET domain = :domain, owner_ref = :owner_ref, item_ref = :item_ref, label = :label," +
	" embedding = :embedding, embed_space = :embed_space, created_at = :created_at" +
	" UPSERT WHERE domain = :domain AND owner_ref = :owner_ref AND item_ref = :item_ref"

// PutExample stores an example. A second confirmation of the same item replaces the first.
func (c *Client) PutExample(ctx context.Context, e Example) error {
	if err := e.check(); err != nil {
		return err
	}
	_, err := c.Command(ctx, putExampleStatement, map[string]any{
		"domain": e.Domain, "owner_ref": e.Owner, "item_ref": e.Ref, "label": e.Label,
		"embedding": e.Vector, "embed_space": e.Space, "created_at": nullableTime(e.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("arcadedb: put example: %w", err)
	}
	return nil
}

// check refuses an example that could never be read back or forgotten.
func (e Example) check() error {
	for name, value := range map[string]string{
		"domain": e.Domain, "owner": e.Owner, "ref": e.Ref, "label": e.Label, "space": e.Space,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("arcadedb: an example needs its %s", name)
		}
	}
	if len(e.Vector) == 0 {
		return errors.New("arcadedb: an example needs its vector")
	}
	return nil
}

// Examples reads a domain's examples in one space. An example from any other space is not there.
func (c *Client) Examples(ctx context.Context, domain, space string) ([]Example, error) {
	rows, err := c.Query(ctx,
		"SELECT owner_ref, item_ref, label, embedding FROM "+ExampleType+" WHERE domain = :domain AND embed_space = :space",
		map[string]any{"domain": domain, "space": space})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: read examples: %w", err)
	}
	out := make([]Example, 0, len(rows))
	for _, row := range rows {
		out = append(out, Example{
			Domain: domain, Owner: rowString(row, "owner_ref"), Ref: rowString(row, "item_ref"),
			Label: rowString(row, "label"), Vector: rowFloats(row, "embedding"), Space: space,
		})
	}
	return out, nil
}

// ForgetExamples removes an owner's examples in a domain and says how many went.
func (c *Client) ForgetExamples(ctx context.Context, domain, owner string) (int, error) {
	rows, err := c.Command(ctx, "DELETE FROM "+ExampleType+" WHERE domain = :domain AND owner_ref = :owner_ref",
		map[string]any{"domain": domain, "owner_ref": owner})
	if err != nil {
		return 0, fmt.Errorf("arcadedb: forget examples: %w", err)
	}
	return countUpdated(rows), nil
}
