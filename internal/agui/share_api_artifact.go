package agui

import (
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/share"
)

// share_api_artifact.go serves one bundled artifact of an already-resolved share, for both the
// public token tier and the D-10 internal tier. The tier decides only how the snapshot was
// resolved; what a resolved snapshot may serve is the same rule on both.
//
// assetID must belong to THAT snapshot (SC4 row 9) or the answer is the tiers' shared 404 —
// holding a link authenticates one snapshot, never any asset id. Bytes are read only from the
// token-scoped share/ object-store namespace, never through the identity-scoped artifact API
// (D-09 copy-never-reference).

// serveShareArtifact is the inert download (37A D-10): the artifact's real MIME is never trusted
// as a serve header, exactly as handleAssetDownload serves an owner's asset.
func (s *Server) serveShareArtifact(w http.ResponseWriter, r *http.Request, snap share.Snapshot, link share.Link, assetID string) {
	artifact, ok := snapshotArtifact(snap, assetID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rc, err := s.share.OpenArtifact(r.Context(), link.ID, link.SnapshotID, assetID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()
	setAttachmentHeaders(w.Header(), artifact.FileName, artifact.SizeBytes)
	_, _ = io.Copy(w, rc)
}

// streamShareArtifact is the Range-capable inline stream of a bundled video, under the same
// rules as /api/assets/{id}/stream. The snapshot already tells its holder every artifact's MIME,
// so answering a non-video with 415 rather than 404 reveals nothing new.
func (s *Server) streamShareArtifact(w http.ResponseWriter, r *http.Request, snap share.Snapshot, link share.Link, assetID string) {
	artifact, ok := snapshotArtifact(snap, assetID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, err := s.share.OpenArtifactSeekable(r.Context(), link.ID, link.SnapshotID, assetID, artifact.SizeBytes)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer func() { _ = body.Close() }()
	serveVideoStream(w, r, body, artifact.MIMEType, artifact.FileName)
}

func snapshotArtifact(snap share.Snapshot, assetID string) (share.SnapshotArtifact, bool) {
	for _, artifact := range snap.Artifacts {
		if artifact.AssetID == assetID {
			return artifact, true
		}
	}
	return share.SnapshotArtifact{}, false
}
