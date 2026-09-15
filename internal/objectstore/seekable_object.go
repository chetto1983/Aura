//nolint:revive // Read, Seek and Close are the io.ReadSeekCloser contract; their docs are the interface's.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

var errSeekableObjectClosed = errors.New("objectstore: seekable object is closed")

// SeekableObject presents one stored object of known size as an io.ReadSeekCloser without
// reading it whole, which is what net/http.ServeContent needs to answer Range requests over an
// object store. Seeks only move an offset; the store is opened at that offset on the next Read,
// and a Seek elsewhere drops the open body so the following Read reopens there.
type SeekableObject struct {
	ctx   context.Context
	store Store
	ref   ObjectRef
	size  int64

	// mu serialises Close with Read: ServeContent's multi-range writer reads from its own
	// goroutine, which can still be inside Read when the handler's deferred Close runs.
	mu     sync.Mutex
	offset int64
	body   io.ReadCloser
	closed bool
}

// NewSeekableObject reads ref through store, scoped to ctx. size is the caller's record of the
// object's length (the asset row, a share snapshot): reads end there.
func NewSeekableObject(ctx context.Context, store Store, ref ObjectRef, size int64) *SeekableObject {
	return &SeekableObject{ctx: ctx, store: store, ref: ref, size: size}
}

func (o *SeekableObject) Read(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, errSeekableObjectClosed
	}
	remaining := o.size - o.offset
	if remaining <= 0 {
		return 0, io.EOF
	}
	if o.body == nil {
		body, err := o.store.GetFrom(o.ctx, o.ref, o.offset)
		if err != nil {
			return 0, err
		}
		o.body = body
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := o.body.Read(p)
	o.offset += int64(n)
	if errors.Is(err, io.EOF) && o.offset < o.size {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (o *SeekableObject) Seek(offset int64, whence int) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, errSeekableObjectClosed
	}
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += o.offset
	case io.SeekEnd:
		offset += o.size
	default:
		return 0, fmt.Errorf("objectstore: invalid seek whence %d", whence)
	}
	if err := checkOffset(offset); err != nil {
		return 0, err
	}
	if offset != o.offset && o.body != nil {
		_ = o.body.Close()
		o.body = nil
	}
	o.offset = offset
	return offset, nil
}

func (o *SeekableObject) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if o.body == nil {
		return nil
	}
	err := o.body.Close()
	o.body = nil
	return err
}
