package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

type tenantConversationProjectionSink struct {
	clients *arcadedb.TenantClients
}

func (s tenantConversationProjectionSink) client(
	ctx context.Context,
	identityID string,
) (*arcadedb.Client, error) {
	return s.clients.For(ctx, identityID)
}

func (s tenantConversationProjectionSink) ApplyConversationProjection(
	ctx context.Context,
	projection arcadedb.ConversationProjection,
) error {
	client, err := s.client(ctx, projection.IdentityID)
	if err != nil {
		return err
	}
	return client.ApplyConversationProjection(ctx, projection)
}

func (s tenantConversationProjectionSink) DeleteConversationProjection(
	ctx context.Context,
	identityID, conversationID string,
) error {
	client, err := s.client(ctx, identityID)
	if err != nil {
		return err
	}
	return client.DeleteConversationProjection(ctx, identityID, conversationID)
}

func (s tenantConversationProjectionSink) DeleteIdentityConversationProjections(
	ctx context.Context,
	identityID string,
) error {
	client, err := s.client(ctx, identityID)
	if err != nil {
		return err
	}
	return client.DeleteIdentityConversationProjections(ctx, identityID)
}

func (s tenantConversationProjectionSink) PruneConversationProjections(
	ctx context.Context,
	identityID string,
	liveConversationIDs []string,
) error {
	client, err := s.client(ctx, identityID)
	if err != nil {
		return err
	}
	return client.PruneConversationProjections(ctx, identityID, liveConversationIDs)
}

func newChatConversationProjector(
	cfg *config.Config,
	source runner.ConversationProjectionSource,
) *runner.ConversationProjector {
	if cfg == nil || source == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return nil
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("conversation projection disabled: tenant credentials unavailable", "error", err)
		return nil
	}
	var admin *arcadedb.Client
	if strings.TrimSpace(cfg.ArcadeDB.AdminUser) != "" {
		admin, err = arcadedb.New(arcadedb.Config{
			BaseURL: cfg.ArcadeDB.BaseURL, Database: cfg.ArcadeDB.Database,
			User: cfg.ArcadeDB.AdminUser, Password: cfg.ArcadeDB.AdminPassword,
		})
		if err != nil {
			slog.Warn("conversation projection disabled: ArcadeDB admin configuration invalid", "error", err)
			return nil
		}
	}
	embedURL, apiKey, model := cfg.EmbedRoute()
	clients := arcadedb.NewTenantClients(
		arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}, admin,
		arcadedb.NewSidecarEmbedder(embedURL, model, apiKey, 0), credentials,
	)
	return runner.NewConversationProjector(source, tenantConversationProjectionSink{clients: clients}, 0)
}

func wireChatConversationReconciliation(
	reconciler *runner.DeleteReconciler,
	projector *runner.ConversationProjector,
	identities runner.ConversationProjectionIdentities,
) {
	if reconciler != nil {
		reconciler.SetConversationProjection(projector, identities)
	}
}
