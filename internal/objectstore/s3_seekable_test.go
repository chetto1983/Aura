package objectstore

import (
	"io"
	"strings"
	"testing"
)

type nonSeekable struct{ io.Reader }

func TestSeekableBodySpoolsAStreamAndPassesASeekerThrough(t *testing.T) {
	stream := nonSeekable{strings.NewReader("telegram photo bytes")}
	body, cleanup, err := seekableBody(stream)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	seeker, ok := body.(io.ReadSeeker)
	if !ok {
		t.Fatalf("spooled body %T is not seekable", body)
	}
	got, err := io.ReadAll(seeker)
	if err != nil || string(got) != "telegram photo bytes" {
		t.Fatalf("spooled bytes = %q, err = %v", got, err)
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek back: %v", err)
	}

	direct := strings.NewReader("already seekable")
	body2, cleanup2, err := seekableBody(direct)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if body2 != io.Reader(direct) {
		t.Fatalf("seekable body was wrapped: %T", body2)
	}
}
