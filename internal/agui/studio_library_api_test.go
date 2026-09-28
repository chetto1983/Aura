package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
)

type fakeStudioLibrary struct {
	owner      string
	before     string
	modalities []assets.Modality
	limit      int
	rows       []assets.Asset
}

func (f *fakeStudioLibrary) ListRecent(
	_ context.Context, identityID, beforeID string, modalities []assets.Modality, limit int,
) ([]assets.Asset, error) {
	f.owner, f.before, f.modalities, f.limit = identityID, beforeID, modalities, limit
	return f.rows, nil
}

// libraryServer wires the library alone, with no StudioBackend: what a daemon with no media
// provider serves.
func libraryServer(t *testing.T, library StudioLibrary) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetStudioLibrary(library)
	return s
}

// The video Studio picks sounds from the library, and clips and pictures together: the kinds
// the request names are the kinds the library is asked for, in that order.
func TestStudioLibraryListsTheKindsAsked(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []assets.Modality
	}{
		{"modality=audio", []assets.Modality{assets.ModalityAudio}},
		{"modality=video&modality=image", []assets.Modality{assets.ModalityVideo, assets.ModalityImage}},
	} {
		library := &fakeStudioLibrary{}
		rec := serveStudio(t, libraryServer(t, library), http.MethodGet, "/api/studio/library?"+tc.query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d (%s), want 200", tc.query, rec.Code, rec.Body.String())
		}
		if !slices.Equal(library.modalities, tc.want) {
			t.Fatalf("%s: library asked for %v, want %v", tc.query, library.modalities, tc.want)
		}
	}
}

// A listing of no kind, or of a kind no picker offers, is the caller's mistake: said, never
// guessed into images.
func TestStudioLibraryRefusesAMissingOrUnknownKind(t *testing.T) {
	for _, query := range []string{"", "?modality=document", "?modality=audio&modality=unknown"} {
		library := &fakeStudioLibrary{}
		rec := serveStudio(t, libraryServer(t, library), http.MethodGet, "/api/studio/library"+query, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status = %d (%s), want 400", query, rec.Code, rec.Body.String())
		}
		if library.owner != "" {
			t.Fatalf("%q: the library was asked anyway", query)
		}
	}
}

func TestStudioLibraryAnswersTheOwnersRowsWithoutTheirStorageKeys(t *testing.T) {
	library := &fakeStudioLibrary{rows: []assets.Asset{{
		ID: "asset-1", IdentityID: studioIdentityID, Modality: assets.ModalityImage,
		FileName: "cat.png", MIMEType: "image/png", SizeBytes: 1024,
		ObjectBucket: "aura-assets", ObjectKey: "identities/alice/secret-object-path.png",
		CreatedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC),
	}}}

	rec := serveStudio(t, libraryServer(t, library), http.MethodGet, "/api/studio/library?modality=image&limit=6&before=asset-0", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if library.owner != studioIdentityID || library.limit != 6 || library.before != "asset-0" ||
		!slices.Equal(library.modalities, []assets.Modality{assets.ModalityImage}) {
		t.Fatalf("library call = %q, before %q, %v, %d", library.owner, library.before, library.modalities, library.limit)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"object_key", "object_bucket", "secret-object-path", "aura-assets"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("library body exposed %q: %s", leaked, body)
		}
	}
	out := decodeStudio[struct {
		Assets []struct {
			ID       string `json:"id"`
			FileName string `json:"file_name"`
			MIMEType string `json:"mime_type"`
		} `json:"assets"`
	}](t, rec)
	if len(out.Assets) != 1 || out.Assets[0].ID != "asset-1" || out.Assets[0].FileName != "cat.png" ||
		out.Assets[0].MIMEType != "image/png" {
		t.Fatalf("library assets = %#v", out.Assets)
	}
}

// The library is served on its own: a daemon with no media provider has no StudioBackend, and the
// video editor's pickers still read it. Unwired it is unavailable, and it is never anonymous.
func TestStudioLibraryNeedsALibraryAndAPrincipalButNoBackend(t *testing.T) {
	const target = "/api/studio/library?modality=audio"
	if rec := serveStudio(t, studioServer(t, &fakeStudioBackend{}), http.MethodGet, target, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("with a backend but no library = %d, want 503", rec.Code)
	}
	library := libraryServer(t, &fakeStudioLibrary{})
	if rec := serveStudio(t, library, http.MethodGet, target, ""); rec.Code != http.StatusOK {
		t.Fatalf("with a library and no backend = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	anonymous := httptest.NewRecorder()
	library.Mux().ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, target, nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("without a principal = %d, want 401", anonymous.Code)
	}
}
