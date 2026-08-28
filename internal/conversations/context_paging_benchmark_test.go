//go:build db_integration

package conversations

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const benchmarkSidecarBytes = 4096

func BenchmarkManagedHistoryWork(b *testing.B) {
	pool := migratedPool(b)
	for _, turns := range []int{50, 250, 1000, 5000} {
		for _, mode := range []string{"inline", "sidecar", "compacted_sidecar"} {
			if mode == "compacted_sidecar" && turns <= defaultHistoryPageTurns {
				continue
			}
			sidecars := mode != "inline"
			name := fmt.Sprintf("turns_%d/%s", turns, mode)
			b.Run(name, func(b *testing.B) {
				store := newStore(b, pool)
				conversationID := seedManagedHistoryBenchmark(b, store, turns, sidecars)
				want := turns
				if mode == "compacted_sidecar" {
					watermark := turns - defaultHistoryPageTurns
					if err := store.SaveCompaction(ownerCtx(), conversationID, "", Compaction{
						Summary:          "benchmark compacted prefix",
						CoversThroughSeq: watermark,
						SourceTurns:      watermark - 1,
					}); err != nil {
						b.Fatalf("save benchmark compaction: %v", err)
					}
					want = defaultHistoryPageTurns + 2
				}
				enc, err := encoder()
				if err != nil {
					b.Fatalf("encoder: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					loaded, loadErr := store.loadManagedLinearTurns(
						ownerCtx(), conversationID, "", defaultHistoryPageTurns, 0)
					if loadErr != nil {
						b.Fatalf("load %d turns: %v", turns, loadErr)
					}
					if len(loaded.turns) != want {
						b.Fatalf("loaded %d turns, want %d", len(loaded.turns), want)
					}
					_ = totalTokens(enc, loaded.turns)
				}
			})
		}
	}
}

func seedManagedHistoryBenchmark(b *testing.B, store *Store, turns int, sidecars bool) string {
	b.Helper()
	conversationID := newConversation(b, store)
	if _, err := store.pool.Exec(ownerCtx(), `
		INSERT INTO aura.conversation_turns (
			conversation_id, seq, role, content, content_sidecar_path, parent_seq
		)
		SELECT $1, n,
			CASE WHEN n = 1 THEN 'system'
			     WHEN n % 2 = 0 THEN 'user'
			     ELSE 'assistant' END,
			CASE WHEN $3::boolean AND n > 1 THEN '' ELSE repeat('x', 128) END,
			CASE WHEN $3::boolean AND n > 1 THEN 'managed-benchmark' ELSE NULL END,
			NULLIF(n - 1, 0)
		FROM generate_series(1, $2::int) AS n`, conversationID, turns, sidecars); err != nil {
		b.Fatalf("seed %d turns: %v", turns, err)
	}
	if !sidecars {
		return conversationID
	}
	dir := filepath.Join(store.runDir, "conversations", conversationID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.Fatalf("create sidecar dir: %v", err)
	}
	payload := make([]byte, benchmarkSidecarBytes)
	for i := range payload {
		payload[i] = 's'
	}
	for seq := 2; seq <= turns; seq++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.content", seq)), payload, 0o600); err != nil {
			b.Fatalf("write sidecar %d: %v", seq, err)
		}
	}
	return conversationID
}
