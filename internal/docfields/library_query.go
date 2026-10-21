package docfields

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// ParseCustomFilters rejects unknown fields and trailing JSON at the shared query boundary.
func ParseCustomFilters(raw string) ([]CustomFilter, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 16384 || !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		return nil, invalid("custom filters must be a bounded JSON array")
	}
	var out []CustomFilter
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, invalid("invalid custom filters: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, invalid("invalid trailing custom filters")
	}
	return out, validateWorkspaceQuery(ListQuery{Custom: out})
}

// ParseListQuery keeps the saved-view and HTTP query validators identical.
func ParseListQuery(v url.Values) (ListQuery, error) {
	q := ListQuery{Fields: arcadedb.FieldFilter{Types: v["type"], Counterparties: v["counterparty"], Tags: v["tags"], TagsAll: v.Get("tags_mode") == "all", DateFrom: v.Get("date_from"), DateTo: v.Get("date_to"), IncludeUnreviewed: true},
		Exclude: arcadedb.FieldFilter{Types: v["type_not"], Counterparties: v["counterparty_not"], Tags: v["tags_not"]}, Unassigned: v["unassigned"], Search: v.Get("q"), SearchMode: v.Get("search_mode"), FileType: NormalizeFileType(v.Get("file_type")), SourceKeys: v["source_key"], Sort: v.Get("sort")}
	if mode := v.Get("tags_mode"); mode != "" && mode != "all" && mode != "any" {
		return ListQuery{}, invalid("unknown tags mode")
	}
	for name, dst := range map[string]*bool{"include_unreviewed": &q.Fields.IncludeUnreviewed, "needs_review": &q.NeedsReview, "inbox": &q.Inbox} {
		if raw := v.Get(name); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return ListQuery{}, invalid("invalid %s", name)
			}
			*dst = value
		}
	}
	for name, dst := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		if raw := v.Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				return ListQuery{}, invalid("invalid %s", name)
			}
			*dst = value
		}
	}
	var err error
	if q.Custom, err = ParseCustomFilters(v.Get("custom")); err != nil {
		return ListQuery{}, err
	}
	if _, err = listOrder(q.Sort); err != nil {
		return ListQuery{}, err
	}
	if _, err = arcadedb.FieldPredicate(q.Fields, map[string]any{}); err != nil {
		return ListQuery{}, invalid("%v", err)
	}
	if _, err = libraryPredicates(q, map[string]any{}); err != nil {
		return ListQuery{}, err
	}
	return q, nil
}
