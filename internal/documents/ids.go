package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// SearchDocumentID is stable across content versions and owner-scoped by
// construction. It deliberately contains no source locator or identity text.
func SearchDocumentID(identityID, sourceKind, sourceKey string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	sourceKind = strings.ToLower(strings.TrimSpace(sourceKind))
	sourceKey = strings.TrimSpace(sourceKey)
	if identityID == "" || sourceKind == "" || sourceKey == "" {
		return "", fmt.Errorf("document identity, source kind, and source key are required")
	}
	h := sha256.New()
	for _, part := range []string{"aura.document.search.v1", identityID, sourceKind, sourceKey} {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return "doc_" + hex.EncodeToString(h.Sum(nil))[:32], nil
}
