package docfields

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// NamedValue is one field value by name, as the agent reads it. Reviewed is a value the operator
// set or accepted, which is what include_unreviewed=false keeps.
type NamedValue struct {
	Name     string `json:"name"`
	Reviewed bool   `json:"reviewed"`
}

// FieldNames are one document's fields by name: what a filter matches on, so a proposed new name,
// a label since deleted and the custom fields are left out.
type FieldNames struct {
	Type         *NamedValue  `json:"type,omitempty"`
	Counterparty *NamedValue  `json:"counterparty,omitempty"`
	DocumentDate *NamedValue  `json:"document_date,omitempty"`
	Tags         []NamedValue `json:"tags,omitempty"`
}

// DocumentFields names the fields of each listed document this identity has. An id that is no
// document of its is absent; a document nobody labelled maps to no field.
func (s *Store) DocumentFields(ctx context.Context, identityID string, documentIDs []string) (map[string]FieldNames, error) {
	out := make(map[string]FieldNames, len(documentIDs))
	if len(documentIDs) == 0 {
		return out, nil
	}
	c, err := s.client(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, "SELECT "+documentRowColumns+" FROM IndexedDocument WHERE search_document_id IN :ids", map[string]any{"ids": documentIDs})
	if arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the documents: %w", err)
	}
	documents, err := withFields(ctx, c, decodeRows(rows))
	if err != nil {
		return nil, err
	}
	labels, err := c.Query(ctx, "SELECT id, name FROM DocumentLabel", nil)
	if err != nil {
		return nil, fmt.Errorf("read the labels: %w", err)
	}
	names := make(map[string]string, len(labels))
	for _, label := range labels {
		names[str(label["id"])] = str(label["name"])
	}
	for _, document := range documents {
		out[document.ID] = nameFields(document.Values, names)
	}
	return out, nil
}

// nameFields names values by their labels. A date is its own name.
func nameFields(values []Value, names map[string]string) FieldNames {
	var f FieldNames
	for _, v := range values {
		if strings.HasPrefix(v.ValueKey, proposedPrefix) {
			continue
		}
		named := NamedValue{Name: names[v.ValueKey], Reviewed: v.reviewed()}
		if v.Field == FieldDocumentDate {
			named.Name = v.ValueKey
		}
		if named.Name == "" {
			continue
		}
		switch v.Field {
		case FieldType:
			f.Type = &named
		case FieldCounterparty:
			f.Counterparty = &named
		case FieldDocumentDate:
			f.DocumentDate = &named
		case FieldTag:
			f.Tags = append(f.Tags, named)
		}
	}
	return f
}
