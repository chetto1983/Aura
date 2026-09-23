package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/config"
)

// Space names the vector space a route produces. ID is what gets stored beside a vector;
// Label is what an operator reads.
type Space struct {
	ID    string
	Label string
}

// spaceKey is the canonical form. Field order is fixed by the struct, so json.Marshal is a
// stable encoding; hashing it removes the ambiguity a separator-joined string had, where a
// model id's ":free" or an endpoint's port could make two routes print the same.
type spaceKey struct {
	V        int    `json:"v"`
	Recipe   int    `json:"recipe"`
	Dims     int    `json:"dims"`
	Route    string `json:"route"`
	Model    string `json:"model,omitempty"`
	Base     string `json:"base,omitempty"`
	Artifact string `json:"artifact,omitempty"`
}

// SpaceFor derives the space from a resolved route. base is used only for a manual
// endpoint, where it is what selects the model; OpenRouter's host never names a space.
// artifact is set only for the local route (AttestLocal).
func SpaceFor(kind config.EmbedKind, model, base string, dims int, artifact string) Space {
	key := spaceKey{
		V: 1, Recipe: RecipeVersion, Dims: dims, Route: string(kind),
		Model: strings.TrimSpace(model), Artifact: strings.TrimSpace(artifact),
	}
	if kind == config.EmbedEndpoint {
		key.Base = strings.TrimRight(strings.TrimSpace(base), "/")
	}
	canonical, err := json.Marshal(key)
	if err != nil {
		panic(fmt.Sprintf("embeddings: marshal space key: %v", err)) // strings and ints only
	}
	sum := sha256.Sum256(canonical)
	return Space{ID: "es1-" + hex.EncodeToString(sum[:8]), Label: spaceLabel(key)}
}

func spaceLabel(key spaceKey) string {
	what := key.Model
	switch key.Route {
	case string(config.EmbedLocal):
		what, _, _ = strings.Cut(key.Artifact, "|")
	case string(config.EmbedEndpoint):
		what = key.Base + " " + key.Model
	}
	return fmt.Sprintf("%s %s, %dd, recipe %d", key.Route, what, key.Dims, key.Recipe)
}

// ErrNoRoute means dense embedding is switched off: the local route with an empty base.
var ErrNoRoute = errors.New("embeddings: no embedding route is configured")

// RouteSpace resolves the space embed's route produces. Only the local route costs a call:
// its model is read from the sidecar (AttestLocal). A cloud model id is its own name.
func RouteSpace(ctx context.Context, client *http.Client, embed config.EmbedConfig, dims int) (Space, error) {
	base, _, model := config.ResolveEmbedRoute(embed, "")
	kind := config.EmbedRouteKind(embed)
	if kind != config.EmbedLocal {
		return SpaceFor(kind, model, base, dims, ""), nil
	}
	if strings.TrimSpace(base) == "" {
		return Space{}, ErrNoRoute
	}
	artifact, err := AttestLocal(ctx, client, base)
	if err != nil {
		return Space{}, err
	}
	return SpaceFor(kind, "", "", dims, artifact), nil
}
