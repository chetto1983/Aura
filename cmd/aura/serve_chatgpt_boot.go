package main

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func chatGPTBootOwner(ctx context.Context, model string, q sqlc.DBTX) (string, error) {
	row, err := sqlc.New(q).GetSetting(ctx, "AURA_LLM_MODEL")
	if errors.Is(err, pgx.ErrNoRows) {
		return identityctx.OperatorIdentity(ctx, q)
	}
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(row.UpdatedBy.String); err != nil || !row.UpdatedBy.Valid || row.Value != model || row.IsSecret {
		return "", errors.New("save the ChatGPT model in Settings to resolve its account profile")
	}
	// The persisted profile's author identifies its catalogue, even on multi-user
	// appliances. Inference still resolves each request's own identity credential.
	return row.UpdatedBy.String, nil
}
