package api

import (
	"net/http"
	"strconv"
	"strings"
)

// handleSkillsCatalog proxies skills.sh catalog search through the
// authenticated API so the frontend doesn't have to deal with CORS or
// scrape the public site itself. Read-only — install lives behind the
// admin gate (see skills_write.go).
func handleSkillsCatalog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.SkillsCatalog == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []SkillCatalogItem{})
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		limit := 25
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 50 {
				limit = n
			}
		}
		items, err := deps.SkillsCatalog.Search(r.Context(), query, limit)
		if err != nil {
			deps.Logger.Warn("api: skills catalog", "error", err, "query", query)
			writeError(w, deps.Logger, http.StatusBadGateway, "skills catalog unavailable")
			return
		}
		out := make([]SkillCatalogItem, 0, len(items))
		for _, item := range items {
			out = append(out, SkillCatalogItem{
				Source:         item.Source,
				SkillID:        item.SkillID,
				Name:           item.Name,
				Installs:       item.Installs,
				InstallCommand: item.InstallCommand(),
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}
