package agent

import (
	"context"

	"github.com/google/uuid"
)

type modelRound struct {
	requestID uuid.UUID
	ordinal   uint32
}

type modelRoundOrdinal uint32

func (ordinal *modelRoundOrdinal) next(requestID uuid.UUID) modelRound {
	(*ordinal)++
	return modelRound{requestID: requestID, ordinal: uint32(*ordinal)}
}

type modelRoundContextKey struct{}

func withModelRound(ctx context.Context, round modelRound) context.Context {
	return context.WithValue(ctx, modelRoundContextKey{}, round)
}

func modelRoundFromContext(ctx context.Context) (modelRound, bool) {
	round, ok := ctx.Value(modelRoundContextKey{}).(modelRound)
	return round, ok
}
