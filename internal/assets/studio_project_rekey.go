package assets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/objectstore"
)

// A Video Studio project saved before aura-video-mcp Plan A sits at chat/<uuid>.json, and the
// ingest sidecar indexes it as a document: its matcher reads only the object key, and that key
// carried no sign of the Studio until objectstore.StudioProjectSuffix. Saving again does not
// help, because every save is a new asset. StudioProjectRekey moves each such project to
// chat/<uuid>.aura-video.json through the file manager's own relocation (Browser.transfer), and
// the ingest then drops it as a key that vanished.
//
// Only a row that passes every test below is touched, and no one test would do on its own: an
// operator can attach a .json from the same cockpit to a chat that has no thread yet. The bytes
// are what no other file passes -- the Studio's saved shape, typed, with every clip naming a
// source it can draw.

const (
	rekeyPage = 100
	// studioProjectMaxBytes bounds the one read the check makes. A project is the editor's state,
	// well under a kilobyte per clip, so a JSON document past 4 MiB is not one.
	studioProjectMaxBytes = 4 << 20
)

var (
	// legacyStudioProjectKey is the key Presign gave a project: chat/, then the uuid it minted
	// for the key (not the row's id), then the one extension assetExtension kept.
	legacyStudioProjectKey = regexp.MustCompile(
		`^chat/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$`)
	// legacyStudioProjectName is what projectFileName wrote: a slug of letters, digits and
	// hyphens, at most 60 of them, or the project's own uuid, then .json.
	legacyStudioProjectName = regexp.MustCompile(`^[A-Za-z0-9-]{1,60}\.json$`)
)

// StudioProjectRekey moves the Studio projects saved before Plan A out of the document index.
type StudioProjectRekey struct {
	Assets     *Service
	Files      *Browser
	Identities IdentityLister
}

// Run re-keys every identity's old Studio projects and returns how many it moved. A moved
// project no longer matches, so it is never a candidate again. What it could not move is named
// in the joined error and tried again on the next run, and a candidate it refused is read again,
// and logged again, on every run.
func (r StudioProjectRekey) Run(ctx context.Context) (int, error) {
	identities, err := r.Identities.IdentityIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("studio project re-key: list identities: %w", err)
	}
	moved := 0
	var errs []error
	for _, identityID := range identities {
		n, err := r.rekeyIdentity(ctx, identityID)
		moved += n
		if err != nil {
			errs = append(errs, fmt.Errorf("studio project re-key of %s: %w", identityID, err))
		}
	}
	return moved, errors.Join(errs...)
}

func (r StudioProjectRekey) rekeyIdentity(ctx context.Context, identityID string) (int, error) {
	moved := 0
	var errs []error
	for before := ""; ; {
		page, err := r.Assets.Store.ListRecent(ctx, identityID, before, []Modality{ModalityDocument}, rekeyPage)
		if err != nil {
			return moved, errors.Join(append(errs, fmt.Errorf("list documents: %w", err))...)
		}
		for _, asset := range page {
			if !savedLikeAStudioProject(asset) {
				continue
			}
			ok, err := r.rekey(ctx, identityID, asset)
			if err != nil {
				errs = append(errs, fmt.Errorf("asset %s at %s: %w", asset.ID, asset.ObjectKey, err))
			}
			if ok {
				moved++
			}
		}
		if len(page) < rekeyPage {
			return moved, errors.Join(errs...)
		}
		before = page[len(page)-1].ID
	}
}

// savedLikeAStudioProject is what web/src/videoStudio/projectStore.ts saveProject left before
// Plan A: a JSON document from the cockpit (ListRecent is asked for documents only), with no
// thread and no library scope, named <slug>.json, at a key of the presign's own uuid.
func savedLikeAStudioProject(asset Asset) bool {
	return asset.SourceKind == SourceWeb && asset.ThreadID == "" && asset.Scope == ScopeThread &&
		asset.MIMEType == "application/json" &&
		legacyStudioProjectName.MatchString(asset.FileName) &&
		legacyStudioProjectKey.MatchString(asset.ObjectKey)
}

