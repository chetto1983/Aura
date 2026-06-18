package display

import "testing"

// TestNormalizeCodeText: a shell/sandbox text result becomes a code payload with
// body + lang.
func TestNormalizeCodeText(t *testing.T) {
	p, ok := normalizeCode("call-1", CodeInput{Body: "hello\nworld", Lang: "bash"})
	if !ok || p.Type != KindCode {
		t.Fatalf("normalizeCode = (%v, %v), want code/true", p.Type, ok)
	}
	if p.Code == nil || p.Code.Body != "hello\nworld" || p.Code.Lang != "bash" {
		t.Fatalf("code payload wrong: %+v", p.Code)
	}
	if p.Artifact != nil {
		t.Fatalf("text result must not set Artifact")
	}
}

// TestNormalizeCodeArtifact: a run whose product is a named file becomes a
// local_artifact payload (filename + size + path), not a code body.
func TestNormalizeCodeArtifact(t *testing.T) {
	p, ok := normalizeCode("call-2", CodeInput{ArtifactFilename: "out.csv", ArtifactSize: 1234, ArtifactPath: "/run/out.csv"})
	if !ok || p.Type != KindLocalArtifact {
		t.Fatalf("normalizeCode(artifact) = (%v, %v), want local_artifact/true", p.Type, ok)
	}
	if p.Artifact == nil || p.Artifact.Filename != "out.csv" || p.Artifact.SizeBytes != 1234 || p.Artifact.Path != "/run/out.csv" {
		t.Fatalf("artifact payload wrong: %+v", p.Artifact)
	}
	if p.Code != nil {
		t.Fatalf("artifact result must not set Code")
	}
}
