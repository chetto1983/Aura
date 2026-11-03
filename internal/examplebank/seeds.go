package examplebank

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// seedTimeout bounds one background build of a domain's seeds. A cold start on a small CPU embeds
// two hundred short texts, which the tick's own budget could not hold.
const seedTimeout = 2 * time.Minute

var errSeedsInAnotherSpace = errors.New("examplebank: the seeds came back in another space")

// seedRows returns d's seed rows for space, or starts their build and reports them not ready. One
// build runs per domain and space; a failed one caches nothing, so the next call starts another.
// The caller holds b.mu.
func (b *Bank) seedRows(ctx context.Context, d Domain, space string) ([][]float64, bool) {
	if len(d.Seeds) == 0 {
		return nil, true
	}
	key := seedKey{d.Name, space}
	if rows, ok := b.seeds[key]; ok {
		return rows, true
	}
	if !b.warming[key] {
		b.warming[key] = true
		go b.warm(context.WithoutCancel(ctx), d, space)
	}
	return nil, false
}

// warm embeds d's seeds in one call and keeps them only if they came back in space.
func (b *Bank) warm(ctx context.Context, d Domain, space string) {
	ctx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()
	started := time.Now()
	texts := make([]string, len(d.Seeds))
	for i, seed := range d.Seeds {
		texts[i] = seed.Text
	}
	vectors, got, err := b.Embed(ctx, texts)

	b.mu.Lock()
	defer b.mu.Unlock()
	key := seedKey{d.Name, space}
	delete(b.warming, key)
	if err == nil && got != space {
		err = errSeedsInAnotherSpace
	}
	if err != nil {
		slog.Warn("example bank seeds not embedded", "domain", d.Name, "space", space, "error", err)
		return
	}
	rows := make([][]float64, len(vectors))
	for i, vector := range vectors {
		rows[i] = Row(vector, d.Seeds[i].Extra)
	}
	b.seeds[key] = rows
	slog.Info("example bank seeds embedded", "domain", d.Name, "seeds", len(rows), "space", space, "took", time.Since(started))
}
