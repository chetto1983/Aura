# Embedding route and space identity — Implementation Plan (1 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the OpenRouter embedding option reach OpenRouter; give every Go process a way to name the vector space its route produces; resolve the memory sidecar's route from `aura.settings` rows instead of its environment.

**Architecture:** `config.ResolveEmbedRoute` stops falling back to the chat LLM's base URL. A new pure function `embeddings.SpaceFor` hashes a canonical JSON description of a route into a stable id; `embeddings.RouteSpace` adds a runtime attestation of the local GGUF read from llama.cpp's `/v1/models`. `aura doctor` prints the resolved space, which is the first real consumer. A new `settings.EmbedRoute` builds the route from rows (absent vs empty respected, environment untouched), and `cmd/arcadedb-mcp` uses it.

**Tech Stack:** Go 1.26, `net/http/httptest`, llama.cpp server b10964 (`/v1/models`), Postgres `aura.settings`.

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` — this plan implements §0 and §1, and the route helper of §6/§7. Plans 2–5 (memory family, ingest, documents family, cockpit + MCP watcher + doctor families) follow.

## Global Constraints

- Every Go command runs in **WSL**, never as a Windows `.exe` (antivirus): `wsl -e bash -lc "cd /mnt/d/Aura && <command>"`. Never assign shell variables inside that string (wsl expands `$VAR` before bash runs).
- No file over 600 LOC; comments only where the why is non-obvious; follow the surrounding style.
- Commit on `master`; the pre-commit hook runs vet, gofmt, lint, file-size and dup — do not run them by hand first, and never pass `--no-verify`.
- Commit messages: imperative subject, body explaining why, ending with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- A test that encodes the defect being fixed may be rewritten; say so in the commit body. No other test may be edited to make code pass.
- The space id format is `es1-` + the first 16 hex digits of SHA-256 over the canonical JSON `{"v":1,"recipe":<RecipeVersion>,"dims":<n>,"route":"local|openrouter|endpoint","model":…,"base":…,"artifact":…}` (empty fields omitted; `base` only for `endpoint`).
- The local artifact is `<gguf file name>|size=<n>|params=<n>|embd=<n>|ftype=<s>` from `/v1/models` `data[0]`; `n_ctx` is excluded (it is the server's `-c`, not the model).
- `llm.DefaultBaseURL` = `https://openrouter.ai/api/v1`; the embed client appends `/v1/embeddings`, so every resolved base has its trailing `/v1` stripped.

## Review Focus

- **A non-OpenRouter chat provider with the OpenRouter embedding option** (Ollama, llama.cpp) — embeddings must still go to `https://openrouter.ai/api` with `OPENROUTER_API_KEY`. Pinned in Task 1.
- **A deleted `AURA_EMBED_MODEL` row with the variable still in the process environment** — the route must fall back to the environment only when the row is absent, and an explicitly empty row must win over the environment. Pinned in Task 4.
- **A llama.cpp `/v1/models` answer without `meta` or with several models** (older build, router mode) — attestation must fail with a named error, never produce a space from partial data. Pinned in Task 3.
- **Model ids containing `:`** (`:free`, `:nitro`) and endpoint URLs with ports — two different routes must never share an id. Pinned in Task 2.
- **The local sidecar still loading (HTTP 503) when `aura doctor` runs** — the probe keeps reporting "still loading" and does not fail on the extra attestation call. Pinned in Task 3.

---

### Task 1: The OpenRouter option reaches OpenRouter (live defect)

**Files:**
- Modify: `internal/config/config_routes.go` (whole file, 45 lines)
- Modify: `internal/config/config_embed.go:47-52` (the `CloudBaseURL` field comment)
- Modify: `cmd/arcadedb-mcp/boot_settings.go:3-13,94-99` (drop the chat-base argument and the `llm` import)
- Test: `internal/config/config_routes_embed_test.go` (add one test)
- Test: `cmd/arcadedb-mcp/boot_settings_test.go:147-160` (rewrite the subtest that encodes the defect)

**Interfaces:**
- Produces: `func ResolveEmbedRoute(embed EmbedConfig, apiKey string) (baseURL, credential, model string)` — the `llmBaseURL` parameter is removed. `(*Config).EmbedRoute()` keeps its signature.

- [ ] **Step 1: Write the failing config test**

Append to `internal/config/config_routes_embed_test.go`:

```go
// Measured on the lab VM 2026-09-23: the chat route was Ollama
// (AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1), and the cockpit's OpenRouter
// option -- which writes an EMPTY cloud base -- resolved embeddings to that Ollama server
// under an OpenRouter model id. Changing the chat model would also have re-routed embeddings.
func TestEmbedRouteOpenRouterOptionIgnoresTheChatBase(t *testing.T) {
	cfg := &Config{}
	cfg.Embed.BaseURL = "http://aura-llama-embed:8081"
	cfg.Embed.CloudModel = "qwen/qwen3-embedding-8b"
	cfg.LLM.BaseURL = "http://host.docker.internal:11434/v1"
	cfg.LLM.APIKey = "sk-or-v1-test"

	base, key, model := cfg.EmbedRoute()
	if base != "https://openrouter.ai/api" {
		t.Fatalf("OpenRouter option resolved to %q: embeddings followed the chat LLM's base", base)
	}
	if key != "sk-or-v1-test" || model != "qwen/qwen3-embedding-8b" {
		t.Errorf("route = (%q, %q), want the OpenRouter credential and the chosen model", key, model)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/config/ -run TestEmbedRoute -count=1 -v"`
Expected: `TestEmbedRouteOpenRouterOptionIgnoresTheChatBase` FAILS with `OpenRouter option resolved to "http://host.docker.internal:11434"`; the three existing `TestEmbedRoute*` tests pass.

- [ ] **Step 3: Fix the resolver**

Replace the whole of `internal/config/config_routes.go` with:

