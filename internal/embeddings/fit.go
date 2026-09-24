package embeddings

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
)

// An input above the model's limit is not truncated by the server: llama.cpp b10951
// refuses it with HTTP 500 and fails every other input in the request with it, and on
// OpenRouter qwen/qwen3-embedding-8b answers HTTP 400 while thenlper/gte-base silently
// keeps 512 tokens (all measured 2026-09-14, prd.md §11). So the client fits each input
// before it leaves, to whatever limit the selected model publishes.
const (
	// specialTokens is the BOS and EOS EmbeddingGemma wraps every input in. Because no
	// token was ever shorter than one UTF-8 byte (3,219 strings measured), an input of
	// limit-specialTokens bytes is guaranteed to fit without asking the tokenizer.
	specialTokens = 2
	// requestTokenBudget bounds one request by tokens as well as by count: a 2,048-token
	// input took 5.7 s on the appliance sidecar, and 32 of them would sit behind one
	// 30-60 s request deadline. A single input larger than the budget still goes alone.
	requestTokenBudget = 4096
)

// inputLimit returns the model's input limit in tokens. It is read from the route's
// catalogue on first use and kept only once read, so a sidecar that was not up yet is
// asked again rather than guessed at.
func (c *Client) inputLimit(ctx context.Context) (int, error) {
	c.limitMu.Lock()
	defer c.limitMu.Unlock()
	if c.limit > 0 {
		return c.limit, nil
	}
	limit, err := c.fetchInputLimit(ctx)
	if err != nil {
		return 0, fmt.Errorf("input limit: %w", err)
	}
	c.limit = limit
	return limit, nil
}

// fetchInputLimit reads the same catalogues the LLM route reads its context window from:
// llama.cpp publishes meta.n_ctx under /v1/models, OpenRouter lists embedding models
// under /v1/embeddings/models and not under /v1/models.
func (c *Client) fetchInputLimit(ctx context.Context) (int, error) {
	provider, catalogue := "llamacpp", serverRoot(c.BaseURL)+"/v1"
	if c.hosted() {
		provider, catalogue = "openrouter", endpoint(c.BaseURL)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	entries, err := llm.FetchModelCatalog(reqCtx, c.httpClient(), provider, catalogue, c.key())
	if err != nil {
		return 0, err
	}
	model := llm.BaseModelID(strings.TrimSpace(c.Model))
	entry, ok := servedModel(entries, model)
	if !ok {
		return 0, fmt.Errorf("catalogue does not serve model %q", model)
	}
	limit := entry.ContextWindow
	if top := entry.TopProviderContextWindow; top > 0 && top < limit {
		limit = top
	}
	if limit <= specialTokens {
		return 0, fmt.Errorf("model %q publishes no usable input limit (%d)", entry.ID, limit)
	}
	return limit, nil
}

// servedModel picks the named model, or the only one a server publishes: the local
// sidecar is addressed without a model name and lists its GGUF path as the id.
func servedModel(entries []llm.ModelCatalogEntry, model string) (llm.ModelCatalogEntry, bool) {
	for _, entry := range entries {
		if entry.ID == model {
			return entry, true
		}
	}
	if len(entries) == 1 {
		return entries[0], true
	}
	return llm.ModelCatalogEntry{}, false
}

// fitInput returns what to send for one text and an upper bound of the tokens it costs.
// A text that fits is sent unchanged, so its vector is byte-for-byte what it was before.
func (c *Client) fitInput(ctx context.Context, text string, limit int) (any, int, error) {
	if len(text)+specialTokens <= limit {
		return text, len(text) + specialTokens, nil
	}
	if c.hosted() {
		// No tokenizer on the cloud route, so the byte bound is the only safe cut. It
		// drops text a truncating provider would have kept (prd.md §11).
		return cutUTF8(text, limit-specialTokens), limit, nil
	}
	ids, err := c.tokenize(ctx, text)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) <= limit {
		return text, len(ids), nil
	}
	// Token IDs rather than detokenized text: /v1/embeddings embeds an ID array as sent,
	// which matched the string input and the detokenized head at cosine 1.0, without a
	// second round trip or a re-tokenization that can land a token or two short.
	head := append(ids[:limit-1:limit-1], ids[len(ids)-1])
	return head, limit, nil
}

type tokenizeRequest struct {
	Content    string `json:"content"`
	AddSpecial bool   `json:"add_special"`
}

type tokenizeResponse struct {
	Tokens []int `json:"tokens"`
}

// tokenize asks the sidecar for the exact IDs /v1/embeddings would embed: both parse
// special tokens, and add_special adds the BOS/EOS the embedding endpoint adds.
func (c *Client) tokenize(ctx context.Context, text string) ([]int, error) {
	var decoded tokenizeResponse
	if err := c.postJSON(ctx, serverRoot(c.BaseURL)+"/tokenize",
		tokenizeRequest{Content: text, AddSpecial: true}, &decoded); err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}
	if len(decoded.Tokens) == 0 {
		return nil, fmt.Errorf("tokenize: no tokens returned")
	}
	return decoded.Tokens, nil
}

// requestEnd returns the end of the request starting at start: at least one input, then
// as many as the count and the token budget both allow.
func requestEnd(costs []int, start, batchSize int) int {
	end, tokens := start+1, costs[start]
	for end < len(costs) && end-start < batchSize && tokens+costs[end] <= requestTokenBudget {
		tokens += costs[end]
		end++
	}
	return end
}

// cutUTF8 returns the longest prefix of text within budget bytes that does not split a rune.
func cutUTF8(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	for budget > 0 && !utf8.RuneStart(text[budget]) {
		budget--
	}
	return text[:budget]
}

// serverRoot strips the OpenAI path from a base URL; llama.cpp serves /tokenize at the root.
func serverRoot(raw string) string {
	base := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(raw), "/"), "/embeddings")
	return strings.TrimSuffix(base, "/v1")
}
