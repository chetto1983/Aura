package docfields

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// SavedView preserves the versioned URL query, including repeated filter values.
type SavedView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Query     string `json:"query"`
	Display   string `json:"display"`
	Pinned    bool   `json:"pinned"`
	Dashboard bool   `json:"dashboard"`
}

func validateView(v *SavedView) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 80 {
		return invalid("a saved view name has 1 to 80 characters")
	}
	if v.Version != 1 {
		return invalid("unsupported saved view version")
	}
	if v.Display != "table" && v.Display != "compact" && v.Display != "cards" {
		return invalid("invalid saved view display")
	}
	if len(v.Query) > 16384 {
		return invalid("saved view query exceeds 16 KiB")
	}
	values, err := url.ParseQuery(v.Query)
	if err != nil {
		return invalid("invalid saved view query")
	}
	allowed := map[string]bool{"q": true, "search_mode": true, "inbox": true, "source_key": true, "custom": true, "type": true, "counterparty": true, "tags": true, "type_not": true, "counterparty_not": true, "tags_not": true, "unassigned": true, "tags_mode": true, "date_from": true, "date_to": true, "include_unreviewed": true, "needs_review": true, "sort": true, "offset": true, "limit": true}
	repeated := map[string]bool{"type": true, "counterparty": true, "tags": true, "type_not": true, "counterparty_not": true, "tags_not": true, "unassigned": true, "source_key": true}
	for key, items := range values {
		if !allowed[key] || (!repeated[key] && len(items) > 1) {
			return invalid("invalid saved view query parameter %q", key)
		}
	}
	_, err = ParseListQuery(values)
	return err
}

// Views reads only the resolved identity's database; no ownership clause is needed in SQL.
func (s *Store) Views(ctx context.Context, identityID string) ([]SavedView, error) {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, "SELECT FROM DocumentSavedView ORDER BY name_key, id", nil)
	if err != nil {
		return nil, fmt.Errorf("list saved document views: %w", err)
	}
	out := make([]SavedView, 0, len(rows))
	for _, row := range rows {
		version, _ := row["version"].(float64)
		pinned, _ := row["pinned"].(bool)
		dashboard, _ := row["dashboard"].(bool)
		out = append(out, SavedView{ID: str(row["id"]), Name: str(row["name"]), Version: int(version), Query: str(row["query"]), Display: str(row["display"]), Pinned: pinned, Dashboard: dashboard})
	}
	return out, nil
}

// SaveView validates the saved query before resolving a database or changing named records.
func (s *Store) SaveView(ctx context.Context, identityID string, v SavedView) (SavedView, error) {
	if err := validateView(&v); err != nil {
		return SavedView{}, err
	}
	c, err := s.client(ctx, identityID)
	if err != nil {
		return SavedView{}, err
	}
	v.ID, err = saveNamed(ctx, c, "DocumentSavedView", v.ID, "name = :name, name_key = :name_key, version = :version, query = :query, display = :display, pinned = :pinned, dashboard = :dashboard", map[string]any{"name": v.Name, "name_key": NameKey(v.Name), "version": v.Version, "query": v.Query, "display": v.Display, "pinned": v.Pinned, "dashboard": v.Dashboard}, s.now().Format(timeLayout), fmt.Sprintf("a saved view named %q already exists", v.Name))
	return v, err
}

// DeleteView reports the same not-found error for unknown and foreign view IDs.
func (s *Store) DeleteView(ctx context.Context, identityID, id string) error {
	c, err := s.client(ctx, identityID)
	if err != nil {
		return err
	}
	return deleteNamed(ctx, c, "DocumentSavedView", id)
}
