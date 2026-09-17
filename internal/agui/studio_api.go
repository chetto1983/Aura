package agui

// studio_api.go serves the cockpit Studio: an identity generates images and videos from one
// composer bar, outside any conversation, and reads its own history and library. Every route
// is scoped to the authenticated principal — the owner is never taken from the body — and
// every generation goes through the same shared paths the agent's tools use.

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

// studioHistoryDefault is one screen of history; the ceiling is the store's own page cap.
const studioHistoryDefault = 24

// StudioBackend is the Studio's live half: the catalog it lists, the two shared generation
// paths it pays through, the job store it reads back, and the identity's image library. The
// composition root implements it over the daemon's media dependencies (cmd/aura), so this
// package needs neither a provider client nor a database.
type StudioBackend interface {
	Models(ctx context.Context, kind mediagen.Kind) (defaultModel string, models []mediagen.Model, err error)
	SubmitVideo(ctx context.Context, owner string, req StudioVideoRequest) (mediagen.Job, error)
	GenerateImage(ctx context.Context, owner string, req StudioImageRequest) (mediagen.Job, error)
	History(ctx context.Context, owner, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error)
	Library(ctx context.Context, owner string, limit int) ([]assets.Asset, error)
	FinalizeUpload(ctx context.Context, owner, assetID string) (assets.Asset, error)
}

func (s *Server) registerStudioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/studio/models", s.handleStudioModels)
	mux.HandleFunc("POST /api/studio/videos", s.handleStudioVideoCreate)
	mux.HandleFunc("POST /api/studio/images", s.handleStudioImageCreate)
	mux.HandleFunc("GET /api/studio/history", s.handleStudioHistory)
	mux.HandleFunc("GET /api/studio/library", s.handleStudioLibrary)
	mux.HandleFunc("POST /api/studio/uploads/{id}/finalize", s.handleStudioUploadFinalize)
}

// studioCaller is the gate every Studio route shares: an unwired Studio is unavailable, and
// an unauthenticated caller has no identity to own a generation.
func (s *Server) studioCaller(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.studio == nil {
		http.Error(w, "studio unavailable", http.StatusServiceUnavailable)
		return "", false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return identityID, true
}

// studioKind reads a kind from a query value. Only the two kinds the catalog knows are
// accepted, so a stale or forged form naming a third is refused rather than passed on.
func studioKind(raw string) (mediagen.Kind, bool) {
	switch kind := mediagen.Kind(raw); kind {
	case mediagen.KindImage, mediagen.KindVideo:
		return kind, true
	}
	return "", false
}

// studioLimit reads a page size: absent means the default, anything the caller names is
// clamped to [1, ceiling], and a value that is not a number is a refusal rather than a
// silent fallback — a cockpit sending one has a bug the operator should see.
func studioLimit(raw string, fallback, ceiling int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return min(max(limit, 1), ceiling), true
}

func (s *Server) handleStudioModels(w http.ResponseWriter, r *http.Request) {
	_, ok := s.studioCaller(w, r)
	if !ok {
		return
	}
	kind, ok := studioKind(r.URL.Query().Get("kind"))
	if !ok {
		http.Error(w, "kind must be image or video", http.StatusBadRequest)
		return
	}
	defaultModel, models, err := s.studio.Models(r.Context(), kind)
	if err != nil {
		writeStudioError(w, err)
		return
	}
	out := studioModelsDTO{Default: defaultModel, Models: make([]studioModelDTO, 0, len(models))}
	for _, model := range models {
		out.Models = append(out.Models, studioModel(model, kind))
	}
	writeJSON(w, out)
}

// studioCreate is what both create routes are: the principal owns the generation, the body is
// decoded strictly so an unknown field is a refusal rather than a silently dropped intent, and
// the accepted record is the answer. create is a closure, not a method value, because the
// backend is only known to be present once studioCaller has passed.
func studioCreate[T any](s *Server, w http.ResponseWriter, r *http.Request,
	create func(context.Context, string, T) (mediagen.Job, error),
) {
	identityID, ok := s.studioCaller(w, r)
	if !ok {
		return
	}
	var body T
	if err := strictDecodeJSON(w, r, &body, decodeOpts{}); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	job, err := create(r.Context(), identityID, body)
	if err != nil {
		writeStudioError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, studioRecord(job))
}

func (s *Server) handleStudioVideoCreate(w http.ResponseWriter, r *http.Request) {
	studioCreate(s, w, r, func(ctx context.Context, owner string, body StudioVideoRequest) (mediagen.Job, error) {
		return s.studio.SubmitVideo(ctx, owner, body)
	})
}

func (s *Server) handleStudioImageCreate(w http.ResponseWriter, r *http.Request) {
	studioCreate(s, w, r, func(ctx context.Context, owner string, body StudioImageRequest) (mediagen.Job, error) {
		return s.studio.GenerateImage(ctx, owner, body)
	})
}

func (s *Server) handleStudioHistory(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.studioCaller(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	var kind mediagen.Kind
	if raw := query.Get("kind"); raw != "" {
		if kind, ok = studioKind(raw); !ok {
			http.Error(w, "kind must be image or video", http.StatusBadRequest)
			return
		}
	}
	limit, ok := studioLimit(query.Get("limit"), studioHistoryDefault, mediagen.StudioPageMax)
	if !ok {
		http.Error(w, "limit must be a number", http.StatusBadRequest)
		return
	}
	jobs, err := s.studio.History(r.Context(), identityID, query.Get("before"), kind, limit)
	if err != nil {
		writeStudioError(w, err)
		return
	}
	out := studioHistoryDTO{Records: make([]studioRecordDTO, 0, len(jobs))}
	for _, job := range jobs {
		out.Records = append(out.Records, studioRecord(job))
	}
	writeJSON(w, out)
}

func (s *Server) handleStudioLibrary(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.studioCaller(w, r)
	if !ok {
		return
	}
	limit, ok := studioLimit(r.URL.Query().Get("limit"), studioHistoryDefault, mediagen.StudioPageMax)
	if !ok {
		http.Error(w, "limit must be a number", http.StatusBadRequest)
		return
	}
	library, err := s.studio.Library(r.Context(), identityID, limit)
	if err != nil {
		writeStudioError(w, err)
		return
	}
	out := studioLibraryDTO{Assets: make([]studioAssetDTO, 0, len(library))}
	for _, asset := range library {
		out.Assets = append(out.Assets, studioAsset(asset))
	}
	writeJSON(w, out)
}

func (s *Server) handleStudioUploadFinalize(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.studioCaller(w, r)
	if !ok {
		return
	}
	asset, err := s.studio.FinalizeUpload(r.Context(), identityID, r.PathValue("id"))
	if err != nil {
		writeStudioError(w, err)
		return
	}
	writeJSON(w, studioAsset(asset))
}
