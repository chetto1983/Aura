package embeddings

import "strings"

// EmbeddingGemma's retrieval prefixes are asymmetric and part of the model input:
// https://ai.google.dev/gemma/docs/embeddinggemma/model_card.
const (
	// QueryPrefix selects retrieval-query embeddings.
	QueryPrefix = "task: search result | query: "
	// UntitledDocumentPrefix selects stored retrieval embeddings without a title.
	UntitledDocumentPrefix = "title: none | text: "
)

// Prefix returns a prefixed copy and never mutates the caller's slice.
func Prefix(prefix string, texts []string) []string {
	out := make([]string, len(texts))
	for index, value := range texts {
		out[index] = prefix + value
	}
	return out
}

// RetrievalQueries formats query-side EmbeddingGemma retrieval inputs.
func RetrievalQueries(queries []string) []string {
	return Prefix(QueryPrefix, queries)
}

// RetrievalDocuments formats stored retrieval inputs with a caller-bounded title.
func RetrievalDocuments(title string, texts []string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "none"
	}
	return Prefix("title: "+title+" | text: ", texts)
}

// RecipeVersion covers everything that turns the same text into a different stored vector
// without changing a model name: the prefixes above and their Python twin
// (services/ingest/chunk.py EMBED_DOC_PREFIX), llama.cpp's --embd-normalize in compose.yaml,
// and TruncateMRL. Bump it when any of them changes. It is part of every embedding space
// (space.go), so vectors stored under the old recipe stop matching the current space and
// are re-embedded.
const RecipeVersion = 1
