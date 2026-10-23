package docfields

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// CustomFilter compares canonical custom value keys, rather than display values.
type CustomFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
}

func validateWorkspaceQuery(q ListQuery) error {
	if !validFileType(q.FileType) {
		return invalid("unknown file type %q", q.FileType)
	}
	if q.SearchMode != "" && q.SearchMode != "name" && q.SearchMode != "content" {
		return invalid("unknown search mode %q", q.SearchMode)
	}
	if len(q.Search) > 16384 || len(q.Custom) > 50 || len(q.SourceKeys)+len(q.SourcePrefixes) > 200 || len(q.DocumentIDs) > 200 {
		return invalid("document query exceeds its bounds")
	}
	for _, f := range q.Custom {
		if !strings.HasPrefix(f.Field, customFieldPrefix) || len(f.Field) <= len(customFieldPrefix) || len(f.Field) > 256 {
			return invalid("invalid custom field %q", f.Field)
		}
		if f.Operator != "eq" && f.Operator != "unassigned" {
			return invalid("unknown custom filter operator %q", f.Operator)
		}
		if (f.Operator == "eq" && strings.TrimSpace(f.Value) == "") || (f.Operator == "unassigned" && f.Value != "") || len(f.Value) > 4096 {
			return invalid("invalid custom filter value")
		}
	}
	return nil
}

func workspacePredicates(q ListQuery, params map[string]any) []string {
	var out []string
	if predicate := fileTypePredicate(q.FileType, params); predicate != "" {
		out = append(out, predicate)
	}
	if q.SearchMode == "content" && strings.TrimSpace(q.Search) != "" {
		// SEARCH_INDEX takes Lucene syntax; phrase quoting keeps archive search literal.
		// https://docs.arcadedb.com/arcadedb/how-to/data-modeling/full-text-index
		params["content_search"] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.TrimSpace(q.Search)) + `"`
		out = append(out, "source_key IN (SELECT source_key FROM Passage WHERE SEARCH_INDEX('Passage[text]', :content_search))")
	}
	if q.Inbox {
		out = append(out, "source_key NOT IN (SELECT source_key FROM DocumentFields WHERE reviewed_at IS NOT NULL)")
	}
	if scope := arcadedb.SourcePredicate(q.SourceKeys, q.SourcePrefixes, params); scope != "" {
		out = append(out, scope)
	}
	if len(q.DocumentIDs) > 0 {
		params["document_ids"] = q.DocumentIDs
		out = append(out, "search_document_id IN :document_ids")
	}
	for i, f := range q.Custom {
		field, value := fmt.Sprintf("custom_field_%d", i), fmt.Sprintf("custom_value_%d", i)
		params[field] = f.Field
		predicate := "field = :" + field
		if !q.Fields.IncludeUnreviewed {
			predicate += ` AND (set_by = 'operator' OR accepted = true)`
		}
		operator := "IN"
		if f.Operator == "eq" {
			params[value] = f.Value
			predicate += " AND value_key = :" + value
		} else {
			operator = "NOT IN"
		}
		out = append(out, "source_key "+operator+" (SELECT source_key FROM DocumentFields WHERE values CONTAINS ("+predicate+"))")
	}
	return out
}
