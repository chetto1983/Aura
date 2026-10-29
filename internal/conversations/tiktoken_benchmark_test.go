package conversations

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The token estimate's cost, cold (nothing counted yet) and warm (the turn after, when only
// the newest turns are new). Measured 2026-10-27 on 4 cores before the bound and the cache:
// 64 KiB of base64 over zeroed bytes took 3.46 s; the ladder over 800 turns took 224 ms at a
// 1M window and 37.5 s with 12.4 GB allocated at 128k, where it drops rounds. Run via:
//
//	go test -run XXX -bench 'BenchmarkTokenEstimate' ./internal/conversations
const benchmarkProse = "Il cockpit importa ogni ramo della conversazione, so the picker moves " +
	"between them without asking the server again; un invio continua il ramo a schermo. "

func benchmarkTurns(turns int) []Turn {
	out := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: strings.Repeat(benchmarkProse, 40)}}
	for seq := 2; seq <= turns; seq++ {
		role, repeat := llm.RoleUser, 2
		if seq%2 == 1 {
			role, repeat = llm.RoleAssistant, 9
		}
		content := fmt.Sprintf("turn %d: %s", seq, strings.Repeat(benchmarkProse, repeat))
		out = append(out, Turn{Seq: seq, ParentSeq: seq - 1, Role: role, Content: content})
	}
	return out
}

func BenchmarkTokenEstimate(b *testing.B) {
	enc, err := encoder()
	if err != nil {
		b.Fatal(err)
	}
	zeros := base64.StdEncoding.EncodeToString(make([]byte, 48<<10))
	b.Run("count/base64_zeros_64KiB", func(b *testing.B) {
		b.SetBytes(int64(len(zeros)))
		for range b.N {
			tokenCounts = newTokenCountCache(1 << 15)
			_ = countTokens(enc, zeros)
		}
	})
	history := benchmarkTurns(800)
	for _, window := range []int{1_000_000, 128_000} {
		cfg := ContextConfig{ContextWindow: window, MaxOutputTokens: 32768, ToolEvictAfterTurns: 6}
		for _, cold := range []bool{true, false} {
			b.Run(fmt.Sprintf("ladder/turns_800/window_%d/cold_%v", window, cold), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					if cold {
						tokenCounts = newTokenCountCache(1 << 15)
					}
					if _, err := applyContextLadder(context.Background(), "c", history, cfg, enc, benchmarkRotSink{}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

type benchmarkRotSink struct{}

func (benchmarkRotSink) insertContextRotEvent(context.Context, string, string, int, int, int) error {
	return nil
}
