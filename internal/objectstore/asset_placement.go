package objectstore

import (
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

// MetadataFileName is the user-metadata entry carrying an uploaded asset's real filename.
// The wire form is "x-amz-meta-filename"; every S3 client lowercases and strips that prefix,
// so this bare key is what both sides see. services/ingest/app.py reads the same one.
const MetadataFileName = "filename"

// maxFileNameMetadataBytes bounds the ENCODED value. S3 budgets all user metadata together
// (2 KiB), and an overflow fails the PUT -- which would turn a long filename into a refused
// document rather than an ugly one. Percent-encoding costs 3 bytes per non-ASCII byte, so a
// 1 KiB cap still carries roughly 340 accented characters.
const maxFileNameMetadataBytes = 1024

// AssetPlacement is where an uploaded asset goes and what travels with it.
type AssetPlacement struct {
	Key      string
	Metadata map[string]string
}

// PlaceAsset decides both halves of an upload at once: the key, which deliberately does NOT
// carry the filename, and the metadata, which does.
//
// The split is the whole design. A key travels into presigned URLs, S3 access logs and error
// strings, so "Quarterly Secrets.pdf" in a key leaks the document's subject to everyone who
// sees a link -- the reason TestServicePresignNeverPutsFilenameInObjectKey exists. But the
// name is also what a person recognises a document by, and with nowhere to ride it reached
// the index as "<assetID>.pdf": the ingest sidecar derived a name from the key, so the
// operator's own filename was searchable nowhere. User metadata is the channel that was
// missing -- it reaches the object, not the URL.
//
// Returned together, not as two functions, because a caller that builds a key without the
// metadata fails silently: the object lands, the row lands, only the name is wrong.
func PlaceAsset(assetID, fileName string) AssetPlacement {
	return AssetPlacement{
		Key:      AssetKey(assetID, fileName),
		Metadata: map[string]string{MetadataFileName: encodeFileName(fileName)},
	}
}

// AssetKey places an uploaded file where its owner can see it and the reconciler can read
// it: "chat/<assetID><.ext>" in the identity's own bucket.
//
// It used to be "identity/<id>/asset/<id>/original", and every part of that has stopped
// earning its place:
//
//   - The identity segment was the isolation. It is not any more — each identity has its
//     own bucket, so a key cannot address another's store at all, and repeating the id
//     inside a bucket that belongs to that id says nothing.
//   - "identity/" is excluded from the ingest sweep and hidden from the file manager, so an
//     attachment landing there was invisible AND unindexed: uploading a document in chat
//     and then asking about it found nothing.
//   - "original" has no extension. The extractor routes on it, so a .docx arriving as
//     "original" was fed to Tika as an unknown type and threw — the errors that made a
//     working pipeline look broken.
//
// The EXTENSION is carried and the name is NOT: ".pdf" leaks nothing and is exactly what the
// extractor routes on. The name rides in metadata instead — see PlaceAsset, which is what
// callers should use.
func AssetKey(assetID, fileName string) string {
	return "chat/" + assetID + assetExtension(fileName)
}

// assetExtension returns a lowercase ".ext", or "" when the name has none.
//
// Bounded and character-checked because it is caller-supplied: a chat client or a Telegram
// message can send any name, and this fragment becomes part of an object key.
func assetExtension(name string) string {
	ext := strings.ToLower(path.Ext(baseName(name)))
	if len(ext) < 2 || len(ext) > 12 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

func baseName(name string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
}

// encodeFileName renders a name as printable ASCII that survives an HTTP header.
//
// S3 user metadata IS a header, so it is US-ASCII: measured against the running Garage on
// 2026-08-13, "Perizia città di Ghèdi.pdf" is refused by the protocol itself, and Italian
// filenames carry accents as a matter of course. url.PathEscape is the encoder because
// Python's urllib.parse.unquote is its exact inverse -- the sidecar needs no code of ours to
// read this. It never emits '+', which unquote would leave alone and unquote_plus would turn
// into a space; that ambiguity is why QueryEscape is not used here.
func encodeFileName(name string) string {
	name = baseName(name)
	// Truncate on the DECODED name so a cut never lands inside a rune or inside a percent
	// escape, and keep the extension: the extractor routes on it, so losing it costs more
	// than losing the tail of a long title.
	ext := path.Ext(name)
	if len(ext) > maxFileNameMetadataBytes/4 {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for len(url.PathEscape(stem+ext)) > maxFileNameMetadataBytes {
		if stem == "" {
			return ""
		}
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return url.PathEscape(stem + ext)
}

// DecodeFileName reverses encodeFileName and returns "" for anything it cannot vouch for.
//
// Empty is the honest answer for an object uploaded before this channel existed -- there are
// such objects in the bucket -- so the caller falls back to the key-derived name instead of
// being handed an invented one. It is also the answer for a value that decodes to a path:
// this name is used to build temp files and is shown to the operator, so a "../" that came
// from an object anyone could have written must not travel any further.
func DecodeFileName(encoded string) string {
	decoded, err := url.PathUnescape(strings.TrimSpace(encoded))
	if err != nil {
		return ""
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" || decoded == "." || decoded == ".." {
		return ""
	}
	if strings.ContainsAny(decoded, "/\\\x00") {
		return ""
	}
	return decoded
}
