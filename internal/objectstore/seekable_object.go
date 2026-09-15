//nolint:revive // Read, Seek and Close are the io.ReadSeekCloser contract; their docs are the interface's.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var errSeekableObjectClosed = errors.New("objectstore: seekable object is closed")

// SeekableObject presents one stored object as an io.ReadSeekCloser without reading it whole,
// which is what net/http.ServeContent needs to answer Range requests over an object store.
// Seeks only move an offset; the store is opened at that offset on the next Read, and a Seek
// elsewhere drops the open body so the following Read reopens there. It is not safe for
// concurrent use.
type SeekableObject struct {
	ctx   context.Context
	store Store
	ref   ObjectRef
	size  int64

	offset int64
	body   io.ReadCloser
	closed bool
	err    error
}

// OpenSeekableObject heads ref before anything is served, so a missing object is an error
// while a response can still say so, and the object serves the size the store reports rather
// than any size recorded about it.
func OpenSeekableObject(ctx context.Context, store Store, ref ObjectRef) (*SeekableObject, error) {
	attrs, err := store.Head(ctx, ref)
	if err != nil {
		return nil, err
	}
	return newSeekableObject(ctx, store, ref, attrs.SizeBytes), nil
}

func newSeekableObject(ctx context.Context, store Store, ref ObjectRef, size int64) *SeekableObject {
	return &SeekableObject{ctx: ctx, store: store, ref: ref, size: size}
}

// Err returns the first store error a Read met. ServeContent discards its copy error once the
// status line is written, so a caller that served this object must ask here.
func (o *SeekableObject) Err() error {
	return o.err
}

func (o *SeekableObject) Read(p []byte) (int, error) {
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
			o.keep(err)
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
	if err != nil && !errors.Is(err, io.EOF) {
		o.keep(err)
	}
	return n, err
}

func (o *SeekableObject) Seek(offset int64, whence int) (int64, error) {
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
	o.closed = true
	if o.body == nil {
		return nil
	}
	err := o.body.Close()
	o.body = nil
	return err
}

func (o *SeekableObject) keep(err error) {
	if o.err == nil {
		o.err = err
	}
}
