package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestListEntitiesMapsRowsAndBoundsTheQuery(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[
		{"name":"Davide","kind":"Person","degree":4},
		{"name":"Aura","kind":"Project","degree":2}]}`)
	got, err := client.ListEntities(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(got) != 2 || got[0] != (EntitySummary{Name: "Davide", Kind: "Person", Facts: 4}) {
		t.Fatalf("entities = %+v", got)
	}
	if !strings.Contains(rec.statements[0], "LIMIT 50") {
		t.Fatalf("default bound missing: %s", rec.statements[0])
	}
	if _, err := client.ListEntities(context.Background(), 7); err != nil {
		t.Fatalf("ListEntities custom limit: %v", err)
	}
	if !strings.Contains(rec.statements[1], "LIMIT 7") {
		t.Fatalf("custom bound missing: %s", rec.statements[1])
	}
	if _, err := client.ListEntities(context.Background(), 1000); err != nil {
		t.Fatalf("ListEntities capped limit: %v", err)
	}
	if !strings.Contains(rec.statements[2], "LIMIT 100") {
		t.Fatalf("hard bound missing: %s", rec.statements[2])
	}
}

func TestListEntitiesPreservesServerErrors(t *testing.T) {
	srv := fakeServer(t, http.StatusForbidden, `{"detail":"tenant refused"}`, nil)
	if _, err := mustClient(t, srv.URL).ListEntities(context.Background(), 1); err == nil ||
		!strings.Contains(err.Error(), "tenant refused") {
		t.Fatalf("error = %v", err)
	}
}

func TestDigestIndexesBothFactEndpointsDeterministically(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[
		{"subject":"Davide","object":"Aura","predicate":"builds","statement":"Davide builds Aura."},
		{"subject":"Marta","object":"Aura","predicate":"uses","statement":"Marta uses Aura."},
		{"subject":"Davide","object":"Caraglio","predicate":"","statement":"Davide lives in Caraglio."}]}`)
	got, err := client.Digest(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if got.Entities != 2 || got.Facts != 3 || got.Covered {
		t.Fatalf("digest metadata = %+v", got)
	}
	lines := strings.Split(got.Text, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "- Aura") ||
		!strings.HasPrefix(lines[1], "- Davide") {
		t.Fatalf("digest order/text = %q", got.Text)
	}
	if !strings.Contains(lines[0], "uses Marta") && !strings.Contains(lines[0], "builds Davide") {
		t.Fatalf("inbound hook missing: %q", lines[0])
	}
	if !strings.Contains(rec.statements[0], "LIMIT 2000") || rec.params[0]["as_of"] == nil {
		t.Fatalf("bounded temporal query missing: %s params=%v", rec.statements[0], rec.params[0])
	}
}

func TestDigestDefaultsAndReportsCompleteCoverage(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[{"subject":"A","object":"B","statement":"A relates to B."}]}`)
	got, err := client.Digest(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if !got.Covered || got.Entities != 2 || !strings.Contains(got.Text, "A relates to B") {
		t.Fatalf("digest = %+v", got)
	}
	if len(rec.statements) != 1 {
		t.Fatalf("queries = %d, want one", len(rec.statements))
	}
}

func TestDigestHonoursConfiguredHardCaps(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[
		{"subject":"A","object":"B","predicate":"first"},
		{"subject":"A","object":"C","predicate":"second"}]}`)
	client.limits = MemoryLimits{Results: 1, DigestFactsPerEntity: 1, DigestScan: 7}.normalized()
	got, err := client.Digest(context.Background(), 1000, 1000)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if got.Entities != 1 || strings.Count(got.Text, ";") != 0 {
		t.Fatalf("digest exceeded configured caps: %+v", got)
	}
	if !strings.Contains(rec.statements[0], "LIMIT 7") {
		t.Fatalf("scan cap missing: %s", rec.statements[0])
	}
}

func TestDigestAndHooksHandleErrorsFallbacksAndUnicode(t *testing.T) {
	srv := fakeServer(t, http.StatusInternalServerError, `{"detail":"read failed"}`, nil)
	if _, err := mustClient(t, srv.URL).Digest(context.Background(), 1, 1); err == nil {
		t.Fatal("expected digest error")
	}
	row := map[string]any{"statement": "  fallback   sentence  "}
	if got := digestHook(row); got != "fallback sentence" {
		t.Fatalf("digestHook = %q", got)
	}
	if got := digestHookInbound(row); got != "fallback sentence" {
		t.Fatalf("digestHookInbound = %q", got)
	}
	if got := digestHook(map[string]any{"predicate": "likes", "object": "tea"}); got != "likes tea" {
		t.Fatalf("digestHook predicate = %q", got)
	}
	if got := digestHookInbound(map[string]any{"predicate": "likes", "subject": "Davide"}); !strings.Contains(got, "likes Davide") {
		t.Fatalf("digestHookInbound predicate = %q", got)
	}
	long := strings.Repeat("è", digestHookRunes+5)
	got := truncateRunes(long+", ", digestHookRunes)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != digestHookRunes+1 {
		t.Fatalf("truncateRunes = %q (%d runes)", got, len([]rune(got)))
	}
}
