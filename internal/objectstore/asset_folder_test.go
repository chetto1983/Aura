package objectstore

import (
	"strings"
	"testing"
)

// Every upload used to land in one folder, `chat/`: a PDF a person wrote, a screenshot they
// pasted and a clip a model generated shared a tree that says nothing about any of them. The
// file manager shows this bucket, so the operator saw one bag of ids. Pictures and clips are
// browsed, reused and edited as media, so they get their own folder; documents keep theirs,
// because that is where the index sweep and the file cards already look.
func TestAssetFolderSeparatesMediaFromDocuments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		folder AssetFolder
		want   string
	}{
		{name: "an image", folder: FolderMedia, want: "media/"},
		{name: "a clip", folder: FolderMedia, want: "media/"},
		{name: "a document", folder: FolderChat, want: "chat/"},
		{name: "an unset folder falls back to the documents tree", folder: "", want: "chat/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			place := PlaceAsset("019f8a2b-0000-7000-8000-000000000001", "clip.mp4", tc.folder)
			if !strings.HasPrefix(place.Key, tc.want) {
				t.Fatalf("key = %q, want the %q folder", place.Key, tc.want)
			}
			if !strings.HasSuffix(place.Key, ".mp4") {
				t.Fatalf("key = %q lost the extension the extractor routes on", place.Key)
			}
			if strings.Contains(place.Key, "clip") {
				t.Fatalf("key = %q carries the filename, which a presigned URL would leak", place.Key)
			}
		})
	}
}

// A folder is a fixed set, not a caller's string: these keys reach presigned URLs and the
// file manager's reserved-path check, so an upload path must not be able to invent a tree.
func TestAssetFolderRefusesAnythingButItsOwnValues(t *testing.T) {
	place := PlaceAsset("019f8a2b-0000-7000-8000-000000000002", "note.pdf", AssetFolder("../../etc/"))
	if !strings.HasPrefix(place.Key, "chat/") {
		t.Fatalf("key = %q, want an unknown folder to fall back to chat/", place.Key)
	}
}
