package arcadedb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExampleSchemaIsStampedAndHasNoVectorIndex(t *testing.T) {
	statements := exampleSchemaStatements()
	for _, stamp := range spaceStampStatements(ExampleType) {
		if !slices.Contains(statements, stamp) {
			t.Fatalf("schema lacks the space stamp %q", stamp)
		}
	}
	for _, statement := range statements {
		if strings.Contains(statement, "LSM_VECTOR") {
			t.Fatalf("an example bank is read whole, so no vector index: %q", statement)
		}
	}
}

func TestPutExampleRefusesAnIncompleteExample(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an incomplete example reached ArcadeDB: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Database: "tenant", User: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	complete := Example{
		Domain: "mail_tier", Owner: "acc", Ref: "m1", Label: "now",
		Vector: []float64{0.6, 0.8}, Space: "space-1", CreatedAt: time.Now(),
	}
	for name, broken := range map[string]func(*Example){
		"domain": func(e *Example) { e.Domain = "" },
		"owner":  func(e *Example) { e.Owner = " " },
		"ref":    func(e *Example) { e.Ref = "" },
		"label":  func(e *Example) { e.Label = "" },
		"vector": func(e *Example) { e.Vector = nil },
		"space":  func(e *Example) { e.Space = "" },
	} {
		example := complete
		broken(&example)
		if err := client.PutExample(context.Background(), example); err == nil {
			t.Fatalf("an example with no %s was accepted", name)
		}
	}
}

func TestExamplesAreNotInTheSpacePass(t *testing.T) {
	for _, family := range memorySpaceTypes {
		if family.name == ExampleType {
			t.Fatal("the space pass re-embeds from text, and an example keeps none")
		}
	}
}
