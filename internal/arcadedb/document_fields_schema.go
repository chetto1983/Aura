package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The document fields types live in the identity's own database, beside the passages CocoIndex
// writes, so the server isolates them the way it isolates memory. They are Go's: CocoIndex never
// names them, so no reconcile touches them (docs/superpowers/verification/2026-10-18-document-fields-mirror.md).
const (
	DocumentLabelType       = "DocumentLabel"
	DocumentCustomFieldType = "DocumentCustomField"
	DocumentFieldsType      = "DocumentFields"
	DocumentFieldEventType  = "DocumentFieldEvent"
	DocumentSavedViewType   = "DocumentSavedView"
)

func documentFieldsSchemaStatements() []string {
	return []string{
		"CREATE DOCUMENT TYPE DocumentSavedView IF NOT EXISTS",
		"CREATE PROPERTY DocumentSavedView.id IF NOT EXISTS STRING",
		"CREATE PROPERTY DocumentSavedView.name_key IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON DocumentSavedView (id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON DocumentSavedView (name_key) UNIQUE",
		"CREATE DOCUMENT TYPE DocumentLabel IF NOT EXISTS",
		"CREATE PROPERTY DocumentLabel.id IF NOT EXISTS STRING",
		"CREATE PROPERTY DocumentLabel.kind IF NOT EXISTS STRING",
		"CREATE PROPERTY DocumentLabel.name_key IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON DocumentLabel (id) UNIQUE",
		// Names are unique per kind, compared lowered and trimmed: name_key is that form.
		"CREATE INDEX IF NOT EXISTS ON DocumentLabel (kind, name_key) UNIQUE",
		"CREATE DOCUMENT TYPE DocumentCustomField IF NOT EXISTS",
		"CREATE PROPERTY DocumentCustomField.id IF NOT EXISTS STRING",
		"CREATE PROPERTY DocumentCustomField.name_key IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON DocumentCustomField (id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON DocumentCustomField (name_key) UNIQUE",
		"CREATE DOCUMENT TYPE DocumentFields IF NOT EXISTS",
		"CREATE PROPERTY DocumentFields.source_key IF NOT EXISTS STRING",
		"CREATE PROPERTY DocumentFields.record_id IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON DocumentFields (source_key) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON DocumentFields (record_id) UNIQUE",
		"CREATE DOCUMENT TYPE DocumentFieldEvent IF NOT EXISTS",
		"CREATE PROPERTY DocumentFieldEvent.record_id IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON DocumentFieldEvent (record_id) NOTUNIQUE",
	}
}

// EnsureDocumentFieldsSchema creates the document fields types and indexes, and the triggers that
// keep their projection on the indexed rows. It reports whether the triggers are in place: they
// need ingest's Passage and IndexedDocument types, and ArcadeDB refuses a trigger on a type that
// does not exist (measured 2026-10-18 on 26.10.1), so until the identity's first ingest the caller
// asks again. Creating the types here instead would turn an empty library, which retrieval reads
// as one, into a schema with none of ingest's indexes.
func (c *Client) EnsureDocumentFieldsSchema(ctx context.Context) (bool, error) {
	if err := c.EnsureDocumentFieldsTypes(ctx); err != nil {
		return false, err
	}
	return c.EnsureDocumentFieldsTriggers(ctx)
}

// EnsureDocumentFieldsTypes creates the document fields types and indexes. Two DDL passes over
// one database at once can fail inside ArcadeDB (an internal NullPointerException, measured
// 2026-10-18 on 26.10.1), so a caller runs it once per database, not once per request.
func (c *Client) EnsureDocumentFieldsTypes(ctx context.Context) error {
	for _, statement := range documentFieldsSchemaStatements() {
		if _, err := c.Command(ctx, statement, nil); err != nil {
			return fmt.Errorf("arcadedb: ensure document fields schema: %w", err)
		}
	}
	return nil
}

// EnsureDocumentFieldsTriggers puts the triggers in place once ingest's types exist, and reports
// whether they are.
func (c *Client) EnsureDocumentFieldsTriggers(ctx context.Context) (bool, error) {
	rows, err := c.Query(ctx, "SELECT name FROM schema:types WHERE name IN :names",
		map[string]any{"names": documentFieldsTargets})
	if err != nil {
		return false, fmt.Errorf("arcadedb: read the indexed types: %w", err)
	}
	if len(rows) < len(documentFieldsTargets) {
		return false, nil
	}
	if err := c.ensureDocumentFieldsTriggers(ctx); err != nil {
		return false, err
	}
	if err := c.restampDocumentFields(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// IsDuplicateKey reports a write a UNIQUE index refused.
func IsDuplicateKey(err error) bool {
	var serverErr *ServerError
	return errors.As(err, &serverErr) && strings.Contains(serverErr.Exception, "DuplicatedKeyException")
}
