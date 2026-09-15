package objectstore

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// openRecordingStore records every GetFrom offset and whether each opened body was closed, so
// the reader's lazy-open and reopen-on-seek behaviour is asserted, not inferred from bytes.
type openRecordingStore struct {
	Store
	mu      sync.Mutex
	offsets []int64
	bodies  []*closeRecordingBody
	openErr error
}

type closeRecordingBody struct {
	io.ReadCloser
	closed bool
}

func (b *closeRecordingBody) Close() error {
	b.closed = true
	return b.ReadCloser.Close()
}

func (s *openRecordingStore) GetFrom(ctx context.Context, ref ObjectRef, offset int64) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offsets = append(s.offsets, offset)
	if s.openErr != nil {
		return nil, s.openErr
	}
	body, err := s.Store.GetFrom(ctx, ref, offset)
	if err != nil {
		return nil, err
	}
	recorded := &closeRecordingBody{ReadCloser: body}
	s.bodies = append(s.bodies, recorded)
	return recorded, nil
}

var seekableRef = ObjectRef{Bucket: "bucket", Key: "clip.mp4"}

func newSeekableRig(t *testing.T, stored string, size int64) (*SeekableObject, *openRecordingStore) {
	t.Helper()
	store := &openRecordingStore{Store: NewFake()}
	if _, err := store.Put(context.Background(), seekableRef, strings.NewReader(stored), PutOptions{Size: int64(len(stored))}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	return NewSeekableObject(context.Background(), store, seekableRef, size), store
}

func readN(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err != nil {
		t.Fatalf("read %d bytes: got %d, err = %v", n, got, err)
	}
	return string(buf)
}

func TestSeekableObjectMeasuresItsSizeWithoutOpeningTheStore(t *testing.T) {
	object, store := newSeekableRig(t, getFromContent, int64(len(getFromContent)))
	end, err := object.Seek(0, io.SeekEnd)
	if err != nil || end != int64(len(getFromContent)) {
		t.Fatalf("Seek(0, SeekEnd) = %d, %v, want %d", end, err, len(getFromContent))
	}
	if start, err := object.Seek(0, io.SeekStart); err != nil || start != 0 {
		t.Fatalf("Seek(0, SeekStart) = %d, %v, want 0", start, err)
	}
	if len(store.offsets) != 0 {
		t.Fatalf("GetFrom offsets = %v, want none before the first Read", store.offsets)
	}
}

func TestSeekableObjectOpensOnceAtTheOffsetAndReopensOnlyOnAMove(t *testing.T) {
	object, store := newSeekableRig(t, getFromContent, int64(len(getFromContent)))
	if _, err := object.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, object, 2); got != "34" {
		t.Fatalf("read after Seek(3) = %q, want 34", got)
	}
	if got := readN(t, object, 2); got != "56" {
		t.Fatalf("continued read = %q, want 56", got)
	}
	if pos, err := object.Seek(0, io.SeekCurrent); err != nil || pos != 7 {
		t.Fatalf("Seek(0, SeekCurrent) = %d, %v, want 7", pos, err)
	}
	if got := readN(t, object, 1); got != "7" {
		t.Fatalf("read after a same-offset seek = %q, want 7", got)
	}
	if len(store.offsets) != 1 || store.offsets[0] != 3 {
		t.Fatalf("GetFrom offsets = %v, want exactly [3] while the offset never moved", store.offsets)
	}

	if pos, err := object.Seek(-9, io.SeekEnd); err != nil || pos != 1 {
		t.Fatalf("Seek(-9, SeekEnd) = %d, %v, want 1", pos, err)
	}
	if !store.bodies[0].closed {
		t.Fatal("the body opened at 3 was not closed when the offset moved")
	}
	if got := readN(t, object, 3); got != "123" {
		t.Fatalf("read after Seek(-9, SeekEnd) = %q, want 123", got)
	}
	if pos, err := object.Seek(2, io.SeekCurrent); err != nil || pos != 6 {
		t.Fatalf("Seek(2, SeekCurrent) = %d, %v, want 6", pos, err)
	}
	if got := readN(t, object, 4); got != "6789" {
		t.Fatalf("read after Seek(2, SeekCurrent) = %q, want 6789", got)
	}
	if want := []int64{3, 1, 6}; len(store.offsets) != len(want) || store.offsets[1] != want[1] || store.offsets[2] != want[2] {
		t.Fatalf("GetFrom offsets = %v, want %v", store.offsets, want)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !store.bodies[2].closed {
		t.Fatal("Close did not close the open body")
	}
}

func TestSeekableObjectEndsAtItsSizeWithoutTouchingTheStore(t *testing.T) {
	object, store := newSeekableRig(t, getFromContent, 4)
	got, err := io.ReadAll(object)
	if err != nil || string(got) != "0123" {
		t.Fatalf("ReadAll = %q, %v, want 0123 clamped to the declared size", got, err)
	}
	if _, err := object.Seek(40, io.SeekStart); err != nil {
		t.Fatalf("Seek past the end error = %v, want allowed", err)
	}
	if n, err := object.Read(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read past the end = %d, %v, want 0, EOF", n, err)
	}
	if len(store.offsets) != 1 {
		t.Fatalf("GetFrom offsets = %v, want one open; reads at the end must not reopen", store.offsets)
	}
}

func TestSeekableObjectReportsAnObjectShorterThanItsSize(t *testing.T) {
	object, _ := newSeekableRig(t, "0123", 10)
	_, err := io.ReadAll(object)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadAll error = %v, want io.ErrUnexpectedEOF for a truncated object", err)
	}
}

func TestSeekableObjectRefusesInvalidSeeksAndSurfacesOpenErrors(t *testing.T) {
	object, store := newSeekableRig(t, getFromContent, int64(len(getFromContent)))
	if _, err := object.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("Seek(-1, SeekStart) error = nil, want refusal")
	}
	if _, err := object.Seek(0, 42); err == nil {
		t.Fatal("Seek with an unknown whence error = nil, want refusal")
	}
	if pos, _ := object.Seek(0, io.SeekCurrent); pos != 0 {
		t.Fatalf("a refused seek moved the offset to %d", pos)
	}
	store.openErr = errors.New("store unavailable")
	if _, err := object.Read(make([]byte, 1)); !errors.Is(err, store.openErr) {
		t.Fatalf("Read error = %v, want the store's open error", err)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("Close() with nothing open error = %v", err)
	}
}

func TestSeekableObjectRefusesUseAfterClose(t *testing.T) {
	object, store := newSeekableRig(t, getFromContent, int64(len(getFromContent)))
	if err := object.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := object.Read(make([]byte, 1)); err == nil {
		t.Fatal("Read after Close error = nil, want refusal")
	}
	if _, err := object.Seek(1, io.SeekStart); err == nil {
		t.Fatal("Seek after Close error = nil, want refusal")
	}
	if len(store.offsets) != 0 {
		t.Fatalf("GetFrom offsets = %v, want no reopen after Close", store.offsets)
	}
}

// net/http.ServeContent's multi-range writer reads from its own goroutine, which can still be
// inside Read when the handler's deferred Close runs. Run under -race.
func TestSeekableObjectCloseRacesARead(t *testing.T) {
	object, _ := newSeekableRig(t, strings.Repeat("x", 1<<16), 1<<16)
	var wg sync.WaitGroup
	wg.Go(func() {
		buf := make([]byte, 16)
		for {
			if _, err := object.Read(buf); err != nil {
				return
			}
		}
	})
	_ = object.Close()
	wg.Wait()
}