```go
package config

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// config_routes.go holds the model-backend route resolvers — the ONE-knob
// local↔cloud swap (D-28). Split out of config.go to keep that file under the
// 600-LOC cap (CLAUDE.md §No god class).

// EmbedRoute resolves the embeddings endpoint as a ONE-knob local↔cloud swap (D-28),
// the same shape STT and TTS already use: the cloud MODEL is the switch, and the local
// base is never a candidate for the cloud route. Empty model means the local sidecar at
// Embed.BaseURL with no auth; a model set means the cloud route — Embed.CloudBaseURL if
// the operator named a non-OpenRouter embedder, otherwise OpenRouter itself — always
// with the single OPENROUTER_API_KEY.
//
// "Otherwise OpenRouter itself" used to be "otherwise the chat LLM's base", and the
// cockpit's OpenRouter option writes exactly that empty cloud base. Measured on the lab VM
// 2026-09-23, whose chat route is Ollama: choosing OpenRouter sent embeddings to Ollama.
func (c *Config) EmbedRoute() (baseURL, apiKey, model string) {
	return ResolveEmbedRoute(c.Embed, c.LLM.APIKey)
}

// ResolveEmbedRoute exposes the daemon's route contract to processes that read the same
// aura.settings rows without loading the daemon's full configuration.
func ResolveEmbedRoute(embed EmbedConfig, apiKey string) (baseURL, credential, model string) {
	model = strings.TrimSpace(embed.CloudModel)
	if model == "" {
		return embed.BaseURL, "", "" // local sidecar, no auth
	}
	base := strings.TrimSpace(embed.CloudBaseURL)
	if base == "" {
		base = llm.DefaultBaseURL
	}
	// The embed client appends "/v1/<endpoint>" to its base, unlike the LLM and vision
	// clients, which append "/chat/completions" to a base that already carries /v1.
	// Without this strip the request would go to "/v1/v1/embeddings" and 404.
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1"), apiKey, model
}
```

In `internal/config/config_embed.go`, replace the `CloudBaseURL` field comment (the four lines starting `// AURA_EMBED_CLOUD_BASE_URL — optional`) with:

```go
	// AURA_EMBED_CLOUD_BASE_URL — optional, for an OpenAI-compatible embedder that is not
	// OpenRouter. Empty means OpenRouter itself (never the chat LLM's base), which is what
	// makes the common case a single setting. It must NOT carry a trailing /v1: this client
	// appends /v1/embeddings, unlike the STT and TTS clients that append /audio/… to a base
	// that already has it.
```

- [ ] **Step 4: Run the config tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/config/ -count=1"`
Expected: `ok  github.com/chetto1983/aura/internal/config`.

- [ ] **Step 5: Update the memory sidecar's caller and its defect-encoding test**

In `cmd/arcadedb-mcp/boot_settings.go`, remove `"github.com/chetto1983/aura/internal/llm"` from the imports and change the `ResolveEmbedRoute` call to:

```go
	baseURL, key, model := config.ResolveEmbedRoute(config.EmbedConfig{
		BaseURL:      strings.TrimSpace(localBase),
		CloudModel:   strings.TrimSpace(os.Getenv("AURA_EMBED_MODEL")),
		CloudBaseURL: strings.TrimSpace(os.Getenv("AURA_EMBED_CLOUD_BASE_URL")),
	}, apiKey)
```

In `cmd/arcadedb-mcp/boot_settings_test.go`, replace the subtest `"cloud uses shared LLM route and stored credential"` (it asserts that the chat base `https://openrouter.example/api/v1` becomes the embedding base — the defect) with:

```go
	t.Run("cloud ignores the chat LLM base", func(t *testing.T) {
		t.Setenv("AURA_EMBED_BASE_URL", "http://aura-llama-embed:8081")
		t.Setenv("AURA_EMBED_MODEL", "vendor/embed-v2")
		t.Setenv("AURA_LLM_BASE_URL", "http://host.docker.internal:11434/v1")
		route := embeddingRouteFromEnv("stored-key")
		if route.baseURL != "https://openrouter.ai/api" || route.model != "vendor/embed-v2" || route.apiKey != "stored-key" {
			t.Fatalf("cloud route = %+v, want OpenRouter whatever the chat LLM's base is", route)
		}
	})
```

- [ ] **Step 6: Run the sidecar tests, then race on both packages**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/arcadedb-mcp/ -run 'TestEmbeddingRoute|TestApplyBootSettings|TestLoadBootSettings' -count=1 -v"`
Expected: all PASS, including `cloud_ignores_the_chat_LLM_base`.

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go build ./... && go test -race ./internal/config/ ./cmd/arcadedb-mcp/ -count=1"`
Expected: build succeeds; both packages `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config_routes.go internal/config/config_embed.go internal/config/config_routes_embed_test.go cmd/arcadedb-mcp/boot_settings.go cmd/arcadedb-mcp/boot_settings_test.go
git commit -F - <<'EOF'
fix(embed): the OpenRouter option reaches OpenRouter, not the chat LLM's base

With AURA_EMBED_CLOUD_BASE_URL empty -- exactly what the cockpit's OpenRouter option
writes -- ResolveEmbedRoute fell back to the chat LLM's base URL. Measured on the lab
VM 2026-09-23, whose chat route is Ollama: choosing OpenRouter sent embeddings to
http://host.docker.internal:11434 under an OpenRouter model id, and any change of chat
model would have silently re-routed embeddings.

The empty cloud base now means llm.DefaultBaseURL, OpenRouter itself.

One test changed rather than the code: the memory sidecar's "cloud uses shared LLM
route" subtest asserted that the chat base becomes the embedding base, which is the
defect written down as a contract. It now asserts the opposite with an Ollama chat base.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: The recipe version and the space identity

**Files:**
- Modify: `internal/embeddings/tasks.go` (add `RecipeVersion`)
- Modify: `internal/config/config_routes.go` (add `EmbedKind`, `EmbedRouteKind`)
- Create: `internal/embeddings/space.go`
- Test: `internal/embeddings/space_test.go`, `internal/embeddings/prefix_parity_test.go`, `internal/config/config_routes_embed_test.go`

The spec's file table lists `internal/config/config_embed_space.go`; the identity lives in `internal/embeddings` instead, because `RecipeVersion` sits beside the prefixes there and `internal/embeddings` already imports `internal/config` — the other direction would be an import cycle.

**Interfaces:**
- Consumes: `config.ResolveEmbedRoute(embed, apiKey)` from Task 1.
- Produces:
  - `type config.EmbedKind string` with constants `config.EmbedLocal = "local"`, `config.EmbedOpenRouter = "openrouter"`, `config.EmbedEndpoint = "endpoint"`; `func config.EmbedRouteKind(embed config.EmbedConfig) config.EmbedKind`.
  - `const embeddings.RecipeVersion = 1`.
  - `type embeddings.Space struct { ID, Label string }`; `func embeddings.SpaceFor(kind config.EmbedKind, model, base string, dims int, artifact string) embeddings.Space`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_routes_embed_test.go`:

