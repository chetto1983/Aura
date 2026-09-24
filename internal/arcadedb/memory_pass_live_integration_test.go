//go:build arcadedb_integration

// The pass against a live ArcadeDB: after a route change every memory row is outside the
// space, memory is lexical, and one pass brings it back dense.
//
// Run: arcade-it.sh MemoryPassLive
package arcadedb

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sameDatabaseClient is a second client on c's database: a writer apart from the pass, as
// another process would be.
func sameDatabaseClient(t *testing.T, c *Client) *Client {
	t.Helper()
	other, err := New(Config{
		BaseURL: c.baseURL, Database: c.commandURL[strings.LastIndex(c.commandURL, "/")+1:],
		User: envOr("ARCADEDB_USER", "root"), Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	return other
}

func countOutside(t *testing.T, c *Client, typeName, space string) int {
	t.Helper()
	rows, err := c.Query(context.Background(),
		"SELECT count(*) AS n FROM "+typeName+" WHERE embedding IS NOT NULL AND "+otherSpace,
		map[string]any{"space": space})
	if err != nil {
		t.Fatalf("count %s outside %s: %v", typeName, space, err)
	}
	return int(rowInt(rows[0], "n"))
}

// Review Focus 1, concurrent (final review #4, #9): a writer still on route A rewrites a
// trace while the pass is embedding its old text. The pass's write must not land over it --
// its vector describes text the row no longer holds -- so the gate stays closed until the
// next run embeds what the writer stored.
func TestMemoryPassLiveKeepsARowRewrittenDuringThePass(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	trace := validReasoningTrace()
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, trace); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	writer := sameDatabaseClient(t, client).WithEmbedder(routeA)
	rewritten := trace
	rewritten.ProviderSummary = "PassRewrite: the summary a stale writer stored mid-pass."
	var once sync.Once
	routeB := &hookEmbedder{inner: constantEmbedder{value: 2, space: "es1-route-b"}, hook: func([]string) {
		once.Do(func() {
			if err := writer.UpsertReasoningTrace(ctx, rewritten); err != nil {
				t.Errorf("stale UpsertReasoningTrace: %v", err)
			}
		})
	}}
	onB := client.WithEmbedder(routeB)
	if _, err := onB.reembedType(ctx, traceSpace, backfillBatch, 0); err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if n := countOutside(t, client, reasoningTraceType, "es1-route-b"); n != 1 {
		t.Fatalf("traces outside route B after the pass = %d, want the rewritten one", n)
	}
	if _, err := onB.reembedType(ctx, traceSpace, backfillBatch, 0); err != nil {
		t.Fatalf("second reembedType: %v", err)
	}
	if n := countOutside(t, client, reasoningTraceType, "es1-route-b"); n != 0 {
		t.Fatalf("traces outside route B after the second run = %d, want 0", n)
	}
}

// Final review #8: rows no read can use must not keep dense memory off. A fact with no text
// (NULL or blank) is set aside instead of kept with its old vector; an expired trace is not
// selected at all.
func TestMemoryPassLiveLeavesNothingTheGateCannotClear(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	for _, subject := range []string{"EmptyNull", "EmptyBlank"} {
		fact := mergeFact(subject, "keeps", "EmptyObject", subject+" keeps the empty object.")
		if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	for statement, value := range map[string]any{"EmptyNull keeps the empty object.": nil, "EmptyBlank keeps the empty object.": "   "} {
		if _, err := client.Command(ctx, "UPDATE FACT SET statement = :value WHERE statement = :statement",
			map[string]any{"value": value, "statement": statement}); err != nil {
			t.Fatalf("empty the statement %q: %v", statement, err)
		}
	}
	trace := validReasoningTrace()
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, trace); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	if _, err := client.Command(ctx, "UPDATE ReasoningTrace SET expires_at = :past WHERE trace_id = :trace_id",
		map[string]any{"past": now.Add(-time.Hour).Format(time.RFC3339Nano), "trace_id": trace.TraceID}); err != nil {
		t.Fatalf("expire the trace: %v", err)
	}

	onB := client.WithEmbedder(constantEmbedder{value: 2, space: "es1-route-b"})
	tally, err := onB.reembedMemory(ctx)
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 0 || tally.refused != 2 {
		t.Fatalf("tally = %+v, want the two empty facts set aside and the expired trace untouched", tally)
	}
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || !open {
		t.Fatalf("after the pass: open=%v err=%v, want open", open, err)
	}
	if n := countOutside(t, client, reasoningTraceType, "es1-route-b"); n != 1 {
		t.Fatalf("expired traces re-embedded: %d left outside route B, want 1", n)
	}
}

// Final review #5: the cursor pages across RID positions the way the engine orders them.
// A string parameter compared as text would put #N:10 before #N:9 and skip rows on every
// run; a small page makes the pass cross position 9 to 10 many times.
func TestMemoryPassLivePagesByRIDAcrossPositionTen(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	seeded := 0
	for ; seeded < 600 && (seeded%10 != 0 || maxRIDPosition(t, client) < 12); seeded++ {
		fact := mergeFact(fmt.Sprintf("Paged%d", seeded), "keeps", "PagedObject",
			fmt.Sprintf("Paged%d keeps the paged object.", seeded))
		if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%d): %v", seeded, err)
		}
	}
	if highest := maxRIDPosition(t, client); highest < 12 {
		t.Fatalf("%d facts never reached RID position 12; the test cannot show the crossing", seeded)
	} else {
		t.Logf("%d facts reach RID position %d, paged 4 at a time", seeded, highest)
	}
	tally, err := client.WithEmbedder(constantEmbedder{value: 2, space: "es1-route-b"}).
		reembedType(ctx, factSpace, 4, 0)
	if err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if n := countOutside(t, client, factEdgeType, "es1-route-b"); n != 0 || tally.embedded != seeded {
		t.Fatalf("after one run: %d facts outside route B, %d of %d embedded", n, tally.embedded, seeded)
	}
}

