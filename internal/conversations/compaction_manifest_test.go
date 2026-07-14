package conversations

import (
	"bytes"
	"testing"
)

func TestManifestByteStableAndExactlyOnce(t *testing.T) {
	m := NewCompactionManifest(5, []int{1, 2}, []int{4}, []int{3}, []int{5}, []ManifestTurn{{Seq: 1, Role: "user", Content: "a"}, {Seq: 2, Role: "assistant", Content: "b"}, {Seq: 3, Role: "system", Content: "l0"}, {Seq: 4, Role: "user", Content: "tail"}, {Seq: 5, Role: "assistant", Content: "post"}})
	a, err := m.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("manifest encoding is unstable")
	}
	if err = m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRejectsDuplicateClassification(t *testing.T) {
	m := NewCompactionManifest(2, []int{1}, []int{1}, nil, nil, []ManifestTurn{{Seq: 1}})
	if err := m.Validate(); err == nil {
		t.Fatal("expected duplicate classification error")
	}
}

func TestManifestRejectsDigestMismatch(t *testing.T) {
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1, Content: "x"}})
	m.Digest = "bad"
	if err := m.Verify(); err == nil {
		t.Fatal("expected digest mismatch")
	}
}

func TestManifestRejectsUnsupportedDigestAlgorithmOrVersion(t *testing.T) {
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}})
	m.DigestAlgorithm = "md5"
	if err := m.Validate(); err == nil {
		t.Fatal("expected unsupported digest algorithm error")
	}
}

func TestManifestRejectsSeqOutsideCapture(t *testing.T) {
	m := NewCompactionManifest(1, []int{5}, nil, nil, nil, []ManifestTurn{{Seq: 1}})
	if err := m.Validate(); err == nil {
		t.Fatal("expected seq-outside-capture error")
	}
}

func TestManifestRejectsUnclassifiedCapturedSeq(t *testing.T) {
	m := NewCompactionManifest(2, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}, {Seq: 2}})
	if err := m.Validate(); err == nil {
		t.Fatal("expected unclassified seq error")
	}
}

func TestManifestRejectsClassifiedSeqAbsentFromCapture(t *testing.T) {
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, nil)
	if err := m.Validate(); err == nil {
		t.Fatal("expected classified-seq-absent error")
	}
}

func TestManifestRejectsCompleteCaptureDigestMismatch(t *testing.T) {
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1, Content: "x"}})
	m.CompleteCaptureDigest = "bad"
	if err := m.Verify(); err == nil {
		t.Fatal("expected complete capture digest mismatch")
	}
}
