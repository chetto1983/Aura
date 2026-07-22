package db

import (
	"context"

	"github.com/chetto1983/aura/internal/obs"
	"github.com/jackc/pgx/v5"
)

const dbMeterScope = "github.com/chetto1983/aura/internal/db"

var dbTransactionBoundary = obs.NewGlobalBoundary(dbMeterScope, obs.BoundaryConfig{
	Operation: "db_transaction", Count: obs.DBCallsID, Duration: obs.DBDurationID,
})

var dbQueryBoundary = obs.NewGlobalBoundary(dbMeterScope, obs.BoundaryConfig{
	Operation: "db_query", Count: obs.DBCallsID, Duration: obs.DBDurationID,
})

type queryBoundaryContextKey struct{}

type queryBoundaryTracer struct{}

func (queryBoundaryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ctx, end := dbQueryBoundary.Start(ctx)
	return context.WithValue(ctx, queryBoundaryContextKey{}, end)
}

func (queryBoundaryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if end, ok := ctx.Value(queryBoundaryContextKey{}).(*obs.BoundaryEnd); ok {
		end.End(data.Err)
	}
}