```go
func TestEmbedRouteKindFollowsTheResolver(t *testing.T) {
	for _, tc := range []struct {
		name  string
		embed EmbedConfig
		want  EmbedKind
	}{
		{"no model is the local sidecar", EmbedConfig{BaseURL: "http://aura-llama-embed:8081"}, EmbedLocal},
		{"a model with no cloud base is OpenRouter", EmbedConfig{CloudModel: "qwen/qwen3-embedding-8b"}, EmbedOpenRouter},
		{"a model with a cloud base is an endpoint", EmbedConfig{CloudModel: "vendor/embed-1", CloudBaseURL: "https://embed.example/v1"}, EmbedEndpoint},
		{"whitespace is not a model", EmbedConfig{CloudModel: "  ", CloudBaseURL: "https://embed.example"}, EmbedLocal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EmbedRouteKind(tc.embed); got != tc.want {
				t.Fatalf("EmbedRouteKind = %q, want %q", got, tc.want)
			}
		})
	}
}
```

Create `internal/embeddings/prefix_parity_test.go`:

```go
package embeddings

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The document prefix is part of every stored vector, and it is written twice: here and in
// the Python ingest worker. Until now only a comment in chunk.py said they must agree.
func TestPythonDocumentPrefixMatchesGo(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "services", "ingest", "chunk.py"))
	if err != nil {
		t.Fatalf("read services/ingest/chunk.py: %v", err)
	}
	match := regexp.MustCompile(`(?m)^EMBED_DOC_PREFIX = "([^"]*)"\s*$`).FindSubmatch(source)
	if match == nil {
		t.Fatal(`services/ingest/chunk.py no longer declares EMBED_DOC_PREFIX = "..." on one line`)
	}
	if got := string(match[1]); got != UntitledDocumentPrefix {
		t.Fatalf("Python EMBED_DOC_PREFIX = %q, Go UntitledDocumentPrefix = %q: stored vectors would differ by process", got, UntitledDocumentPrefix)
	}
}
```

Create `internal/embeddings/space_test.go`:

```go
package embeddings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// The id is a hash of one canonical form, so the form itself is the contract.
func TestSpaceIDIsTheHashOfTheCanonicalForm(t *testing.T) {
	got := SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, "")
	canonical := fmt.Sprintf(`{"v":1,"recipe":%d,"dims":768,"route":"openrouter","model":"qwen/qwen3-embedding-8b"}`, RecipeVersion)
	sum := sha256.Sum256([]byte(canonical))
	if want := "es1-" + hex.EncodeToString(sum[:8]); got.ID != want {
		t.Fatalf("ID = %q, want %q (hash of %s)", got.ID, want, canonical)
	}
}

func TestSpaceSeparatesWhatChangesTheVectors(t *testing.T) {
	const artifact = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	base := SpaceFor(config.EmbedLocal, "", "", 768, artifact)
	for name, other := range map[string]Space{
		"another local artifact": SpaceFor(config.EmbedLocal, "", "", 768, strings.Replace(artifact, "Q8_0", "Q4_K_M", 2)),
		"another width":          SpaceFor(config.EmbedLocal, "", "", 512, artifact),
		"a cloud model":          SpaceFor(config.EmbedOpenRouter, "google/gemini-embedding-2", "", 768, ""),
	} {
		if other.ID == base.ID {
			t.Errorf("%s shares the id %s with the local space", name, base.ID)
		}
	}
}

// Colon-separated strings were ambiguous: model ids carry ":free" and endpoints carry ports.
func TestSpaceIDsDoNotCollideOnColons(t *testing.T) {
	a := SpaceFor(config.EmbedEndpoint, "b", "https://a:8080", 768, "")
	b := SpaceFor(config.EmbedEndpoint, "8080:b", "https://a", 768, "")
	if a.ID == b.ID {
		t.Fatalf("two different endpoint routes share the id %s", a.ID)
	}
	free := SpaceFor(config.EmbedOpenRouter, "nvidia/nemotron-3-embed-1b:free", "", 768, "")
	paid := SpaceFor(config.EmbedOpenRouter, "nvidia/nemotron-3-embed-1b", "", 768, "")
	if free.ID == paid.ID {
		t.Fatalf(":free and the base model share the id %s", free.ID)
	}
}

// OpenRouter's host is not part of the space: a proxy for the same model must not demand a
// rebuild. A manual endpoint's base IS the model selector, so it is.
func TestSpaceIncludesTheBaseOnlyForAManualEndpoint(t *testing.T) {
	if SpaceFor(config.EmbedOpenRouter, "m", "https://proxy.example/api", 768, "").ID !=
		SpaceFor(config.EmbedOpenRouter, "m", "", 768, "").ID {
		t.Error("the OpenRouter space changed with the host")
	}
	if SpaceFor(config.EmbedEndpoint, "m", "https://one.example", 768, "").ID ==
		SpaceFor(config.EmbedEndpoint, "m", "https://two.example", 768, "").ID {
		t.Error("two manual endpoints share one space")
	}
	if SpaceFor(config.EmbedEndpoint, "m", "https://one.example/", 768, "").ID !=
		SpaceFor(config.EmbedEndpoint, "m", "https://one.example", 768, "").ID {
		t.Error("a trailing slash changed the endpoint space")
	}
}

