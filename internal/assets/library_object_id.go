package assets

import (
	"path"
	"strings"

	"github.com/google/uuid"
)

// libraryObjectNamespace seeds the v5 UUIDs below. It is a constant of this codebase, not a
// secret: it only keeps these ids from colliding with any other uuid space.
var libraryObjectNamespace = uuid.MustParse("6f3c1f52-9c1a-4c8e-9a4d-7d5f0c2b8e41")

// libraryObjectID keeps every ingest of one document on ONE object key.
//
// The key is the only handle the reconciler has: CocoIndex re-reads an object whose bytes
// changed and rewrites its passages under the same document id, but it cannot know that two
// keys are two versions of one file. Measured 2026-09-09 on the live stack -- overwriting
// Documenti/realign-garage.md in place moved its indexed sha256 from e32010de to f1286a31 and
// made the superseded text unfindable, while the same file re-ingested through this path
// landed at a fresh chat/<uuid> and left version one answering searches for good.
//
// A v5 UUID rather than the name itself, because AssetKey deliberately keeps file names out
// of object keys -- they travel into presigned URLs and access logs. A digest is stable
// without being readable.
//
// SourceRef wins when the caller has one: it is documented as a stable locator, so two files
// that happen to share a name stay distinct. The name is the fallback, which makes a library
// behave the way its folder does -- ingesting report.pdf twice replaces report.pdf.
// objectAssetID is the ONE place that decides whether an upload keeps its object or gets a
// new one. Chat, Telegram and the document tools all feed the same library, so the rule
// cannot live in one ingress: a thread attachment is a new artefact each time, anything
// scoped to the library is a document that replaces its previous self.
func objectAssetID(scope Scope, identityID, sourceRef, fileName string) string {
	if scope != ScopeLibrary {
		return newAssetID()
	}
	return libraryObjectID(identityID, sourceRef, fileName)
}

func libraryObjectID(identityID, sourceRef, fileName string) string {
	seed := strings.TrimSpace(sourceRef)
	if seed == "" {
		seed = strings.ToLower(path.Base(strings.ReplaceAll(strings.TrimSpace(fileName), `\`, "/")))
	}
	if seed == "" {
		return newAssetID()
	}
	return uuid.NewSHA1(libraryObjectNamespace, []byte(identityID+"\x00"+seed)).String()
}
