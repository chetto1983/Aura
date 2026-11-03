//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestConfirmedExamplesPutReplaceListForget(t *testing.T) {
	c, ctx := disposableArcadeClient(t), context.Background()
	for range 2 {
		if err := c.EnsureMemorySchema(ctx); err != nil {
			t.Fatalf("EnsureMemorySchema: %v", err)
		}
	}
	put := func(owner, ref, label, space string, seed float64) []float64 {
		t.Helper()
		vector := exampleVector(seed)
		if err := c.PutExample(ctx, Example{
			Domain: "mail_tier", Owner: owner, Ref: ref, Label: label, Vector: vector, Space: space, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("put %s/%s: %v", owner, ref, err)
		}
		return vector
	}
	put("a", "1", "today", "s1", 1)
	put("a", "2", "later", "s1", 2)
	put("b", "1", "now", "s2", 3)
	replaced := put("a", "1", "now", "s1", 4)

	examples, err := c.Examples(ctx, "mail_tier", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 2 {
		t.Fatalf("s1 holds %d examples, want 2: %+v", len(examples), examples)
	}
	for _, example := range examples {
		if example.Owner != "a" || example.Space != "s1" {
			t.Fatalf("example = %+v", example)
		}
		if example.Ref != "1" {
			continue
		}
		if example.Label != "now" {
			t.Fatalf("a second confirmation did not replace the first: label %q", example.Label)
		}
		if len(example.Vector) != len(replaced) {
			t.Fatalf("vector width %d, want %d", len(example.Vector), len(replaced))
		}
		for i := range replaced {
			if math.Abs(example.Vector[i]-replaced[i]) > 1e-6 {
				t.Fatalf("component %d = %v, want %v", i, example.Vector[i], replaced[i])
			}
		}
	}

	forgotten, err := c.ForgetExamples(ctx, "mail_tier", "a")
	if err != nil || forgotten != 2 {
		t.Fatalf("forgot %d, %v; want 2", forgotten, err)
	}
	if left, err := c.Examples(ctx, "mail_tier", "s1"); err != nil || len(left) != 0 {
		t.Fatalf("s1 after the forget = %+v, %v", left, err)
	}
	if other, err := c.Examples(ctx, "mail_tier", "s2"); err != nil || len(other) != 1 {
		t.Fatalf("s2 after the forget = %+v, %v", other, err)
	}
}

// exampleVector is a 772-wide vector, the mail domain's width: 768 for the embedding and four facts.
func exampleVector(seed float64) []float64 {
	vector := make([]float64, 772)
	for i := range vector {
		vector[i] = math.Sin(seed*float64(i+1)) / 30
	}
	return vector
}
