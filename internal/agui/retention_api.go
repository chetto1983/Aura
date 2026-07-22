package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

const ownerExportPolicyVersion = "retention-v1"

type serverOwnerExportSource struct {
	server *Server
}

type ownerConversationVersionSource interface {
	VersionForIdentity(context.Context, string, string) (int64, error)
}

type ownerConversationData struct {
	SchemaVersion int                        `json:"schema_version"`
	Conversation  conversations.Conversation `json:"conversation"`
	Turns         []llm.Message              `json:"turns"`
}

func (s serverOwnerExportSource) Snapshot(ctx context.Context, ownerID, conversationID string) (ExportSnapshot, error) {
	if s.server == nil || s.server.conv == nil || s.server.assets == nil {
		return ExportSnapshot{}, errors.New("owner export source unavailable")
	}
	var release func()
	if locker, ok := s.server.run.(threadTryLocker); ok {
		var locked bool
		release, locked = locker.TryLockThread(ctx, conversationID)
		if !locked {
			return ExportSnapshot{}, errors.New("conversation is active")
		}
	}
	releaseOnError := func() {
		if release != nil {
			release()
		}
	}
	conversation, err := s.server.conv.GetForIdentity(ctx, conversationID, ownerID)
	if err != nil {
		releaseOnError()
		return ExportSnapshot{}, ErrOwnerExportNotFound
	}
	conversationVersion := conversation.SnapshotVersion
	history, err := s.server.conv.LoadHistory(ctx, conversationID)
	if err != nil {
		releaseOnError()
		return ExportSnapshot{}, fmt.Errorf("load owner export history: %w", err)
	}
	assetRows, err := s.server.assets.ListForThread(ctx, ownerID, conversationID)
	if err != nil {
		releaseOnError()
		return ExportSnapshot{}, fmt.Errorf("list owner export artifacts: %w", err)
	}
	if versions, ok := s.server.conv.(ownerConversationVersionSource); ok {
		currentVersion, versionErr := versions.VersionForIdentity(ctx, conversationID, ownerID)
		if versionErr != nil {
			releaseOnError()
			return ExportSnapshot{}, fmt.Errorf("load owner export version: %w", versionErr)
		}
		if conversationVersion <= 0 || currentVersion != conversationVersion {
			releaseOnError()
			return ExportSnapshot{}, ErrOwnerExportConflict
		}
	}
	data, err := json.Marshal(ownerConversationData{SchemaVersion: 1, Conversation: conversation, Turns: history})
	if err != nil {
		releaseOnError()
		return ExportSnapshot{}, fmt.Errorf("marshal owner export snapshot: %w", err)
	}
	artifacts := make([]ExportArtifact, 0, len(assetRows))
	for _, asset := range assetRows {
		if asset.Status == assets.StatusDeleted || asset.Status == assets.StatusCanceled {
			continue
		}
		artifacts = append(artifacts, ExportArtifact{
			ID: asset.ID, Filename: asset.FileName, MIMEType: asset.MIMEType, Size: asset.SizeBytes,
		})
	}
	return ExportSnapshot{
		IdentitySnapshotID: uuid.NewString(), ConversationSnapshotID: uuid.NewString(),
		ConversationJSON: data, Artifacts: artifacts, Omissions: []string{},
		ConversationVersion: conversationVersion, Release: release,
	}, nil
}

func (s serverOwnerExportSource) OpenArtifact(ctx context.Context, ownerID, assetID string) (io.ReadCloser, error) {
	body, _, err := s.server.assets.OpenForIdentity(ctx, assetID, ownerID)
	if err != nil {
		return nil, ErrOwnerExportNotFound
	}
	return body, nil
}

func (s *Server) handleOwnerExport(w http.ResponseWriter, r *http.Request) {
	s.handleOwnerExportMode(w, r, false)
}

func (s *Server) handleOwnerExportDelete(w http.ResponseWriter, r *http.Request) {
	s.handleOwnerExportMode(w, r, true)
}

func (s *Server) handleOwnerExportMode(w http.ResponseWriter, r *http.Request, deleteAfter bool) {
	if s.run == nil || s.conv == nil || s.assets == nil {
		http.Error(w, "owner export unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID, ok := parseConvID(w, r)
	if !ok {
		return
	}
	var destination ExportDestination = NewMemoryExportDestination(defaultOwnerExportLimit)
	if deleteAfter {
		destination = s.ownerExports
		if destination == nil {
			http.Error(w, "durable owner export unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	exporter := &OwnerExporter{
		Source: serverOwnerExportSource{server: s}, Destination: destination,
		Deleter: s.run, AuraVersion: "unknown", PolicyVersion: ownerExportPolicyVersion,
	}
	var result ExportResult
	var err error
	if deleteAfter {
		result, err = exporter.ExportDelete(r.Context(), ownerID, conversationID)
	} else {
		result, err = exporter.Export(r.Context(), ownerID, conversationID)
	}
	if err != nil {
		if errors.Is(err, ErrOwnerExportConflict) {
			http.Error(w, "conversation changed during export", http.StatusConflict)
			return
		}
		if errors.Is(err, ErrOwnerExportNotFound) {
			http.Error(w, "conversation not found", http.StatusNotFound)
			return
		}
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	if deleteAfter {
		downloadURL := "/api/owner-exports/" + url.PathEscape(result.ExportID) + "?expires=" + strconv.FormatInt(result.ExpiresAt.Unix(), 10)
		writeJSONStatus(w, http.StatusCreated, map[string]any{
			"export_id": result.ExportID, "download_url": downloadURL,
			"expires_at": result.ExpiresAt.Format(time.RFC3339), "size": result.Size, "sha256": result.SHA256,
		})
		return
	}
	body, err := destination.Open(r.Context(), ownerID, result.ExportID, result.ExpiresAt)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	defer func() { _ = body.Close() }()
	header := w.Header()
	header.Set("Content-Type", "application/zip")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Disposition", contentDisposition("aura-conversation-export.zip"))
	header.Set("Content-Length", strconv.FormatInt(result.Size, 10))
	header.Set("X-Aura-SHA256", result.SHA256)
	if _, err := io.Copy(w, body); err != nil {
		return
	}
}

func (s *Server) handleOwnerExportDownload(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.ownerExports == nil {
		http.Error(w, "durable owner export unavailable", http.StatusServiceUnavailable)
		return
	}
	exportID := r.PathValue("id")
	if _, err := uuid.Parse(exportID); err != nil {
		http.Error(w, "owner export not found", http.StatusNotFound)
		return
	}
	expiresUnix, err := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if err != nil {
		http.Error(w, "owner export not found", http.StatusNotFound)
		return
	}
	expiresAt := time.Unix(expiresUnix, 0).UTC()
	if !expiresAt.After(time.Now().UTC()) || expiresAt.After(time.Now().UTC().Add(defaultOwnerExportRetention+time.Minute)) {
		http.Error(w, "owner export not found", http.StatusNotFound)
		return
	}
	body, err := s.ownerExports.Open(r.Context(), ownerID, exportID, expiresAt)
	if err != nil {
		http.Error(w, "owner export not found", http.StatusNotFound)
		return
	}
	defer func() { _ = body.Close() }()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", contentDisposition("aura-conversation-export.zip"))
	_, _ = io.Copy(w, body)
}
