package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMountedMemoryContextUsesTheAuthenticatedIdentityDigest(t *testing.T) {
	// entities and total deliberately disagree: the pointer asks for a one-entity
	// index (see the args assertion below), so `entities` is the size of the PAGE it
	// asked for and only `total` is the size of the memory. Reading the former told
	// the operator's agent it knew 1 entity when it knew 88 (measured 2026-09-04).
	client := &memoryReadinessClient{text: `{"text":"Davide located_in Caraglio","entities":1,"total":88,"facts":67,"covered":false}`}
	provider := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second)

	got, err := provider.Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if client.name != "memory_digest" {
		t.Fatalf("tool = %q, want memory_digest", client.name)
	}
	// The fixture round-trips args through a real JSON-RPC wire (no hand-rolled
	// mcptools double survives, per D-103), so JSON numbers decode as float64.
	// One entity, one fact: the counts are the whole payload now, so asking the server to
	// render fifty entities would be work whose output is thrown away -- which is
	// exactly why the count of rendered entities cannot be the count reported.
	if client.args["limit"] != float64(1) || client.args["facts_per_entity"] != float64(1) {
		t.Fatalf("args = %+v", client.args)
	}
	if aura, present := client.meta["aura"]; present {
		t.Fatalf("memory digest sent proprietary Aura metadata: %v", aura)
	}
	if _, present := client.args["user_identifier"]; present {
		t.Error("identity leaked into wire arguments")
	}
	// The POINTER, not the memory. Carrying the digest's text is what removed the need to
	// recall: the agent paraphrased what was already in front of it and the retrieval tools
	// went uncalled, the neighbourhood hop among them — it exists only behind a tool call.
	if strings.Contains(got, "Davide located_in Caraglio") {
		t.Fatalf("the digest's content leaked back into the turn: %q", got)
	}
	if !strings.Contains(got, "67 facts across 88 entities") {
		t.Fatalf("context = %q, want the memory's shape and not the page's", got)
	}
	for _, wayIn := range []string{"memory_facts_about", "memory_recall", "memory_search", "memory-aura"} {
		if !strings.Contains(got, wayIn) {
			t.Fatalf("the pointer does not name %s, so it points nowhere: %q", wayIn, got)
		}
	}
	// `recent` earns its place here because the failure it prevents needs no tool call:
	// an agent that does not know the mode exists answers "I recall nothing" to a question
	// about the past and never opens the skill that would have told it.
	if !strings.Contains(got, "recent") {
		t.Fatalf("the pointer omits the one mode a backward-looking question needs: %q", got)
	}
	// Everything WITH semantics belongs to memory-aura alone. This block is in every turn
	// and the skill is loaded on demand, so a rule restated in both drifts in one of them.
	for _, skillOwned := range []string{"depth", "cursor", "supersede", "Person", "Object"} {
		if strings.Contains(got, skillOwned) {
			t.Fatalf("the pointer restates %q, which the memory-aura skill owns: %q", skillOwned, got)
		}
	}
}

// A memory MCP older than `total` omits it, and this binary restarts independently
// of that one -- so the field decodes to zero and the pointer would announce an
// empty memory to an agent whose index it just read one entity from.
func TestMountedMemoryContextNeverShrinksTheMemoryItWasShown(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"Davide located_in Caraglio","entities":1,"facts":67,"covered":false}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if !strings.Contains(got, "67 facts across 1 entities") {
		t.Fatalf("context = %q, want the count the index proved", got)
	}
}

func TestMountedMemoryContextCanAttachAfterDeferredMount(t *testing.T) {
	provider := newMemoryContextProvider(nil, 5, time.Second)
	if _, err := provider.Context(context.Background(), "identity-a"); err == nil {
		t.Fatal("context must fail before the deferred memory mount attaches")
	}

	client := &memoryReadinessClient{text: `{"text":"late memory","entities":1,"total":1,"facts":1,"covered":true}`}
	provider.setClient(client.mount(t, "identity-a"))

	got, err := provider.Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context after attach: %v", err)
	}
	if !strings.Contains(got, "1 facts across 1 entities") {
		t.Fatalf("context = %q, want the pointer once the mount attaches", got)
	}
}

