package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

const ownerExportPolicyVersion = "retention-v1"

type serverOwnerExportSource struct {
	server *Server
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
		ConversationJSON: data, Artifacts: artifacts, Omissions: []string{}, Release: release,
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
	destination := NewMemoryExportDestination(defaultOwnerExportLimit)
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
		if errors.Is(err, ErrOwnerExportNotFound) {
			http.Error(w, "conversation not found", http.StatusNotFound)
			return
		}
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	body, err := destination.Open(r.Context(), result.ExportID)
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
