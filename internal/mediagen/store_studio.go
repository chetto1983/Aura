package mediagen

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/pgnumeric"
)

// StudioPageMax bounds one Studio history page.
const StudioPageMax = 48

// ListStudio returns one page of the owner's Studio rows, newest first, of kind when it is
// not empty. beforeID is the last row of the previous page, empty for the first; a
// malformed one names no row and yields an empty page. limit is clamped to [1, StudioPageMax].
func (s *Store) ListStudio(ctx context.Context, ownerID, beforeID string, kind Kind, limit int) ([]Job, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return nil, err
	}
	var before pgtype.UUID
	if beforeID != "" {
		if before, err = db.ParseUUID("before id", beforeID); err != nil {
			return []Job{}, nil
		}
	}
	limit = min(max(limit, 1), StudioPageMax)
	filter := pgtype.Text{}
	if kind != "" {
		filter = pgtype.Text{String: string(kind), Valid: true}
	}
	return s.withJobs(ctx, ownerID, func(q *sqlc.Queries) ([]sqlc.AuraMediaJob, error) {
		return q.ListStudioMediaJobs(ctx, sqlc.ListStudioMediaJobsParams{
			IdentityID: owner, Kind: filter, BeforeID: before,
			RowLimit: int32(limit), //nolint:gosec // clamped to StudioPageMax above.
		})
	})
}

// InsertImage records one synchronous image generation, already completed and delivered: an
// image is paid for and produced in a single call, so there is nothing to supervise. The
// asset must be the owner's accepted agent image with no thread, the Studio's own result; any
// other asset matches nothing and the row is refused rather than written unbacked.
func (s *Store) InsertImage(ctx context.Context, job Job) (Job, error) {
	if err := validateImageRecord(job); err != nil {
		return Job{}, err
	}
	owner, err := db.ParseUUID("owner id", job.IdentityID)
	if err != nil {
		return Job{}, err
	}
	asset, err := db.ParseUUID("asset id", job.AssetID)
	if err != nil {
		return Job{}, err
	}
	cost, err := pgnumeric.NullableFromFloat(job.CostUSD)
	if err != nil {
		return Job{}, err
	}
	row, err := s.withJob(ctx, job.IdentityID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		out, err := q.InsertCompletedMediaJob(ctx, sqlc.InsertCompletedMediaJobParams{
			IdentityID: owner, ProviderJobID: job.ProviderJobID, Model: job.Model,
			Request: job.Request, CostUsd: cost, AssetID: asset,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return out, &Error{Code: "asset_not_found", Message: "The generated image is not available."}
		}
		return out, err
	})
	if err != nil {
		return Job{}, err
	}
	return row, nil
}

func validateImageRecord(job Job) error {
	switch {
	case job.Kind != KindImage:
		return fmt.Errorf("mediagen: InsertImage records an image, not %q", job.Kind)
	case job.Surface != SurfaceStudio:
		return errors.New("mediagen: only the Studio records a finished generation")
	case job.Model == "" || job.AssetID == "":
		return errors.New("mediagen: an image record needs its model and asset")
	}
	_, err := validProviderID(job.ProviderJobID)
	return err
}
