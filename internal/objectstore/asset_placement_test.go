package objectstore

import (
	"net/url"
	"strings"
	"testing"
)

// The name is what a person recognises a document by, and until now it reached the index
// as "<assetID>.pdf": AssetKey deliberately leaves the name out of the key -- a key travels
// into presigned URLs and access logs, so "Quarterly Secrets.pdf" there leaks the subject --
// and nothing else carried it, so the ingest sidecar derived a name from the key and the
// operator's own filename was searchable nowhere.
//
// PlaceAsset returns the key and its metadata TOGETHER because they are one decision. Two
// separate calls is one a third upload path can forget, and forgetting is silent: the object
// lands, the row lands, and only the name is wrong.
func TestPlaceAssetCarriesTheNameBesideTheKeyNotInsideIt(t *testing.T) {
	place := PlaceAsset("019f8a2b-0000-7000-8000-000000000001", "Contratto ACME 2026.pdf")

	if place.Key != "chat/019f8a2b-0000-7000-8000-000000000001.pdf" {
		t.Fatalf("key = %q", place.Key)
	}
	if strings.Contains(strings.ToLower(place.Key), "contratto") {
		t.Fatalf("the name leaked into the key: %q", place.Key)
	}
	if got := place.Metadata[MetadataFileName]; got != "Contratto%20ACME%202026.pdf" {
		t.Fatalf("metadata name = %q", got)
	}
	if got := DecodeFileName(place.Metadata[MetadataFileName]); got != "Contratto ACME 2026.pdf" {
		t.Fatalf("round trip = %q", got)
	}
}

// S3 user metadata is an HTTP header, so it is US-ASCII: measured against the running Garage
// on 2026-08-13, "Perizia città di Ghèdi.pdf" is refused outright by the protocol. Italian
// filenames carry accents as a matter of course, so an encoding is the contract, not a
// nicety -- and it has to survive the round trip, which is what the sidecar undoes.
func TestPlaceAssetEncodesNamesS3CannotCarry(t *testing.T) {
	for _, name := range []string{
		"Perizia città di Ghèdi — 2026.pdf",
		"Отчёт.pdf",
		"Relazione (bozza) - v2 [final].pdf",
		"già+così&poi=fine.txt",
		`"virgolette" e 'apici'.md`,
	} {
		t.Run(name, func(t *testing.T) {
			encoded := PlaceAsset("id", name).Metadata[MetadataFileName]
			for i := 0; i < len(encoded); i++ {
				if encoded[i] > 0x7e || encoded[i] < 0x20 {
					t.Fatalf("byte %d of %q is not printable ASCII", i, encoded)
				}
			}
			if got := DecodeFileName(encoded); got != name {
				t.Fatalf("round trip: %q -> %q", name, got)
			}
		})
	}
}

// A metadata value that overflows the store's header budget fails the UPLOAD, which would
// turn a long filename into a refused document. The name is the expendable part -- the bytes
// are not -- so it is truncated on a rune boundary and the extension is kept, because the
// extension is what the extractor routes on.
func TestPlaceAssetBoundsTheNameSoAnUploadCannotFailOnIt(t *testing.T) {
	long := strings.Repeat("à", 2000) + ".pdf"
	encoded := PlaceAsset("id", long).Metadata[MetadataFileName]

	if len(encoded) > maxFileNameMetadataBytes {
		t.Fatalf("encoded name is %d bytes, over the %d cap", len(encoded), maxFileNameMetadataBytes)
	}
	decoded := DecodeFileName(encoded)
	if !strings.HasSuffix(decoded, ".pdf") {
		t.Fatalf("truncation dropped the extension: %q", decoded)
	}
	if !strings.HasPrefix(decoded, "àà") {
		t.Fatalf("truncated to something unrecognisable: %q", decoded)
	}
	if strings.ContainsRune(decoded, '�') {
		t.Fatalf("truncation split a rune: %q", decoded)
	}
}

// An object uploaded before this channel existed has no metadata at all, and there are such
// objects in the bucket today. The reader must say "nothing here" rather than invent a name,
// so the caller can fall back to the key exactly as it does now.
func TestDecodeFileNameRefusesWhatItCannotTrust(t *testing.T) {
	for name, value := range map[string]string{
		"absent":            "",
		"blank":             "   ",
		"undecodable":       "%zz%zz",
		"path traversal":    "..%2F..%2Fetc%2Fpasswd",
		"absolute path":     "%2Fetc%2Fpasswd",
		"bare separator":    "%2F",
		"nul byte":          "a%00b.pdf",
		"only an extension": ".",
	} {
		t.Run(name, func(t *testing.T) {
			if got := DecodeFileName(value); got != "" {
				t.Fatalf("accepted %q as the name %q", value, got)
			}
		})
	}
}

// The encoder is url.PathEscape and the decoder is its exact inverse on the Python side
// (urllib.parse.unquote). This pins the property the two languages have to agree on, so a
// change to either is caught here rather than by a wrong name in the index.
func TestEncodedNameIsPlainPercentEncoding(t *testing.T) {
	name := "Perizia città (bozza).pdf"
	encoded := PlaceAsset("id", name).Metadata[MetadataFileName]
	if encoded != url.PathEscape(name) {
		t.Fatalf("encoding drifted from url.PathEscape: %q", encoded)
	}
	if strings.Contains(encoded, "+") {
		t.Fatal("a '+' would decode as a space under unquote_plus and as '+' under unquote")
	}
}
