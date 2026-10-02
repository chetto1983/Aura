package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

// The cockpit's own loadProject accepts this file (web/src/videoStudio/__tests__/projectStore.test.ts,
// "loads the project file the server re-keys"), so the Go check and the Studio's parser agree on
// a real project, not on two copies of an assumption.
const studioProjectFixture = "testdata/studio-project.json"

const (
	rekeyOwner     = "owner-1"
	legacyKey      = "chat/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.json"
	rekeyedKey     = "chat/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.aura-video.json"
	legacyFileName = "Reel-di-Aura.json"
)

func studioProjectBytes(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(studioProjectFixture)
	if err != nil {
		t.Fatalf("read %s: %v", studioProjectFixture, err)
	}
	return body
}

type rekeyRig struct {
	rekey   StudioProjectRekey
	rows    *fakeAssetStore
	objects objectstore.Store
}

func newRekeyRig(t *testing.T) rekeyRig {
	t.Helper()
	svc, rows := newAssetServiceTestRig(t, Limits{})
	return rekeyRig{
		rekey: StudioProjectRekey{
			Assets:     svc,
			Files:      &Browser{Objects: svc.Objects, SharedBucket: svc.Bucket, Rows: rows},
			Identities: identityList{rekeyOwner},
		},
		rows:    rows,
		objects: svc.Objects,
	}
}

// studioSave is the row projectStore.ts saveProject left before aura-video-mcp Plan A: a JSON
// document from the cockpit with no thread, named <slug>.json, at chat/<the presign's uuid>.json.
func studioSave(id string, created time.Time) Asset {
	return Asset{
		ID: id, IdentityID: rekeyOwner, SourceKind: SourceWeb, Scope: ScopeThread,
		Modality: ModalityDocument, Status: StatusComplete, MIMEType: "application/json",
		FileName: legacyFileName, ObjectBucket: "asset-test", ObjectKey: legacyKey, CreatedAt: created,
	}
}

// store puts the row and, unless body is nil, its bytes, the way a finalized upload leaves them.
func (r rekeyRig) store(t *testing.T, asset Asset, body []byte) {
	t.Helper()
	if body != nil {
		asset.SizeBytes = int64(len(body))
		if _, err := r.objects.Put(context.Background(), assetRef(asset),
			bytes.NewReader(body), objectstore.PutOptions{Size: asset.SizeBytes}); err != nil {
			t.Fatalf("Put %s: %v", asset.ObjectKey, err)
		}
	}
	r.rows.mu.Lock()
	r.rows.assets[asset.ID] = asset
	r.rows.mu.Unlock()
}