func TestSpaceLabelIsReadable(t *testing.T) {
	const artifact = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	for _, tc := range []struct {
		space Space
		want  string
	}{
		{SpaceFor(config.EmbedLocal, "", "", 768, artifact), "local embeddinggemma-300M-Q8_0.gguf, 768d, recipe 1"},
		{SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, ""), "openrouter qwen/qwen3-embedding-8b, 768d, recipe 1"},
		{SpaceFor(config.EmbedEndpoint, "vendor/embed-1", "https://embed.example", 768, ""), "endpoint https://embed.example vendor/embed-1, 768d, recipe 1"},
	} {
		if tc.space.Label != tc.want {
			t.Errorf("Label = %q, want %q", tc.space.Label, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/config/ ./internal/embeddings/ -count=1"`
Expected: compile errors — `undefined: EmbedKind`, `undefined: EmbedRouteKind`, `undefined: SpaceFor`, `undefined: RecipeVersion`. (`TestPythonDocumentPrefixMatchesGo` would pass on its own: the prefixes agree today; it exists to catch drift.)

- [ ] **Step 3: Add the route kind**

Append to `internal/config/config_routes.go`:

```go
// EmbedKind names which of the three routes ResolveEmbedRoute takes. It is part of the
// embedding space a route produces (internal/embeddings/space.go).
type EmbedKind string

const (
	EmbedLocal      EmbedKind = "local"
	EmbedOpenRouter EmbedKind = "openrouter"
	EmbedEndpoint   EmbedKind = "endpoint"
)

// EmbedRouteKind reports the route ResolveEmbedRoute resolves embed to.
func EmbedRouteKind(embed EmbedConfig) EmbedKind {
	switch {
	case strings.TrimSpace(embed.CloudModel) == "":
		return EmbedLocal
	case strings.TrimSpace(embed.CloudBaseURL) == "":
		return EmbedOpenRouter
	default:
		return EmbedEndpoint
	}
}
```

- [ ] **Step 4: Add the recipe version**

Append to `internal/embeddings/tasks.go`:

```go
// RecipeVersion covers everything that turns the same text into a different stored vector
// without changing a model name: the prefixes above and their Python twin
// (services/ingest/chunk.py EMBED_DOC_PREFIX), llama.cpp's --embd-normalize in compose.yaml,
// and TruncateMRL. Bump it when any of them changes. It is part of every embedding space
// (space.go), so vectors stored under the old recipe stop matching the current space and
// are re-embedded.
const RecipeVersion = 1
```

- [ ] **Step 5: Write the space**

Create `internal/embeddings/space.go`:

```go
package embeddings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
```

- [ ] **Step 6: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/config/ ./internal/embeddings/ -count=1 -v -run 'EmbedRouteKind|Space|Prefix'"`
Expected: every listed test PASS.

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test -race ./internal/config/ ./internal/embeddings/ -count=1"`
Expected: both `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config_routes.go internal/config/config_routes_embed_test.go internal/embeddings/tasks.go internal/embeddings/space.go internal/embeddings/space_test.go internal/embeddings/prefix_parity_test.go
git commit -F - <<'EOF'
feat(embed): name the vector space a route produces

A stored vector is only comparable with vectors from the same model, recipe and width,
and nothing named that combination. SpaceFor hashes a canonical JSON form of it into a
short id (es1-<16 hex>) plus a readable label. Hashing a structured form, rather than
joining fields with colons, keeps ":free" model ids and endpoint ports from making two
routes print the same.

RecipeVersion covers what changes vectors without changing a model name: the task
prefixes, llama.cpp's --embd-normalize, TruncateMRL. The document prefix is also written
in Python, and until now only a comment said the two must agree; a test now reads
chunk.py and compares.

The identity lives in internal/embeddings rather than internal/config, as the spec's file
table had it, because embeddings already imports config.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: Attest the local model at run time; `aura doctor` prints the space

**Files:**
- Create: `internal/embeddings/attest.go`
- Modify: `internal/embeddings/space.go` (add `ErrNoRoute`, `RouteSpace`)
- Modify: `cmd/aura/doctor_embed_probe.go` (append the space to both branches)
- Test: `internal/embeddings/attest_test.go`, `cmd/aura/doctor_test.go:183-270`

**Interfaces:**
- Consumes: `SpaceFor`, `config.EmbedRouteKind`, `config.ResolveEmbedRoute` (Tasks 1–2); `serverRoot` (`internal/embeddings/fit.go:156-160`).
- Produces:
  - `func embeddings.AttestLocal(ctx context.Context, client *http.Client, baseURL string) (string, error)`.
  - `var embeddings.ErrNoRoute error`; `func embeddings.RouteSpace(ctx context.Context, client *http.Client, embed config.EmbedConfig, dims int) (embeddings.Space, error)` — plans 2, 3 and 5 call this to stamp and compare.

- [ ] **Step 1: Write the failing attestation tests**

Create `internal/embeddings/attest_test.go`:

```go
package embeddings

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// The body llama.cpp b10964 returned on the lab VM, 2026-09-23 (the Ollama-compatible
// "models" half trimmed; the attestation reads only "data").
const measuredModels = `{"object":"list","data":[{"id":"/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf","aliases":["/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf"],"tags":[],"object":"model","created":1790170262,"owned_by":"llamacpp","meta":{"vocab_type":true,"n_vocab":262144,"n_ctx":2048,"n_ctx_train":2048,"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`

func modelsServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAttestLocalNamesTheServedModel(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	got, err := AttestLocal(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("AttestLocal: %v", err)
	}
	const want = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	if got != want {
		t.Fatalf("artifact = %q, want %q", got, want)
	}
}

// The context size is the server's -c, not the model: restarting with another -c must not
// look like a model change.
func TestAttestLocalIgnoresTheContextSize(t *testing.T) {
	server := modelsServer(t, strings.ReplaceAll(measuredModels, `"n_ctx":2048`, `"n_ctx":8192`), http.StatusOK)
	got, err := AttestLocal(context.Background(), server.Client(), server.URL+"/v1")
	if err != nil {
		t.Fatalf("AttestLocal: %v", err)
	}
	if strings.Contains(got, "8192") {
		t.Fatalf("artifact %q carries the context size", got)
	}
}

func TestAttestLocalRefusesWhatItCannotName(t *testing.T) {
	for name, body := range map[string]string{
		"no meta":      `{"data":[{"id":"/m/a.gguf"}]}`,
		"no ftype":     strings.Replace(measuredModels, `,"ftype":"Q8_0"`, ``, 1),
		"two models":   `{"data":[{"id":"/m/a.gguf","meta":{"n_embd":768,"n_params":1,"size":1,"ftype":"Q8_0"}},{"id":"/m/b.gguf","meta":{"n_embd":768,"n_params":1,"size":1,"ftype":"Q8_0"}}]}`,
		"empty list":   `{"data":[]}`,
		"not json":     `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			server := modelsServer(t, body, http.StatusOK)
			if got, err := AttestLocal(context.Background(), server.Client(), server.URL); err == nil {
				t.Fatalf("AttestLocal = %q, want an error", got)
			}
		})
	}
	t.Run("loading", func(t *testing.T) {
		server := modelsServer(t, `{"error":{"code":503,"message":"Loading model"}}`, http.StatusServiceUnavailable)
		if _, err := AttestLocal(context.Background(), server.Client(), server.URL); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("err = %v, want the HTTP status named", err)
		}
	})
}

func TestRouteSpaceAttestsOnlyTheLocalRoute(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	local, err := RouteSpace(context.Background(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, 768)
	if err != nil {
		t.Fatalf("RouteSpace(local): %v", err)
	}
	if !strings.Contains(local.Label, "embeddinggemma-300M-Q8_0.gguf") {
		t.Fatalf("local label = %q", local.Label)
	}

	cloud, err := RouteSpace(context.Background(), nil, config.EmbedConfig{
		BaseURL: "http://unreachable.invalid", CloudModel: "qwen/qwen3-embedding-8b",
	}, 768)
	if err != nil {
		t.Fatalf("RouteSpace(cloud) touched the network or failed: %v", err)
	}
	if cloud.ID != SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, "").ID {
		t.Fatalf("cloud space = %+v", cloud)
	}

	if _, err := RouteSpace(context.Background(), nil, config.EmbedConfig{}, 768); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("empty local base: err = %v, want ErrNoRoute", err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/embeddings/ -count=1 -run 'Attest|RouteSpace'"`
Expected: compile errors — `undefined: AttestLocal`, `undefined: RouteSpace`, `undefined: ErrNoRoute`.

- [ ] **Step 3: Write the attestation**

Create `internal/embeddings/attest.go`:

```go
package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// localModelList is the slice of llama.cpp's /v1/models the attestation reads. Measured on
// the lab VM 2026-09-23 (llama.cpp b10964): data[0].id is the GGUF path, and meta carries
// n_embd 768, n_params 307581696, size 327060480 and ftype "Q8_0". n_ctx is left out on
// purpose: it is the -c the server was started with, not a property of the model.
type localModelList struct {
	Data []struct {
		ID   string `json:"id"`
		Meta struct {
			NEmbd   int    `json:"n_embd"`
			NParams int64  `json:"n_params"`
			Size    int64  `json:"size"`
			FType   string `json:"ftype"`
		} `json:"meta"`
	} `json:"data"`
}

// AttestLocal names the model the local sidecar actually serves, from the sidecar itself.
// AURA_EMBED_FINGERPRINT cannot do this: it is written once at install and nothing re-checks
// it, so a GGUF replaced on disk would keep the old name.
func AttestLocal(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverRoot(baseURL)+"/v1/models", nil)
	if err != nil {
		return "", fmt.Errorf("attest local embedder: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("attest local embedder: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("attest local embedder: /v1/models returned HTTP %d", response.StatusCode)
	}
	var list localModelList
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&list); err != nil {
		return "", fmt.Errorf("attest local embedder: decode /v1/models: %w", err)
	}
	if len(list.Data) != 1 {
		return "", fmt.Errorf("attest local embedder: the sidecar serves %d models, want exactly 1", len(list.Data))
	}
	model := list.Data[0]
	name := path.Base(strings.TrimSpace(model.ID))
	if name == "." || name == "/" || model.Meta.NEmbd <= 0 || model.Meta.NParams <= 0 ||
		model.Meta.Size <= 0 || strings.TrimSpace(model.Meta.FType) == "" {
		return "", fmt.Errorf("attest local embedder: /v1/models does not describe its model (id %q)", model.ID)
	}
	return fmt.Sprintf("%s|size=%d|params=%d|embd=%d|ftype=%s",
		name, model.Meta.Size, model.Meta.NParams, model.Meta.NEmbd, strings.TrimSpace(model.Meta.FType)), nil
}
```

Append to `internal/embeddings/space.go` (add `"context"`, `"errors"` and `"net/http"` to its imports):

```go
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
```

- [ ] **Step 4: Run the attestation tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test -race ./internal/embeddings/ -count=1"`
Expected: `ok  github.com/chetto1983/aura/internal/embeddings`.

- [ ] **Step 5: Extend the doctor tests**

In `cmd/aura/doctor_test.go`, in `TestDoctorEmbedProbeReportsWhatTheSidecarActuallyLoaded`, add a `/v1/models` case to the server's switch (before `default:`):

```go
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf","meta":{"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`))
```

and extend its `want` list to:

```go
	for _, want := range []string{"embeddinggemma-300M-Q8_0.gguf", "n_ctx 2048", "1 slot", "space es1-", "local embeddinggemma-300M-Q8_0.gguf, 768d"} {
```

In `TestDoctorEmbedProbeDoesNotCallTheCloudRoute`, after the existing model assertion, add:

```go
	if !strings.Contains(detail, "space es1-") || !strings.Contains(detail, "endpoint ") {
		t.Errorf("detail = %q, want the resolved space named", detail)
	}
```

- [ ] **Step 6: Run them and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/aura/ -run TestDoctorEmbedProbe -count=1 -v"`
Expected: `TestDoctorEmbedProbeReportsWhatTheSidecarActuallyLoaded` and `TestDoctorEmbedProbeDoesNotCallTheCloudRoute` FAIL with `want it to contain "space es1-"` / `want the resolved space named`; the loading test passes.

- [ ] **Step 7: Print the space in the probe**

In `cmd/aura/doctor_embed_probe.go`, add the import `"github.com/chetto1983/aura/internal/embeddings"`, then change the cloud branch and the final return of `defaultDoctorProbeEmbed`:

```go
	base, _, model := cfg.EmbedRoute()
	if model != "" {
		// A hosted embedder's health belongs to its provider, and probing it would bill a
		// call on every `aura doctor`. What this stack can be wrong about is the route.
		space, err := embeddings.RouteSpace(ctx, nil, cfg.Embed, cfg.Embed.Dimensions)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("cloud model %s (not probed), %s", model, describeSpace(space)), nil
	}
```

and replace `return describeEmbedProps(props), nil` with:

```go
	space, err := embeddings.RouteSpace(ctx, client, cfg.Embed, cfg.Embed.Dimensions)
	if err != nil {
		return "", err
	}
	return describeEmbedProps(props) + ", " + describeSpace(space), nil
```

Append:

```go
// describeSpace prints the id stored beside every vector and the name an operator reads, so
// a doctor run on two machines shows at a glance whether their corpora are comparable.
func describeSpace(space embeddings.Space) string {
	return fmt.Sprintf("space %s (%s)", space.ID, space.Label)
}
```

- [ ] **Step 8: Run the doctor tests, build, and race**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/aura/ -run 'TestDoctor' -count=1 -v"`
Expected: every `TestDoctor*` PASS.

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go build ./... && go test -race ./internal/embeddings/ ./cmd/aura/ -run 'Doctor|Attest|RouteSpace|Space' -count=1"`
Expected: build succeeds; both `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/embeddings/attest.go internal/embeddings/attest_test.go internal/embeddings/space.go cmd/aura/doctor_embed_probe.go cmd/aura/doctor_test.go
git commit -F - <<'EOF'
feat(embed): attest the local model from the sidecar; doctor names the space

The local route's space needs the model it actually serves, and AURA_EMBED_FINGERPRINT
cannot say: it is derived once at install and the updater never touches it, so a GGUF
replaced on disk keeps the old name. llama.cpp's /v1/models reports the file, size,
parameter count, width and quantization -- measured on the lab VM 2026-09-23 -- and that
is what AttestLocal reads. n_ctx is excluded: it is the server's -c, not the model.

RouteSpace resolves the space for any route; only the local one costs a call. aura doctor
prints it, so two machines' corpora can be compared at a glance, and it is the first
consumer of the identity that the next plans stamp beside every vector.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: Resolve the route from `aura.settings` rows; the memory sidecar uses it

**Files:**
- Create: `internal/settings/embed_route.go`
- Modify: `cmd/arcadedb-mcp/boot_settings.go` (whole file)
- Modify: `cmd/arcadedb-mcp/main.go:49-73` (use the resolved route; log the space)
- Test: `internal/settings/embed_route_test.go`, `cmd/arcadedb-mcp/boot_settings_test.go`

**Interfaces:**
- Consumes: `settings.Lister`, `(*settings.Store).Secret` (`internal/settings/settings.go:133-190`); `config.EmbedConfig`, `config.ResolveEmbedRoute` (Task 1); `embeddings.RouteSpace` (Task 3).
- Produces:
  - `type settings.SecretLister interface { Lister; Secret(ctx context.Context, key string) (string, error) }`
  - `func settings.EmbedRoute(ctx context.Context, store settings.SecretLister, lookupEnv func(string) (string, bool), defaultLocalBase string) (config.EmbedConfig, string, error)` — returns the route inputs and the sealed `OPENROUTER_API_KEY`. Plan 3's ingest supervisor and plan 5's MCP watcher call it on every tick; it never mutates the environment.

- [ ] **Step 1: Write the failing helper tests**

Create `internal/settings/embed_route_test.go`:

```go
package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeRouteStore struct {
	rows      []sqlc.AuraSettings
	listErr   error
	secret    string
	secretErr error
}

func (f fakeRouteStore) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, f.listErr }
func (f fakeRouteStore) Secret(context.Context, string) (string, error)    { return f.secret, f.secretErr }

func env(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestEmbedRouteRowsWinOverTheEnvironment(t *testing.T) {
	store := fakeRouteStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_EMBED_BASE_URL", Value: "http://settings-embed:8081"},
		{Key: "AURA_EMBED_MODEL", Value: "vendor/embed-v2"},
	}, secret: "stored-key"}
	embed, key, err := EmbedRoute(context.Background(), store, env(map[string]string{
		"AURA_EMBED_BASE_URL": "http://stale-env:8081", "AURA_EMBED_MODEL": "stale/model",
	}), "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if embed.BaseURL != "http://settings-embed:8081" || embed.CloudModel != "vendor/embed-v2" || key != "stored-key" {
		t.Fatalf("route = %+v key %q, want the rows and the sealed key", embed, key)
	}
}

// OverlayEnv only ever calls Setenv, so a row deleted after boot stayed in the process
// environment and a re-read could not see the deletion. This helper never touches the
// environment: an absent row falls back to it, an EMPTY row overrides it.
func TestEmbedRouteDistinguishesAnAbsentRowFromAnEmptyOne(t *testing.T) {
	processEnv := env(map[string]string{"AURA_EMBED_MODEL": "stale/model", "AURA_EMBED_BASE_URL": "http://compose:8081"})

	absent, _, err := EmbedRoute(context.Background(), fakeRouteStore{}, processEnv, "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if absent.CloudModel != "stale/model" || absent.BaseURL != "http://compose:8081" {
		t.Fatalf("absent rows: route = %+v, want the environment", absent)
	}

	empty, _, err := EmbedRoute(context.Background(), fakeRouteStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_EMBED_MODEL", Value: ""}, {Key: "AURA_EMBED_BASE_URL", Value: ""},
	}}, processEnv, "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if empty.CloudModel != "" || empty.BaseURL != "" {
		t.Fatalf("empty rows: route = %+v, want empty (local route, dense retrieval off)", empty)
	}
}

