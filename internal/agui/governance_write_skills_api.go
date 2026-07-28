package agui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// governance_write_skills_api.go is the Phase-29 thin REST adapter over the
// SkillsWriteProvider seam (SKW-01/02/03). The routes under the /api/governance/skills/
// carve-out install a skill from a source field or a skills.sh catalog result, lifecycle
// it (restore/archive), create/update/delete it, and search the external catalog — each
// over the EXISTING Phase-11 Writer gate + the Phase-25 approval queue, never new gate or
// activation logic. There is NO business logic here: each handler nil-checks the skills
// write provider (503 when unwired), size-caps + decodes the body, reads the authenticated
// principal as the audit actor, makes ONE provider call, and projects to JSON — mirroring
// governance_write_api.go (the MCP write handlers).
//
// SKW-01/02 install posture (Claude-Code parity, operator directive 2026-06-21): the install
// fetches + validates + ACTIVATES directly — no approval pause, no "stage for approval"
// two-step. The five write-boundary validations + the loader-level injection blocklist still
// run on every body before activation (the intrinsic security keep), and the container is the
// blast boundary (post-D-09: no --ignore-scripts). The installed skill lands in the active
// loader immediately.
//
// SKW-03 restore-collision guard: the restore handler maps the provider's
// ErrSkillActiveExists sentinel to 409 — the provider stat'd active/{name} and returned the
// sentinel BEFORE Writer.Restore (which does an os.RemoveAll that would silently overwrite
// an active skill). The parent-mux mount behind RequireCapability(governance.write) is
// cmd/aura/serve_webui.go's job; every wire error passes through sanitizeErr (no leak).

// registerGovernanceSkillsWriteRoutes mounts the skills write routes on the supplied mux
// using Go 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/
// carve-out, never a bare /api/ (Pitfall 5). The {name}/restore and {name}/archive routes
// are more specific than the {name} create/update/delete routes so longest-pattern
// precedence keeps them distinct; the catalog GET is a fixed path.
func (s *Server) registerGovernanceSkillsWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/governance/skills/install", s.handleSkillInstall)
	mux.HandleFunc("POST /api/governance/skills/{name}/restore", s.handleSkillRestore)
	mux.HandleFunc("POST /api/governance/skills/{name}/archive", s.handleSkillArchive)
	mux.HandleFunc("POST /api/governance/skills/validate", s.handleSkillValidate)
	mux.HandleFunc("POST /api/governance/skills", s.handleSkillCreate)
	mux.HandleFunc("PATCH /api/governance/skills/{name}", s.handleSkillUpdate)
	mux.HandleFunc("DELETE /api/governance/skills/{name}", s.handleSkillDelete)
	mux.HandleFunc("GET /api/governance/skills/catalog", s.handleSkillCatalog)
}

// skillInstallBody is the install request: a source field (owner/repo, URL, or local path).
type skillInstallBody struct {
	Source string `json:"source"`
}

// skillMutateBody is the create/update request: the frontmatter name/description + body +
// the always flag. Delete carries only the {name} path value (no body).
type skillMutateBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Always      bool   `json:"always"`
}

// handleSkillInstall serves POST /api/governance/skills/install (SKW-01/02): fetch the skill
// through the Installer, run the five write-boundary validations, and ACTIVATE it directly
// (Claude-Code parity — no approval pause). The response is the SkillsInstallInfo
// (source/hash/preview/destination, the five checklist items, status "active"). The installed
// skill is in the active loader immediately.
func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginSkillsWrite(w, r)
	if !ok {
		return
	}
	var body skillInstallBody
	if !decodeSkillsBody(w, r, &body) {
		return
	}
	info, err := s.governanceWrite.Skills.Install(r.Context(), actor, strings.TrimSpace(body.Source))
	if err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	// SPEC Prohibition #1 (no raw secret anywhere): a source can carry an embedded credential
	// (`?token=...`, `user:pass@host`). Redact it before the echo crosses the wire — the
	// held-out secret-scan (governance_write_secret_scan_test.go) catches a regression.
	info.Source = sanitizeSkillSource(info.Source)
	writeJSON(w, info)
}

