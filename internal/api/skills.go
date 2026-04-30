package api

import (
	"errors"
	"net/http"
	"os"
	"regexp"
)

// skillNameRe mirrors the loader's allowed character set so a malicious
// name in the URL can't escape the skills directory.
var skillNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// maxSkillBodyChars caps the body returned by GET /skills/{name} so a
// runaway SKILL.md can't blow out the dashboard's fetch buffer. Matches
// the LLM-tool truncation boundary.
const maxSkillBodyChars = 16000

func handleSkillsList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Skills == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []SkillSummary{})
			return
		}
		loaded, err := deps.Skills.LoadAll()
		if err != nil {
			deps.Logger.Warn("api: list skills", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list skills")
			return
		}
		out := make([]SkillSummary, 0, len(loaded))
		for _, s := range loaded {
			out = append(out, SkillSummary{Name: s.Name, Description: s.Description})
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleSkillGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Skills == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "skill not found")
			return
		}
		name := r.PathValue("name")
		if !skillNameRe.MatchString(name) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid skill name")
			return
		}
		skill, err := deps.Skills.LoadByName(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, deps.Logger, http.StatusNotFound, "skill not found")
				return
			}
			deps.Logger.Warn("api: read skill", "name", name, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read skill")
			return
		}
		body := skill.Content
		truncated := false
		if len(body) > maxSkillBodyChars {
			body = body[:maxSkillBodyChars]
			truncated = true
		}
		writeJSON(w, deps.Logger, http.StatusOK, SkillDetail{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     body,
			Truncated:   truncated,
		})
	}
}