func TestEmbedRouteDefaultsTheLocalBaseOnlyWhenNothingNamesIt(t *testing.T) {
	embed, _, err := EmbedRoute(context.Background(), fakeRouteStore{}, env(nil), "http://aura-llama-embed:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if embed.BaseURL != "http://aura-llama-embed:8081" || embed.CloudModel != "" || embed.CloudBaseURL != "" {
		t.Fatalf("route = %+v, want the product default local base", embed)
	}
}

func TestEmbedRouteFailsClosed(t *testing.T) {
	for name, store := range map[string]fakeRouteStore{
		"rows unreadable":   {listErr: errors.New("postgres unavailable")},
		"secret unreadable": {secretErr: errors.New("sealed row unreadable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := EmbedRoute(context.Background(), store, env(nil), "http://default:8081"); err == nil {
				t.Fatal("EmbedRoute succeeded; a route must never be guessed from stale state")
			}
		})
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/settings/ -run TestEmbedRoute -count=1"`
Expected: compile error — `undefined: EmbedRoute`.

- [ ] **Step 3: Write the helper**

Create `internal/settings/embed_route.go`:

```go
package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/config"
)

// SecretLister is what reading a route needs: the rows, and the one sealed credential.
type SecretLister interface {
	Lister
	Secret(ctx context.Context, key string) (string, error)
}

// EmbedRoute reads the embedding route from aura.settings without touching the process
// environment. A present row wins even when empty (an empty AURA_EMBED_BASE_URL switches
// dense retrieval off); an absent row falls back to lookupEnv, then to defaultLocalBase
// for the local base only -- the same precedence OverlayEnv gives at boot, minus its one
// flaw: OverlayEnv never unsets, so a process re-reading through it could not see a
// deleted row. The credential is the sealed OPENROUTER_API_KEY, the one the daemon's
// EmbedRoute also uses; there is no embedding-specific key.
func EmbedRoute(
	ctx context.Context, store SecretLister, lookupEnv func(string) (string, bool), defaultLocalBase string,
) (config.EmbedConfig, string, error) {
	rows, err := store.List(ctx)
	if err != nil {
		return config.EmbedConfig{}, "", fmt.Errorf("embedding route: %w", err)
	}
	stored := make(map[string]string, len(rows))
	for _, row := range rows {
		stored[row.Key] = row.Value
	}
	value := func(key, fallback string) string {
		if v, ok := stored[key]; ok {
			return strings.TrimSpace(v)
		}
		if v, ok := lookupEnv(key); ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	key, err := store.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return config.EmbedConfig{}, "", fmt.Errorf("embedding credential: %w", err)
	}
	return config.EmbedConfig{
		BaseURL:      value("AURA_EMBED_BASE_URL", defaultLocalBase),
		CloudModel:   value("AURA_EMBED_MODEL", ""),
		CloudBaseURL: value("AURA_EMBED_CLOUD_BASE_URL", ""),
	}, strings.TrimSpace(key), nil
}
```

- [ ] **Step 4: Run the helper tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test -race ./internal/settings/ -run TestEmbedRoute -count=1 -v"`
Expected: the four `TestEmbedRoute*` PASS.

- [ ] **Step 5: Rewrite the memory sidecar's boot tests for the new shape**

The sidecar stops deriving its route from the environment it just overlaid; `embeddingRouteFromEnv` goes away, and so do the tests that exercised it. In `cmd/arcadedb-mcp/boot_settings_test.go`:

- Keep `fakeBootSettings`, `TestLoadBootSettingsRequiresPostgresDSNBeforeOpeningAnything`, `TestLoadBootSettingsRequiresAuthulaSecretBeforeOpeningAnything` and `TestLoadBootSettingsPropagatesOpenFailureWithoutFallback` unchanged.
- Replace `TestApplyBootSettingsMakesPostgresWinAndKeepsSecretOutOfEnv`, `TestApplyBootSettingsFailsClosed`, `TestLoadBootSettingsClosesTheBootstrapStore` and `TestEmbeddingRouteMatchesDaemonLocalAndCloudContract` with:

```go
func TestApplyBootSettingsResolvesTheRouteFromRowsAndKeepsSecretOutOfEnv(t *testing.T) {
	t.Setenv("AURA_EMBED_BASE_URL", "http://stale-env:8081")
	t.Setenv("AURA_EMBED_MODEL", "")
	t.Setenv("OPENROUTER_API_KEY", "inherited-but-not-authoritative")
	store := &fakeBootSettings{
		rows: []sqlc.AuraSettings{
			{Key: "AURA_EMBED_BASE_URL", Value: "http://settings-embed:8081"},
			{Key: "AURA_EMBED_MODEL", Value: "vendor/embed-v2"},
		},
		secret: "stored-openrouter-key",
	}

	route, err := applyBootSettings(t.Context(), store)
	if err != nil {
		t.Fatalf("applyBootSettings: %v", err)
	}
	if route.baseURL != "https://openrouter.ai/api" || route.model != "vendor/embed-v2" || route.apiKey != "stored-openrouter-key" {
		t.Fatalf("route = %+v, want the stored model on OpenRouter with the sealed key", route)
	}
	if store.secretKey != "OPENROUTER_API_KEY" {
		t.Fatalf("secret read as %q, want OPENROUTER_API_KEY", store.secretKey)
	}
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "inherited-but-not-authoritative" {
		t.Fatalf("OPENROUTER_API_KEY = %q: the stored secret must never enter the environment", got)
	}
}

func TestApplyBootSettingsKeepsTheLocalRouteWithoutAModel(t *testing.T) {
	for _, key := range []string{"AURA_EMBED_BASE_URL", "AURA_EMBED_MODEL", "AURA_EMBED_CLOUD_BASE_URL"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	route, err := applyBootSettings(t.Context(), &fakeBootSettings{secret: "stored-key"})
	if err != nil {
		t.Fatalf("applyBootSettings: %v", err)
	}
	if route.baseURL != "http://aura-llama-embed:8081" || route.model != "" || route.apiKey != "" {
		t.Fatalf("local route = %+v, want the product default with no model or credential", route)
	}
}

func TestApplyBootSettingsFailsClosed(t *testing.T) {
	for name, store := range map[string]*fakeBootSettings{
		"settings overlay": {listErr: errors.New("postgres unavailable")},
		"secret read":      {secretErr: errors.New("sealed row unreadable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := applyBootSettings(t.Context(), store); err == nil {
				t.Fatal("applyBootSettings succeeded; boot must not fall back to stale env")
			}
		})
	}
}

func TestLoadBootSettingsClosesTheBootstrapStore(t *testing.T) {
	closed := false
	store := &fakeBootSettings{secret: "stored-key"}
	_, err := loadBootSettingsWith(t.Context(), "postgres://db/aura", "authula-secret", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		return store, func() { closed = true }, nil
	})
	if err != nil {
		t.Fatalf("loadBootSettingsWith: %v", err)
	}
	if !closed {
		t.Fatal("bootstrap Postgres pool was not closed")
	}
}
```

- [ ] **Step 6: Run them and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/arcadedb-mcp/ -run 'BootSettings' -count=1"`
Expected: compile errors — `route.baseURL undefined (type string has no field or method baseURL)` (`applyBootSettings` still returns the key).

- [ ] **Step 7: Rewrite `boot_settings.go`**

Replace the whole of `cmd/arcadedb-mcp/boot_settings.go` with:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/settings"
)