func TestMountedMemoryContextRejectsBrokenResponses(t *testing.T) {
	for name, client := range map[string]*memoryReadinessClient{
		"transport": {err: errors.New("offline")},
		"malformed": {text: "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Context(context.Background(), "identity-a")
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMountedMemoryContextOmitsAnEmptyDigest(t *testing.T) {
	client := &memoryReadinessClient{text: `{"text":"  ","entities":0,"total":0,"facts":0,"covered":true}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Context(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("context = %q, want empty", got)
	}
}

// The preload reads through memory_recall, not memory_search: both rank the same
// way, but only recall also returns the entities the question reached, which is
// the difference between injecting what memory SAYS about the wording and
// injecting the piece of graph the wording landed on.
func TestMountedMemoryContextSearchPreloadsRelevantFacts(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[` +
		`{"kind":"fact","fact":{"statement":"Davide prefers Go"}},` +
		`{"kind":"fact","fact":{"statement":"lives in Caraglio"}},` +
		`{"kind":"conversation"}],"retrieval":{"abstained":false}}`}
	provider := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second)

	got, err := provider.Search(context.Background(), "identity-a", "what does the user prefer")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if client.name != "memory_recall" {
		t.Fatalf("tool = %q, want memory_recall", client.name)
	}
	if client.args["query"] != "what does the user prefer" || client.args["limit"] != float64(5) ||
		client.args["mode"] != "semantic" {
		t.Fatalf("args = %+v", client.args)
	}
	if aura, present := client.meta["aura"]; present {
		t.Fatalf("memory recall sent proprietary Aura metadata: %v", aura)
	}
	if !strings.Contains(got, "Davide prefers Go") || !strings.Contains(got, "lives in Caraglio") {
		t.Fatalf("preload = %q", got)
	}
}

// The nodes are the reason for the switch, so they have to reach the block -- and
// as edges read outwards from the node, not as the sentences they were written as.
func TestMountedMemoryContextSearchInjectsEntityOutlines(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[` +
		`{"kind":"fact","fact":{"statement":"Il progetto Aura usa ArcadeDB come memoria."}}],` +
		`"entities":[{"name":"ArcadeDB","kind":"System","facts":[` +
		`{"subject":"Aura","predicate":"usa_memoria_a_lungo_termine","object":"ArcadeDB"},` +
		`{"subject":"ArcadeDB","predicate":"non_supporta","object":"Ri-puntare-endpoint"}]}],` +
		`"retrieval":{"abstained":false}}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "memoria a lungo termine")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(got, "ArcadeDB (System):") {
		t.Fatalf("preload carries no entity outline: %q", got)
	}
	if !strings.Contains(got, "non_supporta -> Ri-puntare-endpoint") {
		t.Fatalf("outward edge missing: %q", got)
	}
	// The first fact names ArcadeDB as its OBJECT, so from this node the relation
	// points the other way and must not be printed as though the node held it.
	if !strings.Contains(got, "usa_memoria_a_lungo_termine (of) -> Aura") {
		t.Fatalf("inbound edge printed unreversed: %q", got)
	}
}

// A node with more edges than the outline budget is trimmed, not inlined whole:
// the outline exists to show the node is worth opening, not to be the read.
func TestMountedMemoryContextSearchBoundsEntityOutlines(t *testing.T) {
	facts := make([]string, 0, 9)
	for index := range 9 {
		facts = append(facts, `{"subject":"N","predicate":"p`+string(rune('a'+index))+`","object":"o"}`)
	}
	client := &memoryReadinessClient{text: `{"evidence":[],"entities":[{"name":"N","facts":[` +
		strings.Join(facts, ",") + `]}],"retrieval":{"abstained":false}}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if edges := strings.Count(got, " -> "); edges != preloadEdgesPerEntity {
		t.Fatalf("edges = %d, want %d: %q", edges, preloadEdgesPerEntity, got)
	}
}

func TestMountedMemoryContextSearchAbstainsToEmpty(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[{"kind":"fact","fact":{"statement":"x"}}],"retrieval":{"abstained":true}}`}
	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "" {
		t.Fatalf("abstention must yield empty, got %q", got)
	}
}

func TestMountedMemoryContextSearchRejectsBrokenResponses(t *testing.T) {
	for name, client := range map[string]*memoryReadinessClient{
		"transport": {err: errors.New("offline")},
		"malformed": {text: "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).Search(context.Background(), "identity-a", "q"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
