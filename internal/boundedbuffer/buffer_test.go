package boundedbuffer

import "testing"

func TestBufferKeepsNewestBytesAcrossWrites(t *testing.T) {
	buf := New(5)

	if n, err := buf.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatalf("first Write = (%d, %v), want (3, nil)", n, err)
	}
	if n, err := buf.Write([]byte("def")); err != nil || n != 3 {
		t.Fatalf("second Write = (%d, %v), want (3, nil)", n, err)
	}
	if got := buf.String(); got != "bcdef" {
		t.Fatalf("String() = %q, want newest window %q", got, "bcdef")
	}
	if got := buf.Len(); got != 5 {
		t.Fatalf("Len() = %d, want 5", got)
	}
}

func TestBufferLargeWriteKeepsTail(t *testing.T) {
	buf := New(4)

	if n, err := buf.Write([]byte("012345")); err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	if got := buf.String(); got != "2345" {
		t.Fatalf("String() = %q, want newest tail %q", got, "2345")
	}
}

func TestBufferDefaultsAndZeroValue(t *testing.T) {
	buf := New(0)
	if n, err := buf.Write([]byte("x")); err != nil || n != 1 {
		t.Fatalf("default Write = (%d, %v), want (1, nil)", n, err)
	}
	if got := buf.String(); got != "x" {
		t.Fatalf("default String() = %q, want x", got)
	}

	var zero Buffer
	if n, err := zero.Write([]byte("discarded")); err != nil || n != 9 {
		t.Fatalf("zero Write = (%d, %v), want (9, nil)", n, err)
	}
	if got := zero.Len(); got != 0 {
		t.Fatalf("zero Len() = %d, want 0", got)
	}
}