// rekey moves one candidate when its bucket, size and bytes say it is a Studio project, and logs
// the decision either way.
func (r StudioProjectRekey) rekey(ctx context.Context, identityID string, asset Asset) (bool, error) {
	store, bucket, err := r.Files.resolveStore(ctx, identityID)
	if err != nil {
		return false, err
	}
	refusal := ""
	switch {
	case asset.ObjectBucket != bucket:
		refusal = "it lives in another bucket than the file manager's"
	case asset.SizeBytes > studioProjectMaxBytes:
		refusal = "it is larger than any Studio project"
	default:
		project, err := readAtMost(ctx, store, objectstore.ObjectRef{Bucket: bucket, Key: asset.ObjectKey})
		if err != nil {
			return false, err
		}
		if !isStudioProject(project) {
			refusal = "its bytes are not a Studio project"
		}
	}
	if refusal != "" {
		slog.Info("aura assets: left a JSON document where it is", "asset_id", asset.ID, "key", asset.ObjectKey, "reason", refusal)
		return false, nil
	}
	to := strings.TrimSuffix(asset.ObjectKey, ".json") + objectstore.StudioProjectSuffix
	name := strings.TrimSuffix(asset.FileName, ".json") + objectstore.StudioProjectSuffix
	if _, err := r.Files.transfer(ctx, identityID, asset.ObjectKey, to, true, name); err != nil {
		return false, err
	}
	slog.Info("aura assets: re-keyed a Studio project out of the document index", "asset_id", asset.ID, "from", asset.ObjectKey, "to", to)
	return true, nil
}

// readAtMost reads at most studioProjectMaxBytes of the object. The cap the rule enforces is the
// row's size_bytes; an object longer than its row says is cut here, and a cut that falls inside
// the JSON fails the content check.
func readAtMost(ctx context.Context, store objectstore.Store, ref objectstore.ObjectRef) ([]byte, error) {
	body, _, err := store.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(io.LimitReader(body, studioProjectMaxBytes))
}

// studioProjectFile is the part of the Studio's saved shape (projectStore.ts hasProjectShape and
// referencesHold) that tells a project from any other JSON. A pointer or a slice pointer is
// required: nil means the field is missing or null, and a field of the wrong JSON type fails the
// decode.
type studioProjectFile struct {
	ID       *string            `json:"id"`
	Name     *string            `json:"name"`
	FPS      *float64           `json:"fps"`
	Size     *studioFrame       `json:"size"`
	Sources  *[]studioSource    `json:"sources"`
	Video    *[]studioClip      `json:"video"`
	Overlays *[]json.RawMessage `json:"overlays"`
}

type studioFrame struct {
	Width  *float64 `json:"width"`
	Height *float64 `json:"height"`
}

type studioSource struct {
	ID      *string `json:"id"`
	AssetID *string `json:"assetId"`
	Kind    string  `json:"kind"`
}

type studioClip struct {
	ID       *string  `json:"id"`
	SourceID *string  `json:"sourceId"`
	Duration *float64 `json:"duration"`
}

func isStudioProject(raw []byte) bool {
	var file studioProjectFile
	if json.Unmarshal(raw, &file) != nil {
		return false
	}
	if file.ID == nil || file.Name == nil || !positive(file.FPS) || file.Size == nil ||
		!positive(file.Size.Width) || !positive(file.Size.Height) ||
		file.Sources == nil || file.Video == nil || file.Overlays == nil {
		return false
	}
	kinds := make(map[string]string, len(*file.Sources))
	for _, source := range *file.Sources {
		if source.ID == nil || source.AssetID == nil ||
			(source.Kind != "video" && source.Kind != "image" && source.Kind != "audio") {
			return false
		}
		kinds[*source.ID] = source.Kind
	}
	for _, clip := range *file.Video {
		if clip.ID == nil || clip.SourceID == nil || !positive(clip.Duration) {
			return false
		}
		if kind := kinds[*clip.SourceID]; kind != "video" && kind != "image" {
			return false
		}
	}
	return true
}

func positive(value *float64) bool {
	return value != nil && *value > 0
}