// handleSkillRestore serves POST /api/governance/skills/{name}/restore (SKW-03): the
// provider stats active/{name} and returns ErrSkillActiveExists (→ 409, Writer.Restore NOT
// called, the active skill untouched) on a name collision; otherwise it restores +
// re-materializes + audits. Returns 204 on success.
func (s *Server) handleSkillRestore(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginSkillsWrite(w, r)
	if !ok {
		return
	}
	if err := s.governanceWrite.Skills.Restore(r.Context(), actor, r.PathValue("name")); err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSkillArchive serves POST /api/governance/skills/{name}/archive (SKW-03): the
// provider de-materializes + moves active/{name} → archived + audits. Returns 204.
func (s *Server) handleSkillArchive(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginSkillsWrite(w, r)
	if !ok {
		return
	}
	if err := s.governanceWrite.Skills.Archive(r.Context(), actor, r.PathValue("name")); err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSkillCreate serves POST /api/governance/skills (SKW-01): the provider wraps
// Writer.WriteMutationByName("create", ...), which writes the skill live. The resulting
// status string is returned.
func (s *Server) handleSkillCreate(w http.ResponseWriter, r *http.Request) {
	s.handleSkillMutate(w, r, "create", "")
}

// handleSkillUpdate serves PATCH /api/governance/skills/{name} (SKW-01): the provider wraps
// Writer.WriteMutationByName("update", ...).
func (s *Server) handleSkillUpdate(w http.ResponseWriter, r *http.Request) {
	s.handleSkillMutate(w, r, "update", r.PathValue("name"))
}

// handleSkillDelete serves DELETE /api/governance/skills/{name} (SKW-01): the provider
// removes the skill from the active root and the export dir and writes one audit row.
// Returns 204 with an empty body, byte-for-byte the shape of handleSkillArchive.
//
// It used to route through Mutate("delete", …), which synthesised an empty frontmatter
// and then failed the description check — answering a sanitized 502 while deleting
// nothing and recording nothing. A 200 carrying {"status": …} was the shape that let
// that go unnoticed for as long as it did: the caller had something to read, so it
// looked like it had worked.
func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginSkillsWrite(w, r)
	if !ok {
		return
	}
	if err := s.governanceWrite.Skills.Delete(r.Context(), actor, r.PathValue("name")); err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSkillValidate serves POST /api/governance/skills/validate: it runs the write
// boundary against the submitted draft and WRITES NOTHING. The cockpit editor calls it
// while the operator types so the feedback comes from the code that will accept or reject
// the save, not from a second copy of the rules living in the browser.
//
// It always answers 200 with {ok, field?, message?}. A rejected draft is not an HTTP
// error — the draft is mid-edit by definition, and a 4xx would make "you are not finished
// typing" indistinguishable from "the request was malformed".
func (s *Server) handleSkillValidate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.beginSkillsWrite(w, r); !ok {
		return
	}
	var body skillMutateBody
	if !decodeSkillsBody(w, r, &body) {
		return
	}
	writeJSON(w, s.governanceWrite.Skills.Validate(body.Name, body.Description, body.Body, body.Always))
}

// handleSkillMutate is the shared create/update front door: decode the body, prefer the
// path {name} (update) over the body name (create), make ONE provider Mutate call.
func (s *Server) handleSkillMutate(w http.ResponseWriter, r *http.Request, action, pathName string) {
	actor, ok := s.beginSkillsWrite(w, r)
	if !ok {
		return
	}
	var body skillMutateBody
	if !decodeSkillsBody(w, r, &body) {
		return
	}
	name := pathName
	if name == "" {
		name = body.Name
	}
	status, err := s.governanceWrite.Skills.Mutate(r.Context(), actor, action, name, body.Description, body.Body, body.Always)
	if err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": status})
}

// handleSkillCatalog serves GET /api/governance/skills/catalog?q= (SKW-01): the provider
// runs the external catalog search ONLY when AURA_SKILLS_EXTERNAL_DISCOVERY is on; otherwise
// it returns a disabled result with the toggle state explicit (no silent discovery). The GET
// is privileged (it triggers an external network fetch when the flag is on), so it inherits
// the same governance.write gate as the mutations at the parent mux.
func (s *Server) handleSkillCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.beginSkillsRead(w, r) {
		return
	}
	res, err := s.governanceWrite.Skills.Search(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeSkillsWriteError(w, err)
		return
	}
	writeJSON(w, res)
}

// beginSkillsWrite is the shared front door for every skills write handler: nil-check the
// skills write provider (503 when unwired) and read the authenticated principal as the audit
// actor (401 if absent — though the parent-mux capability gate already bound it).
func (s *Server) beginSkillsWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.governanceWrite.Skills == nil {
		http.Error(w, "skills write not configured", http.StatusServiceUnavailable)
		return "", false
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return actor, true
}

// beginSkillsRead is the catalog GET front door: nil-check the provider (503). The catalog
// search needs no audit actor (it writes nothing), so it does not require a bound principal
// here — the parent-mux RequireCapability(governance.write) gate already authenticated +
// authorized the caller.
func (s *Server) beginSkillsRead(w http.ResponseWriter, r *http.Request) bool {
	if s.governanceWrite.Skills == nil {
		http.Error(w, "skills write not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// decodeSkillsBody size-caps + decodes a JSON body into dst, writing a clean 400 on a
// malformed body. An empty body is allowed (delete uses no body).
func decodeSkillsBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// writeSkillsWriteError maps a provider skills-write error onto the right HTTP status with a
// sanitized body: a restore collision → 409 (the restore-collision landmine guard), an
// invalid input (empty source) → 400, anything else → a sanitized 502 (a backend/FS/DB
// failure is not the client's fault). The typed sentinels are declared in
// governance_write_seam.go; the concrete provider returns them.
func writeSkillsWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSkillActiveExists):
		http.Error(w, "active skill already exists", http.StatusConflict)
	case errors.Is(err, ErrSkillInvalidInput):
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": sanitizeErr(err)})
	default:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
	}
}