func (r rekeyRig) bytesAt(t *testing.T, bucket, key string) []byte {
	t.Helper()
	body, _, err := r.objects.Get(context.Background(), objectstore.ObjectRef{Bucket: bucket, Key: key})
	if objectstore.IsNotFound(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	defer func() { _ = body.Close() }()
	read, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return read
}

// A project saved before Plan A sits at chat/<uuid>.json, where the ingest indexes it as a
// document; saving it again only adds a new asset. Moved to the Studio's suffix, the old key
// vanishes from the bucket, which is how the ingest drops a document. The row keeps its id, so
// every link to it still loads, and takes the name a save would give it today.
func TestStudioProjectRekeyMovesAnOldStudioProjectOutOfTheIndex(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	rig := newRekeyRig(t)
	project := studioProjectBytes(t)
	rig.store(t, studioSave("asset-project", time.Now()), project)

	moved, err := rig.rekey.Run(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("Run = %d, %v; want 1, nil", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
	if got := rig.bytesAt(t, "asset-test", rekeyedKey); !bytes.Equal(got, project) {
		t.Fatalf("bytes at %s = %q, want the project", rekeyedKey, got)
	}
	if got := rig.bytesAt(t, "asset-test", legacyKey); got != nil {
		t.Fatalf("the old key still holds %d bytes: the ingest would keep it", len(got))
	}
	for _, want := range []string{"re-keyed a Studio project", "asset_id=asset-project", "to=" + rekeyedKey} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}

	moved, err = rig.rekey.Run(context.Background())
	if err != nil || moved != 0 {
		t.Fatalf("second Run = %d, %v; want 0, nil: a moved project is never a candidate again", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
}

// An operator's own JSON is never touched. Each row below differs from a pre-Plan-A Studio save
// in exactly one respect, so each conjunct of the rule is shown to hold on its own.
func TestStudioProjectRekeyLeavesEveryOtherJSONWhereItIs(t *testing.T) {
	notAProject := []byte(`{"id":"settings","name":"export settings","fps":30,"size":{"width":1920,"height":1080}}`)
	for _, tc := range []struct {
		name  string
		edit  func(*Asset)
		bytes func(project []byte) []byte
		// logged: the row looked like a Studio save, so leaving it is a decision worth a line.
		logged bool
	}{
		{name: "attached in a chat thread", edit: func(a *Asset) { a.ThreadID = "thread-1" }},
		{name: "sent from Telegram", edit: func(a *Asset) { a.SourceKind = SourceTelegram }},
		{name: "filed in the library", edit: func(a *Asset) { a.Scope = ScopeLibrary }},
		{name: "not a document", edit: func(a *Asset) { a.Modality = ModalityImage }},
		{name: "not JSON by its type", edit: func(a *Asset) { a.MIMEType = "text/plain" }},
		{name: "a name the Studio never writes", edit: func(a *Asset) { a.FileName = "Reel di Aura.json" }},
		{name: "a key the file manager named", edit: func(a *Asset) { a.ObjectKey = "chat/Reel-di-Aura.json" }},
		{name: "under media/", edit: func(a *Asset) { a.ObjectKey = "media/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.json" }},
		{name: "in another bucket", edit: func(a *Asset) { a.ObjectBucket = "someone-elses" }, logged: true},
		{name: "JSON that is not a project", bytes: func([]byte) []byte { return notAProject }, logged: true},
		{name: "larger than any project", bytes: func(project []byte) []byte {
			return append(project, bytes.Repeat([]byte(" "), studioProjectMaxBytes)...)
		}, logged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t, slog.LevelInfo)
			rig := newRekeyRig(t)
			asset := studioSave("asset-json", time.Now())
			if tc.edit != nil {
				tc.edit(&asset)
			}
			body := studioProjectBytes(t)
			if tc.bytes != nil {
				body = tc.bytes(body)
			}
			rig.store(t, asset, body)

			moved, err := rig.rekey.Run(context.Background())
			if err != nil || moved != 0 {
				t.Fatalf("Run = %d, %v; want 0, nil", moved, err)
			}
			requireRowAt(t, rig.rows, "asset-json", asset.ObjectKey, asset.FileName)
			if got := rig.bytesAt(t, asset.ObjectBucket, asset.ObjectKey); !bytes.Equal(got, body) {
				t.Fatalf("the object at %s changed", asset.ObjectKey)
			}
			if left := strings.Contains(logs.String(), "left a JSON document where it is"); left != tc.logged {
				t.Fatalf("logged the decision = %v, want %v:\n%s", left, tc.logged, logs.String())
			}
		})
	}
}

// The identity's documents are read a page at a time, newest first, and an old project lies
// behind every newer document; stopping at the first page would leave exactly the oldest ones
// indexed.
func TestStudioProjectRekeyReadsPastTheFirstPage(t *testing.T) {
	rig := newRekeyRig(t)
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	rig.store(t, studioSave("asset-project", start), studioProjectBytes(t))
	for i := range 2*rekeyPage + 1 {
		rig.store(t, Asset{
			ID: fmt.Sprintf("asset-pdf-%03d", i), IdentityID: rekeyOwner, SourceKind: SourceWeb,
			Modality: ModalityDocument, Status: StatusComplete, MIMEType: "application/pdf",
			FileName: "fattura.pdf", ObjectBucket: "asset-test", ObjectKey: fmt.Sprintf("chat/fattura-%03d.pdf", i),
			CreatedAt: start.Add(time.Duration(i+1) * time.Minute),
		}, nil)
	}

	moved, err := rig.rekey.Run(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("Run = %d, %v; want 1, nil", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
}

// A project whose bytes cannot be read is named in the error and read again at the next start;
// the projects after it are still moved.
func TestStudioProjectRekeyGoesOnPastAProjectItCannotRead(t *testing.T) {
	rig := newRekeyRig(t)
	lost := studioSave("asset-lost", time.Now())
	lost.ObjectKey = "chat/7e8f9a0b-1c2d-4e3f-8a4b-5c6d7e8f9a0b.json"
	lost.SizeBytes = 512
	rig.store(t, lost, nil)
	rig.store(t, studioSave("asset-project", time.Now().Add(-time.Hour)), studioProjectBytes(t))

	moved, err := rig.rekey.Run(context.Background())
	if moved != 1 || err == nil || !strings.Contains(err.Error(), "asset-lost") {
		t.Fatalf("Run = %d, %v; want 1 and an error naming asset-lost", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
	requireRowAt(t, rig.rows, "asset-lost", lost.ObjectKey, legacyFileName)

	rig.rekey.Identities = failingIdentities{err: errors.New("identity store down")}
	if _, err := rig.rekey.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "identity store down") {
		t.Fatalf("Run with no identities = %v, want the lister's error", err)
	}
}

// The content check is the conjunct no other JSON passes: the Studio's container shape, typed,
// with every clip naming a source it can draw. A file the Studio's own parser would refuse is not
// moved either way; refused here, it simply stays where it was.
func TestIsStudioProjectRecognisesOnlyTheStudiosShape(t *testing.T) {
	project := string(studioProjectBytes(t))
	if !isStudioProject([]byte(project)) {
		t.Fatal("the Studio's own project was refused")
	}
	for name, body := range map[string]string{
		"not JSON":        `{"id":`,
		"an array":        `[` + project + `]`,
		"an empty object": `{}`,
		"a numeric id":    strings.Replace(project, `"id": "6f1c2a9e-4b7d-4e2a-9c51-0d3e8f7a1b24"`, `"id": 7`, 1),
		"no frame rate":   strings.Replace(project, `"fps": 30`, `"fps": 0`, 1),
		// The project's own frame is the first size in the file, ahead of every source's.
		"a frame with no height":     strings.Replace(project, `"width": 1920, "height": 1080`, `"width": 1920`, 1),
		"sources that are null":      strings.Replace(project, `"sources": [`, `"sources": null, "unused": [`, 1),
		"no overlays lane list":      strings.Replace(project, `"overlays": [`, `"lanes": [`, 1),
		"a source of no known kind":  strings.Replace(project, `"kind": "audio"`, `"kind": "midi"`, 1),
		"a clip of a missing source": strings.Replace(project, `"sourceId": "src-still"`, `"sourceId": "src-gone"`, 1),
		"a clip playing a sound":     strings.Replace(project, `"sourceId": "src-still"`, `"sourceId": "src-music"`, 1),
		"a clip of no length":        strings.Replace(project, `"duration": 4,`, `"duration": 0,`, 1),
		"a clip with no id":          strings.Replace(project, `"id": "clip-2",`, ``, 1),
	} {
		if body == project {
			t.Fatalf("%s: the fixture no longer contains the text this case rewrites", name)
		}
		if isStudioProject([]byte(body)) {
			t.Errorf("%s: accepted as a Studio project", name)
		}
	}
}
