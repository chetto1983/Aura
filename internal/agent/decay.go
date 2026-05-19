package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

const (
	DefaultOperationalDecayUnrecalledTTL = 30 * 24 * time.Hour
	DefaultOperationalDecayUpdatedGrace  = 7 * 24 * time.Hour
)

type DecaySummary struct {
	Scanned int
	Deleted int
	Kept    int
}

// DecayCycle removes stale Normal-priority operational lessons. Critical and
// High priorities are exempt in the storage predicate.
func DecayCycle(ctx context.Context, store *memoryindex.Store, now time.Time, logger *slog.Logger) (DecaySummary, error) {
	if store == nil {
		return DecaySummary{}, fmt.Errorf("decay: memory store unavailable")
	}
	summary, err := store.DecayOperationalLessons(ctx, now, DefaultOperationalDecayUnrecalledTTL, DefaultOperationalDecayUpdatedGrace)
	out := DecaySummary{Scanned: summary.Scanned, Deleted: summary.Deleted, Kept: summary.Kept}
	if err != nil {
		return out, fmt.Errorf("decay: operational lessons: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("operational lesson decay complete", "scanned", out.Scanned, "deleted", out.Deleted, "kept", out.Kept)
	return out, nil
}
