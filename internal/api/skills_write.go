package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// SkillInstaller is the boundary the API uses to add a skill from the
// skills.sh catalog. Bot wires the real implementation backed by `npx
// skills add ...`; tests inject a fake.
type SkillInstaller interface {
	Install(ctx context.Context, source, skillID string) (output string, err error)
}

// SkillDeleter removes a single local skill directory under the
// configured skills root. Implementations are responsible for path
// containment (no traversal, only direct children of the root).
type SkillDeleter interface {
	Delete(name string) error
}

// ErrSkillNotFound is returned by SkillDeleter when the named skill
// does not exist on disk.
var ErrSkillNotFound = errors.New("skill not found")

// catalogSourceRE constrains accepted Source values for the install
// endpoint. The catalog returns either a github shorthand
// (`user/repo`), a github URL, or an npm package. We accept the same
// safe character set those forms use and never invoke a shell, so the
// risk surface is the npx subprocess itself, not interpolation.
var catalogSourceRE = regexp.MustCompile(`^[A-Za-z0-9@:._/\-]{1,200}$`)

// catalogSkillIDRE matches the skills.sh skillId format.
var catalogSkillIDRE = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)

// installSkillRequest is the body of POST /skills/install.
type installSkillRequest struct {
	Source  string `json:"source"`
	SkillID string `json:"skill_id,omitempty"`
}

func handleSkillInstall(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.SkillsAdmin {
			writeError(w, deps.Logger, http.StatusForbidden, "skill install disabled (set SKILLS_ADMIN=true)")
			return
		}
		if deps.SkillsInstaller == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "skill installer unavailable")
			return
		}
		var req installSkillRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		req.Source = strings.TrimSpace(req.Source)
		req.SkillID = strings.TrimSpace(req.SkillID)
		if req.Source == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "source is required")
			return
		}
		if !catalogSourceRE.MatchString(req.Source) || strings.Contains(req.Source, "..") {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid source")
			return
		}
		if req.SkillID != "" && !catalogSkillIDRE.MatchString(req.SkillID) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid skill_id")
			return
		}
		// Install can be slow (npm cold cache, github clone) — give the
		// process a generous ceiling but cap it so a hung install doesn't
		// pin a request goroutine forever.
		ctx, cancel := context.WithTimeout(r.Context(), installTimeout)
		defer cancel()
		out, err := deps.SkillsInstaller.Install(ctx, req.Source, req.SkillID)
		if err != nil {
			deps.Logger.Warn("api: skill install failed", "source", req.Source, "skill_id", req.SkillID, "error", err)
			// Surface the first 2 KiB of stdout/stderr to the dashboard so
			// the user sees what went wrong without us echoing arbitrary
			// shell output back into the JSON body.
			writeJSON(w, deps.Logger, http.StatusBadGateway, SkillInstallResponse{
				OK:     false,
				Output: clipOutput(out),
				Error:  err.Error(),
			})
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, SkillInstallResponse{
			OK:     true,
			Output: clipOutput(out),
		})
	}
}

func handleSkillDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.SkillsAdmin {
			writeError(w, deps.Logger, http.StatusForbidden, "skill delete disabled (set SKILLS_ADMIN=true)")
			return
		}
		if deps.SkillsDeleter == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "skill deleter unavailable")
			return
		}
		name := r.PathValue("name")
		if !skillNameRe.MatchString(name) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid skill name")
			return
		}
		if err := deps.SkillsDeleter.Delete(name); err != nil {
			if errors.Is(err, ErrSkillNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "skill not found")
				return
			}
			deps.Logger.Warn("api: skill delete failed", "name", name, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, fmt.Sprintf("delete failed: %v", err))
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, SkillDeleteResponse{OK: true, Name: name})
	}
}

// clipOutput trims overly long npx stdout/stderr so a verbose install
// log doesn't blow up the dashboard fetch buffer.
func clipOutput(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncated]"
}