// The production path end to end, against a real model (final review recommendation): the
// route attests the local sidecar on every read of its space and embeds in batches with the
// document prefix, and the pass moves a whole memory into that space. Every other pass test
// uses fake embedders. The logged time per record is the pass's load on the sidecar.
func TestMemoryPassLiveWithTheRealEmbedder(t *testing.T) {
	real := liveEmbedder(t)
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	before := constantEmbedder{value: 1, space: "es1-before"}
	const facts = 64
	for i := range facts {
		fact := mergeFact(fmt.Sprintf("Archive%d", i), "keeps", "ArchiveObject",
			fmt.Sprintf("Archive%d keeps notebook number %d in the Turin archive.", i, i))
		if _, err := client.WithEmbedder(before).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%d): %v", i, err)
		}
	}
	projection := liveConversationProjection("identity-a", "conversation-real", 1,
		"We talked about the Turin archive and where its notebooks are kept.")
	if err := client.WithEmbedder(before).ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if err := client.WithEmbedder(before).UpsertReasoningTrace(ctx, validReasoningTrace()); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}

	space, err := real.Space(ctx)
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	onReal := client.WithEmbedder(real)
	start := time.Now()
	tally, err := onReal.reembedMemory(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != facts+2 || tally.refused != 0 || tally.failed != 0 {
		t.Fatalf("tally = %+v, want %d facts, the turn and the trace embedded", tally, facts)
	}
	t.Logf("space %s (%s): %d records in %s, %.0f ms each",
		space.ID, space.Label, tally.embedded, elapsed.Round(time.Millisecond),
		float64(elapsed.Milliseconds())/float64(tally.embedded))
	if open, err := onReal.memoryDenseOpen(ctx, space.ID); err != nil || !open {
		t.Fatalf("after the pass: open=%v err=%v, want open", open, err)
	}
	hybrid, err := onReal.SearchFactsHybrid(ctx, "dove sono i quaderni dell'archivio di Torino", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if hybrid.RetrievalPath != retrievalPathHybrid || len(hybrid.Facts) == 0 {
		t.Fatalf("an Italian question over the re-embedded memory = %+v, want dense hits", hybrid)
	}
}

func maxRIDPosition(t *testing.T, c *Client) int {
	t.Helper()
	rows, err := c.Query(context.Background(), "SELECT @rid AS rid FROM FACT", nil)
	if err != nil {
		t.Fatalf("list fact rids: %v", err)
	}
	highest := -1
	for _, row := range rows {
		rid := rowString(row, "rid")
		if n, err := strconv.Atoi(rid[strings.Index(rid, ":")+1:]); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

func TestMemoryPassLiveMovesTheMemoryAndReopensTheGate(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	for _, subject := range []string{"PassOne", "PassTwo", "PassPoison"} {
		fact := mergeFact(subject, "keeps", "PassObject", subject+" keeps the pass object.")
		if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	projection := liveConversationProjection("identity-a", "conversation-pass", 1, "passturnamber")
	if err := client.WithEmbedder(routeA).ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, validReasoningTrace()); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}

	routeB := &refusingEmbedder{refuse: []string{"PassPoison keeps the pass object."}, status: http.StatusBadRequest, space: "es1-route-b"}
	onB := client.WithEmbedder(routeB)
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || open {
		t.Fatalf("before the pass: open=%v err=%v, want closed", open, err)
	}
	tally, err := onB.reembedMemory(ctx)
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 4 || tally.refused != 1 {
		t.Fatalf("tally = %+v, want 2 facts + 1 turn + 1 trace embedded and 1 fact refused", tally)
	}
	onB.memoryGate.checked = time.Time{}
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || !open {
		t.Fatalf("after the pass: open=%v err=%v, want open (the refusal does not count)", open, err)
	}

	// Review Focus 1: a stale writer still on route A closes the gate again, and the next
	// run re-embeds what it wrote.
	stale := mergeFact("PassLate", "keeps", "PassObject", "PassLate keeps the pass object.")
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, stale, now); err != nil {
		t.Fatalf("stale UpsertFact: %v", err)
	}
	onB = client.WithEmbedder(routeB)
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); open {
		t.Fatal("a stale writer's vector did not close the gate")
	}
	if _, err := onB.reembedMemory(ctx); err != nil {
		t.Fatalf("second reembedMemory: %v", err)
	}
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); !open {
		t.Fatal("the second run did not re-embed the stale writer's fact")
	}
}
