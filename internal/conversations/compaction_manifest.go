package conversations

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

const (
	manifestDigestAlgorithm = "sha256"
	manifestDigestVersion   = 1
)

// ManifestTurn is the immutable, digest-covered projection of one canonical turn.
type ManifestTurn struct {
	Seq        int    `json:"seq"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// VersionedCompactionManifest partitions every captured turn exactly once.
type VersionedCompactionManifest struct {
	CapturedWatermarkSeq  int            `json:"captured_watermark_seq"`
	SummarizedTurnSeqs    []int          `json:"summarized_turn_seqs"`
	TailTurnSeqs          []int          `json:"tail_turn_seqs"`
	ProtectedTurnSeqs     []int          `json:"protected_turn_seqs"`
	ExcludedTurnSeqs      []int          `json:"excluded_turn_seqs"`
	Turns                 []ManifestTurn `json:"turns"`
	DigestAlgorithm       string         `json:"digest_algorithm"`
	DigestVersion         int            `json:"digest_version"`
	CompleteCaptureDigest string         `json:"complete_capture_digest"`
	Digest                string         `json:"-"`
}

// NewCompactionManifest returns a version-one immutable manifest and its digests.
func NewCompactionManifest(watermark int, summarized, tail, protected, excluded []int, turns []ManifestTurn) VersionedCompactionManifest {
	m := VersionedCompactionManifest{CapturedWatermarkSeq: watermark, SummarizedTurnSeqs: cloneSorted(summarized), TailTurnSeqs: cloneSorted(tail), ProtectedTurnSeqs: cloneSorted(protected), ExcludedTurnSeqs: cloneSorted(excluded), Turns: append([]ManifestTurn(nil), turns...), DigestAlgorithm: manifestDigestAlgorithm, DigestVersion: manifestDigestVersion}
	sort.Slice(m.Turns, func(i, j int) bool { return m.Turns[i].Seq < m.Turns[j].Seq })
	turnBytes, _ := canonicaljson.Marshal(m.Turns)
	m.CompleteCaptureDigest = sha256Hex(turnBytes)
	manifestBytes, _ := m.CanonicalBytes()
	m.Digest = sha256Hex(manifestBytes)
	return m
}

func cloneSorted(v []int) []int { out := append([]int(nil), v...); sort.Ints(out); return out }
func sha256Hex(v []byte) string { sum := sha256.Sum256(v); return hex.EncodeToString(sum[:]) }

// CanonicalBytes emits the byte-stable version-one digest payload.
func (m VersionedCompactionManifest) CanonicalBytes() ([]byte, error) {
	type digestPayload VersionedCompactionManifest
	return canonicaljson.Marshal(digestPayload(m))
}

// Validate enforces disjoint, complete classification through the capture watermark.
func (m VersionedCompactionManifest) Validate() error {
	if m.DigestAlgorithm != manifestDigestAlgorithm || m.DigestVersion != manifestDigestVersion {
		return fmt.Errorf("unsupported manifest digest %s v%d", m.DigestAlgorithm, m.DigestVersion)
	}
	classified := map[int]string{}
	sets := []struct {
		name   string
		values []int
	}{{"summarized", m.SummarizedTurnSeqs}, {"tail", m.TailTurnSeqs}, {"protected", m.ProtectedTurnSeqs}, {"excluded", m.ExcludedTurnSeqs}}
	for _, set := range sets {
		for _, seq := range set.values {
			if seq <= 0 || seq > m.CapturedWatermarkSeq {
				return fmt.Errorf("%s seq %d outside capture", set.name, seq)
			}
			if prior := classified[seq]; prior != "" {
				return fmt.Errorf("seq %d classified as %s and %s", seq, prior, set.name)
			}
			classified[seq] = set.name
		}
	}
	turns := map[int]struct{}{}
	for _, turn := range m.Turns {
		if turn.Seq <= m.CapturedWatermarkSeq {
			turns[turn.Seq] = struct{}{}
		}
	}
	for seq := range turns {
		if classified[seq] == "" {
			return fmt.Errorf("captured seq %d is unclassified", seq)
		}
	}
	for seq := range classified {
		if _, ok := turns[seq]; !ok {
			return fmt.Errorf("classified seq %d is absent from capture", seq)
		}
	}
	return nil
}

// Verify validates membership and both SHA-256 digest layers.
func (m VersionedCompactionManifest) Verify() error {
	if err := m.Validate(); err != nil {
		return err
	}
	turnBytes, err := canonicaljson.Marshal(m.Turns)
	if err != nil {
		return err
	}
	if sha256Hex(turnBytes) != m.CompleteCaptureDigest {
		return errors.New("complete capture digest mismatch")
	}
	want := m.Digest
	m.Digest = ""
	raw, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	if sha256Hex(raw) != want {
		return errors.New("manifest digest mismatch")
	}
	return nil
}