// This is the product default when neither a row nor the environment names a local base.
// An explicitly empty row still wins, so the operator can disable dense retrieval.
const defaultMemoryEmbedBaseURL = "http://aura-llama-embed:8081"

type bootSettingsStore = settings.SecretLister

type bootSettingsOpener func(context.Context, string, string) (bootSettingsStore, func(), error)

type embeddingRoute struct {
	embed   config.EmbedConfig
	baseURL string
	model   string
	apiKey  string
}

func loadBootSettings(ctx context.Context) (embeddingRoute, error) {
	return loadBootSettingsWith(
		ctx,
		os.Getenv("AURA_DB_URL"),
		os.Getenv("AURA_AUTHULA_SECRET"),
		openBootSettings,
	)
}

func loadBootSettingsWith(
	ctx context.Context,
	dsn string,
	authulaSecret string,
	open bootSettingsOpener,
) (embeddingRoute, error) {
	if strings.TrimSpace(dsn) == "" {
		return embeddingRoute{}, fmt.Errorf("AURA_DB_URL is required for the settings authority")
	}
	if strings.TrimSpace(authulaSecret) == "" {
		return embeddingRoute{}, fmt.Errorf("AURA_AUTHULA_SECRET is required to read sealed settings")
	}
	store, closeStore, err := open(ctx, dsn, authulaSecret)
	if err != nil {
		return embeddingRoute{}, fmt.Errorf("settings database: %w", err)
	}
	defer closeStore()
	return applyBootSettings(ctx, store)
}

