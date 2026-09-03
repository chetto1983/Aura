package arcadedb

// The entity classes, as ArcadeDB's own graph-RAG reference models them.
//
// That reference (ArcadeData/arcadedb-usecases, graph-rag/sql/01-schema.sql) does not
// carry an entity's class in a string property. It declares the classes as VERTEX TYPES
// extending a common Entity:
//
//	CREATE VERTEX TYPE Entity IF NOT EXISTS;
//	CREATE VERTEX TYPE Person IF NOT EXISTS EXTENDS Entity;
//	CREATE VERTEX TYPE Concept IF NOT EXISTS EXTENDS Entity;
//	CREATE VERTEX TYPE Organization IF NOT EXISTS EXTENDS Entity;
//
// This memory shipped a free-text `kind` instead, and the difference is not cosmetic: a
// string has no closed set, so nothing stops the next writer inventing the next label.
// Measured 2026-09-03 across three independent import agents on the same corpus, each
// briefed with the list already in use: they coined Model, Dataset and Pattern anyway,
// taking the vocabulary from eight labels to eleven in a single afternoon. That is the
// same curve the entity NAMES were on before the vocabulary reply was added, one level up.
//
// The class vocabulary is POLE+O — Person, Object, Location, Event, Organisation — the
// model Neo4j publishes for investigative graphs, plus the explicit Other its own POLE
// example carries as an escape hatch (neo4j-graph-examples/pole). Six types, closed. A
// write that names a class outside the set does not widen the set and does not fail: the
// entity lands in Other and the reply says the request was refused, the same shape the
// vocabulary reply already uses. Refusing a whole fact over a typo in an optional field
// would cost more than it protects.
//
// `kind` SURVIVES as the concrete refinement, because that is what the official POLE model
// does too: Officer beside Person, Vehicle and Phone beside Object, PostCode beside
// Location. The class is the frame; the kind is the detail inside it.
//
// Uniqueness across the subtypes was probed, not assumed. Against a live 26.x server, a
// UNIQUE index declared on the supertype rejects a duplicate name inserted into a sibling
// subtype:
//
//	CREATE VERTEX Object SET name = 'Aura'
//	-> DuplicatedKeyException: Duplicated key [Aura] found on index 'Entity[name]'
//	   already assigned to record #5:0
//
// so one name still means one entity, and `SELECT FROM Entity` still returns every class —
// ArcadeDB treats queries as polymorphic by default and searches the buckets of the target
// type and all subtypes (docs, concepts/inheritance.adoc).

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// poleOther is the class for an entity that fits none of the five. The official POLE
// example ships the same escape hatch; without one, every awkward entity would either be
// refused or be forced into a class that misdescribes it.
const poleOther = "Other"

// poleClasses is the closed set, in the order the model names them. Adding to this list is
// a deliberate schema change, which is the entire point of it being a list.
var poleClasses = []string{"Person", "Object", "Location", "Event", "Organisation", poleOther}

// poleByKind maps the concrete kinds this corpus actually used onto their class. It is
// short on purpose: it covers what was measured in use on 2026-09-03, and anything else
// falls to Other rather than being guessed at. A writer who knows better passes the class
// explicitly.
var poleByKind = map[string]string{
	"person":       "Person",
	"officer":      "Person",
	"organisation": "Organisation",
	"organization": "Organisation",
	"team":         "Organisation",
	"company":      "Organisation",
	"location":     "Location",
	"environment":  "Location",
	"place":        "Location",
	"event":        "Event",
	"phase":        "Event",
	"milestone":    "Event",
	"incident":     "Event",
	"object":       "Object",
	"tool":         "Object",
	"system":       "Object",
	"project":      "Object",
	"model":        "Object",
	"dataset":      "Object",
	"branch":       "Object",
	"technique":    "Object",
	"pattern":      "Object",
	"document":     "Object",
	"concept":      "Object",
}

// poleSchemaStatements declare the classes as subtypes, mirroring the reference schema.
// Every statement is idempotent, so this runs on every boot alongside the rest.
func poleSchemaStatements() []string {
	statements := make([]string, 0, len(poleClasses))
	for _, class := range poleClasses {
		statements = append(statements,
			"CREATE VERTEX TYPE "+class+" IF NOT EXISTS EXTENDS Entity")
	}
	return statements
}

// validPoleClass reports the canonical spelling of class, and whether it is in the set.
// Matching folds case so `person` and `Person` are the same class rather than a refusal
// the caller cannot act on.
func validPoleClass(class string) (string, bool) {
	folded := strings.ToLower(strings.TrimSpace(class))
	if folded == "" {
		return "", false
	}
	for _, known := range poleClasses {
		if strings.ToLower(known) == folded {
			return known, true
		}
	}
	return "", false
}

// poleClassFor resolves the class an entity should be written as. An explicit class wins;
// otherwise the kind decides; otherwise Other, which is a real answer rather than a
// failure. The bool reports whether an explicitly requested class was rejected, so the
// caller can say so instead of silently substituting.
func poleClassFor(requested, kind string) (class string, refusedRequest bool) {
	if strings.TrimSpace(requested) != "" {
		if canonical, ok := validPoleClass(requested); ok {
			return canonical, false
		}
		return poleOther, true
	}
	if mapped, ok := poleByKind[strings.ToLower(strings.TrimSpace(kind))]; ok {
		return mapped, false
	}
	return poleOther, false
}

// PoleClassList is the set as a caller-facing sentence, so a tool description or a refusal
// names what IS allowed rather than only what is not. Exported because the MCP surface has
// to state the closed set, and restating it there by hand is how the two drift apart.
func PoleClassList() string {
	return strings.Join(poleClasses, ", ")
}

// entityClassScan reads which class each named entity already has. The class is decided
// once, at first write: an entity that exists as a Person is not re-minted as an Object
// just because a later fact typed it differently, because that write would hit the unique
// name index and fail. Reporting the divergence is more useful than either outcome.
func (c *Client) entityClassScan(ctx context.Context, names ...string) (map[string]string, error) {
	wanted := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, twice := seen[name]; twice {
			continue
		}
		seen[name] = struct{}{}
		wanted = append(wanted, name)
	}
	if len(wanted) == 0 {
		return map[string]string{}, nil
	}
	sort.Strings(wanted)
	rows, err := c.Query(ctx,
		"SELECT name, @type AS pole FROM Entity WHERE name IN :names",
		map[string]any{"names": wanted})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: scan entity classes: %w", err)
	}
	held := make(map[string]string, len(rows))
	for _, row := range rows {
		if name := rowString(row, "name"); name != "" {
			held[name] = rowString(row, "pole")
		}
	}
	return held, nil
}

// upsertEntityInClass is the reference's own UPSERT, aimed at a subtype instead of the
// bare supertype. Probed live: `UPDATE Object SET name = :name UPSERT RETURN AFTER WHERE
// name = :name` resolves an existing subtype record and creates one when absent, exactly
// as it does on Entity.
func upsertEntityInClass(class string, typed bool) string {
	if typed {
		return "UPDATE " + class + " SET name = :name, kind = :kind UPSERT " +
			"RETURN AFTER WHERE name = :name"
	}
	return "UPDATE " + class + " SET name = :name UPSERT RETURN AFTER WHERE name = :name"
}
