package memoryindex

import (
	"crypto/sha256"
	"encoding/hex"
)

// ContentHash returns the sha256 hex digest of role + body + embeddingModelID.
// Matches the Mem0 v3 pattern: text identity includes the embedding model so a
// model change forces re-embedding even when the text is unchanged.
func ContentHash(role, body, embeddingModelID string) string {
	h := sha256.New()
	h.Write([]byte(role))
	h.Write([]byte("\n"))
	h.Write([]byte(body))
	h.Write([]byte("\n"))
	h.Write([]byte(embeddingModelID))
	return hex.EncodeToString(h.Sum(nil))
}