func openBootSettings(ctx context.Context, dsn, authulaSecret string) (bootSettingsStore, func(), error) {
	pool, err := db.Open(ctx, &db.Config{URL: dsn})
	if err != nil {
		return nil, nil, err
	}
	store, err := settings.NewStore(pool, authulaSecret)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("settings store: %w", err)
	}
	return store, pool.Close, nil
}

// applyBootSettings overlays the non-embedding rows (memory bounds, timeouts) onto the
// environment, as the daemon does, and resolves the embedding route from the rows
// themselves: settings.EmbedRoute is the one mapping the ingest supervisor shares, and it
// can see a deleted row where a re-read through the environment could not.
func applyBootSettings(ctx context.Context, store bootSettingsStore) (embeddingRoute, error) {
	if err := settings.OverlayEnv(ctx, store); err != nil {
		return embeddingRoute{}, fmt.Errorf("settings overlay: %w", err)
	}
	embed, key, err := settings.EmbedRoute(ctx, store, os.LookupEnv, defaultMemoryEmbedBaseURL)
	if err != nil {
		return embeddingRoute{}, err
	}
	baseURL, credential, model := config.ResolveEmbedRoute(embed, key)
	return embeddingRoute{embed: embed, baseURL: baseURL, model: model, apiKey: credential}, nil
}
```

Note: `applyBootSettings` runs `OverlayEnv` first, so the environment it then passes to `settings.EmbedRoute` already holds the row values. That is harmless — rows win either way — and keeps the non-embedding keys working exactly as before.

- [ ] **Step 8: Wire the route and log the space in `main.go`**

In `cmd/arcadedb-mcp/main.go`, add `"github.com/chetto1983/aura/internal/config"` and `"github.com/chetto1983/aura/internal/embeddings"` to the imports if absent. Then replace

```go
	embedAPIKey, err := loadBootSettings(context.Background())
	if err != nil {
		return err
	}
