package agui

import (
	"context"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/plugins"
)

// PluginsProvider is the plugin board's backend (spec 2026-10-25-pack-install §4.5): one
// implementation, internal/plugins.Backend, behind the cockpit and the chat alike.
type PluginsProvider interface {
	List() ([]plugins.Record, error)
	Review(ctx context.Context, ref string) (plugins.Review, error)
	Install(ctx context.Context, actor string, req plugins.InstallRequest) (plugins.Report, error)
	Upgrade(ctx context.Context, actor, name string) (plugins.UpgradeReport, error)
	Remove(ctx context.Context, actor, name string, deleteData bool) (plugins.RemoveReport, error)
}

// registerGovernancePluginRoutes mounts the plugin board. cmd/aura/serve_webui.go puts the
// list behind governance.read and the four writes behind identity.create: a plugin's
// servers run in every identity's box, so installing one administers the deployment.
func (s *Server) registerGovernancePluginRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/governance/plugins", s.handlePluginList)
	mux.HandleFunc("POST /api/governance/plugins/review", s.handlePluginReview)
	mux.HandleFunc("POST /api/governance/plugins", s.handlePluginInstall)
	mux.HandleFunc("POST /api/governance/plugins/{name}/upgrade", s.handlePluginUpgrade)
	mux.HandleFunc("DELETE /api/governance/plugins/{name}", s.handlePluginRemove)
}

func (s *Server) handlePluginList(w http.ResponseWriter, _ *http.Request) {
	if s.governanceWrite.Plugins == nil {
		http.Error(w, "plugins not configured", http.StatusServiceUnavailable)
		return
	}
	records, err := s.governanceWrite.Plugins.List()
	if err != nil {
		writePluginError(w, err)
		return
	}
	if records == nil {
		records = []plugins.Record{}
	}
	writeJSON(w, map[string]any{"plugins": records})
}

// handlePluginReview serves POST /api/governance/plugins/review {ref}: what an install of the
// plugin at its default branch would write, bound by a digest. It writes nothing.
func (s *Server) handlePluginReview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.beginPluginWrite(w, r); !ok {
		return
	}
	var body struct {
		Ref string `json:"ref"`
	}
	if !decodeMCPBody(w, r, &body) {
		return
	}
	review, err := s.governanceWrite.Plugins.Review(r.Context(), body.Ref)
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, review)
}

// handlePluginInstall serves POST /api/governance/plugins {ref, commit, digest}: the operator's
// one confirmation of a review.
func (s *Server) handlePluginInstall(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginPluginWrite(w, r)
	if !ok {
		return
	}
	var body plugins.InstallRequest
	if !decodeMCPBody(w, r, &body) {
		return
	}
	report, err := s.governanceWrite.Plugins.Install(r.Context(), actor, body)
	writePluginOutcome(w, report, err)
}

// handlePluginUpgrade serves POST /api/governance/plugins/{name}/upgrade: the plugin moves to
// the head of its repository's default branch, or answers that it is up to date there. Like
// Codex's upgrade it has no review step.
func (s *Server) handlePluginUpgrade(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginPluginWrite(w, r)
	if !ok {
		return
	}
	report, err := s.governanceWrite.Plugins.Upgrade(r.Context(), actor, r.PathValue("name"))
	writePluginOutcome(w, report, err)
}

// handlePluginRemove serves DELETE /api/governance/plugins/{name}; ?data=delete also deletes
// the plugin's data in the caller's box.
func (s *Server) handlePluginRemove(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginPluginWrite(w, r)
	if !ok {
		return
	}
	report, err := s.governanceWrite.Plugins.Remove(r.Context(), actor, r.PathValue("name"), r.URL.Query().Get("data") == "delete")
	writePluginOutcome(w, report, err)
}

// beginPluginWrite answers 503 while the backend is unwired and 401 without a principal, and
// returns the principal as the actor.
func (s *Server) beginPluginWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.governanceWrite.Plugins == nil {
		http.Error(w, "plugins not configured", http.StatusServiceUnavailable)
		return "", false
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return actor, true
}

// writePluginOutcome answers a write with its report, or with its refusal. A write that an error
// stopped after it began answers 207 with the report beside the error: something landed or left,
// and the operator needs to know what.
func writePluginOutcome[R interface{ Started() bool }](w http.ResponseWriter, report R, err error) {
	switch {
	case err == nil:
		writeJSON(w, report)
	case report.Started():
		writeJSONStatus(w, http.StatusMultiStatus, map[string]any{"error": sanitizeErr(err), "report": report})
	default:
		writePluginError(w, err)
	}
}

// writePluginError maps the backend's refusals: a request the caller can correct is 400, a
// review that no longer holds or a name already installed is 409, an unknown plugin is 404,
// and anything else is a sanitized 502.
func writePluginError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, plugins.ErrInvalidRequest):
		status = http.StatusBadRequest
	case errors.Is(err, plugins.ErrDigestChanged), errors.Is(err, plugins.ErrAlreadyInstalled):
		status = http.StatusConflict
	case errors.Is(err, plugins.ErrNotInstalled):
		status = http.StatusNotFound
	}
	writeJSONStatus(w, status, map[string]string{"error": sanitizeErr(err)})
}
