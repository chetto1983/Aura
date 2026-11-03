// Package examplebank keeps what a person confirmed as vectors in their own memory database, and
// builds from it, with each domain's shipped seeds, the identity's classifier
// (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md). It knows no domain: what text
// becomes a vector, which facts are appended, when an example is written and how its answers are
// scored are the domain's. The math is semindex's.
package examplebank

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/semindex"
)

// Seed is one shipped example: the text its vector embeds, the facts appended to it, its label.
type Seed struct {
	Text  string
	Extra []float64
	Label string
}

// Domain is what one consumer brings. An example counts ExampleWeight times, a seed once. Owners
// lists the owners whose examples still count: any other owner's are left out and forgotten.
type Domain struct {
	Name          string
	Seeds         []Seed
	ExampleWeight int
	Owners        func(ctx context.Context, identityID string) (map[string]bool, error)
}

// Store is where an identity's examples live.
type Store interface {
	PutExample(ctx context.Context, identityID string, e arcadedb.Example) error
	Examples(ctx context.Context, identityID, domain, space string) ([]arcadedb.Example, error)
	ForgetExamples(ctx context.Context, identityID, domain, owner string) (int, error)
}

// ErrSeedsWarming says the domain's seeds are still being embedded for this space. The build runs
// in the background, so a caller on a deadline answers nothing now rather than wait for it.
var ErrSeedsWarming = errors.New("examplebank: the seeds are still being embedded")

// Bank serves every identity and domain of a process. The zero value with a Store and an Embedder
// is ready.
type Bank struct {
	Store    Store
	Embedder arcadedb.DenseEmbedder

	mu          sync.Mutex
	seeds       map[seedKey][][]float64
	warming     map[seedKey]bool
	classifiers map[bankKey]*semindex.Classifier
	// generation moves with every write to an identity's domain, so a classifier built from what
	// was read before the write is never cached after it.
	generation map[ownerKey]int
}

type seedKey struct{ domain, space string }

type ownerKey struct{ identity, domain string }

type bankKey struct {
	ownerKey
	space string
}

// Row is a stored vector: the embedding normalised, then the domain's facts as they are.
func Row(embedding, extra []float64) []float64 {
	var sum float64
	for _, x := range embedding {
		sum += x * x
	}
	norm := math.Sqrt(sum)
	out := make([]float64, 0, len(embedding)+len(extra))
	for _, x := range embedding {
		if norm > 0 {
			x /= norm
		}
		out = append(out, x)
	}
	return append(out, extra...)
}

// Embed embeds texts and names the space of the vectors. The space is read before and after, as
// document retrieval does, so vectors embedded across a model swap are refused, not mislabelled.
func (b *Bank) Embed(ctx context.Context, texts []string) ([][]float64, string, error) {
	before, err := b.Embedder.Space(ctx)
	if err != nil {
		return nil, "", err
	}
	vectors, err := b.Embedder.Embed(ctx, texts)
	if err != nil {
		return nil, "", err
	}
	after, err := b.Embedder.Space(ctx)
	if err != nil {
		return nil, "", err
	}
	if before.ID != after.ID {
		return nil, "", errors.New("examplebank: the embedding space changed during the request")
	}
	if len(vectors) != len(texts) {
		return nil, "", fmt.Errorf("examplebank: %d vectors for %d texts", len(vectors), len(texts))
	}
	return vectors, after.ID, nil
}

// Classifier is the identity's classifier for d in space: d's seeds once, each live example
// ExampleWeight times. It is built once and kept until the identity's examples in d change.
func (b *Bank) Classifier(ctx context.Context, identityID string, d Domain, space string) (*semindex.Classifier, error) {
	key := bankKey{ownerKey{identityID, d.Name}, space}
	b.mu.Lock()
	b.init()
	if classifier, ok := b.classifiers[key]; ok {
		b.mu.Unlock()
		return classifier, nil
	}
	seeds, ready := b.seedRows(ctx, d, space)
	generation := b.generation[key.ownerKey]
	b.mu.Unlock()
	if !ready {
		return nil, ErrSeedsWarming
	}
	classifier, complete, err := b.build(ctx, identityID, d, space, seeds)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if complete && b.generation[key.ownerKey] == generation {
		b.classifiers[key] = classifier
	}
	return classifier, nil
}

// build reads the examples and adds them to the seeds. complete is false when an orphan could not
// be forgotten, so the classifier is not kept and the next build tries again.
func (b *Bank) build(ctx context.Context, identityID string, d Domain, space string, seeds [][]float64) (*semindex.Classifier, bool, error) {
	examples, err := b.Store.Examples(ctx, identityID, d.Name, space)
	if err != nil {
		return nil, false, err
	}
	alive, err := d.Owners(ctx, identityID)
	if err != nil {
		return nil, false, err
	}
	classifier := semindex.NewClassifier(b.Embedder)
	for i, seed := range d.Seeds {
		classifier.AddVecs(seed.Label, seeds[i])
	}
	orphans := map[string]bool{}
	for _, example := range examples {
		if !alive[example.Owner] {
			orphans[example.Owner] = true
			continue
		}
		rows := make([][]float64, d.ExampleWeight)
		for i := range rows {
			rows[i] = example.Vector
		}
		classifier.AddVecs(example.Label, rows...)
	}
	complete := true
	for owner := range orphans {
		if _, err := b.Store.ForgetExamples(ctx, identityID, d.Name, owner); err != nil {
			complete = false
		}
	}
	return classifier, complete, nil
}

// Put stores a confirmed example of d.
func (b *Bank) Put(ctx context.Context, identityID string, d Domain, e arcadedb.Example) error {
	if e.Domain != d.Name {
		return fmt.Errorf("examplebank: an example of %q given to %q", e.Domain, d.Name)
	}
	defer b.changed(identityID, d.Name)
	return b.Store.PutExample(ctx, identityID, e)
}

// ForgetOwner removes an owner's examples of d.
func (b *Bank) ForgetOwner(ctx context.Context, identityID string, d Domain, owner string) error {
	defer b.changed(identityID, d.Name)
	_, err := b.Store.ForgetExamples(ctx, identityID, d.Name, owner)
	return err
}

// changed drops the identity's classifiers of a domain in every space, after the write, whether it
// succeeded or not.
func (b *Bank) changed(identityID, domain string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.init()
	owner := ownerKey{identityID, domain}
	b.generation[owner]++
	for key := range b.classifiers {
		if key.ownerKey == owner {
			delete(b.classifiers, key)
		}
	}
}

// init makes the maps. The caller holds b.mu.
func (b *Bank) init() {
	if b.classifiers != nil {
		return
	}
	b.seeds = map[seedKey][][]float64{}
	b.warming = map[seedKey]bool{}
	b.classifiers = map[bankKey]*semindex.Classifier{}
	b.generation = map[ownerKey]int{}
}