```

with

```go
	embedRoute, err := loadBootSettings(context.Background())
	if err != nil {
		return err
	}
```

and replace

```go
	embedRoute := embeddingRouteFromEnv(embedAPIKey)
	embedder := arcadedb.NewSidecarEmbedder(embedRoute.baseURL, embedRoute.model, embedRoute.apiKey, 0)
	if embedder != nil {
		// NOT attached to `client`: that one only ever runs DDL as the admin, and
		// the per-tenant clients the resolver builds get the embedder themselves.
		logger.Info("dense retrieval enabled", "embed_url", embedRoute.baseURL)
	} else {
```

with

```go
	embedder := arcadedb.NewSidecarEmbedder(embedRoute.baseURL, embedRoute.model, embedRoute.apiKey, 0)
	if embedder != nil {
		// NOT attached to `client`: that one only ever runs DDL as the admin, and
		// the per-tenant clients the resolver builds get the embedder themselves.
		// Memory vectors are pinned at the default width (arcadedb vectorDimensions), so
		// that is the width this process's space names.
		space, spaceErr := embeddings.RouteSpace(context.Background(), nil, embedRoute.embed, config.DefaultEmbedDimensions)
		logger.Info("dense retrieval enabled", "embed_url", embedRoute.baseURL,
			"space", space.ID, "space_label", space.Label, "space_error", errString(spaceErr))
	} else {
```

Append to `cmd/arcadedb-mcp/boot_settings.go`:

```go
// errString keeps a failed attestation visible in the boot log without failing boot: the
// local sidecar may still be loading, and the space is informational until plan 2 stamps it.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
```

- [ ] **Step 9: Run the sidecar tests, build, and race**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/arcadedb-mcp/ -run 'BootSettings' -count=1 -v"`
Expected: every `*BootSettings*` test PASS.

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go vet ./... && go build ./... && go test -race ./internal/settings/ ./cmd/arcadedb-mcp/ ./internal/embeddings/ ./internal/config/ -count=1"`
Expected: vet clean, build succeeds, four packages `ok`.

Run: `wsl -e bash -lc "cd /mnt/d/Aura && ~/go/bin/deadcode ./cmd/... 2>&1 | grep -E 'embeddings|settings/embed_route|boot_settings' || echo none"`
Expected: `none` — `SpaceFor`, `RouteSpace`, `AttestLocal` and `EmbedRoute` are all reachable from `cmd/aura` or `cmd/arcadedb-mcp`.

- [ ] **Step 10: Commit**

```bash
git add internal/settings/embed_route.go internal/settings/embed_route_test.go cmd/arcadedb-mcp/boot_settings.go cmd/arcadedb-mcp/boot_settings_test.go cmd/arcadedb-mcp/main.go
git commit -F - <<'EOF'
feat(settings): resolve the embedding route from rows, not from the environment

The memory sidecar read its route by overlaying aura.settings onto its environment and
reading the environment back. OverlayEnv only ever calls Setenv, so a row deleted after
boot would stay in the environment, and any later re-read -- which the next plans need,
for the sidecar's route watcher and for the ingest supervisor's every tick -- could not
see the deletion.

settings.EmbedRoute reads the rows directly: a present row wins even when empty, an
absent one falls back to the environment, and the credential is the sealed
OPENROUTER_API_KEY. The sidecar resolves its route through it and logs the space its
writes will carry.

Four boot tests were replaced rather than edited: they exercised embeddingRouteFromEnv,
which no longer exists. Each guarantee they held -- rows win, fail closed, the secret
never enters the environment, the local default, the store is closed -- is asserted
against the new shape.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

## After the last task

- [ ] Run the unit tier for every touched package once more, alone on the tree:
  `wsl -e bash -lc "cd /mnt/d/Aura && go test -race ./internal/config/ ./internal/embeddings/ ./internal/settings/ ./cmd/arcadedb-mcp/ ./cmd/aura/ -count=1"`
  Expected: five `ok`.
- [ ] Do not push yet: plans 1–5 land as one phase, pushed at its end with CI green (CLAUDE.md §Git push discipline). The E2E on `192.168.101.158` runs after plan 5.
