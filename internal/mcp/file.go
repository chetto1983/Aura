package mcp

import (
	"fmt"
	"net/url"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// file.go names the binary content a tool result can carry. A server hands a file
// back inline (an image, audio or embedded-resource block) or as a resource_link the
// client reads back on the same session. Either way it becomes a FilePart, and the
// bridge turns each FilePart into a FileOutcome: a path in the agent's workspace, or
// the reason there is none. The bytes never reach the model.

const (
	// MaxFileBytes caps one file: Gmail's attachment limit, and far below the aura
	// container's 768 MiB even after base64 inflation on the wire.
	MaxFileBytes = 25 << 20
	// MaxCallFileBytes caps the files of one tool call together.
	MaxCallFileBytes = 50 << 20
	// OctetStream is the MIME type that says nothing about a file.
	OctetStream = "application/octet-stream"
)

// FilePart is one file a tool result carried. Unavailable says why its bytes could
// not be obtained; Data is then empty, and Size — a link's advertised size, or a
// measured length over the cap — is what stands in for it.
type FilePart struct {
	Name        string
	MIMEType    string
	Data        []byte
	Size        int64
	Unavailable string
}

// FileOutcome is what became of one FilePart, as the model reads it: the workspace
// path it was written to, or why it was not.
type FileOutcome struct {
	Path            string `json:"path,omitempty"`
	Name            string `json:"name"`
	MIMEType        string `json:"mime_type,omitempty"`
	SizeBytes       int64  `json:"size_bytes"`
	SHA256          string `json:"sha256,omitempty"`
	NotMaterialized string `json:"not_materialized,omitempty"`
}

// NotMaterialized is p's outcome when it was not written: for reason, or for the
// reason its bytes were unavailable in the first place.
func (p FilePart) NotMaterialized(reason string) FileOutcome {
	if p.Unavailable != "" {
		reason = p.Unavailable
	}
	size := int64(len(p.Data))
	if size == 0 {
		size = p.Size
	}
	return FileOutcome{Name: p.Name, MIMEType: p.MIMEType, SizeBytes: size, NotMaterialized: reason}
}

// FileCapExceeded is the reason a file over MaxFileBytes is refused.
func FileCapExceeded(size int64) string {
	return fmt.Sprintf("%d bytes exceeds the %d-byte file cap", size, MaxFileBytes)
}

// FileFromContents is one resource's contents as a FilePart: a non-empty blob as its
// bytes, else non-empty text as UTF-8 — a blob wins when both are present. False when
// the contents carry neither (including a present-but-empty blob or text).
func FileFromContents(rc *sdkmcp.ResourceContents) (FilePart, bool) {
	switch {
	case rc == nil:
		return FilePart{}, false
	case len(rc.Blob) > 0:
		return FilePart{Name: NameFromURI(rc.URI), MIMEType: rc.MIMEType, Data: rc.Blob}, true
	case rc.Text != "":
		return FilePart{Name: NameFromURI(rc.URI), MIMEType: rc.MIMEType, Data: []byte(rc.Text)}, true
	default:
		return FilePart{}, false
	}
}

// NameFromURI is the last path segment of uri, percent-decoded, the name a file gets
// when its server gave none: attachment://stash/abc123 names abc123. A segment that
// fails to decode (a malformed escape) is returned raw rather than dropped.
func NameFromURI(uri string) string {
	rest := uri
	if _, after, ok := strings.Cut(rest, "://"); ok {
		rest = after
	}
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimRight(rest, "/")
	seg := rest[strings.LastIndex(rest, "/")+1:]
	if decoded, err := url.PathUnescape(seg); err == nil {
		return decoded
	}
	return seg
}
