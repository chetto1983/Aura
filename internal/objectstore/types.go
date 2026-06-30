//nolint:revive // Internal objectstore contracts are exported across Aura packages.
package objectstore

import (
	"context"
	"io"
	"time"
)

type ObjectRef struct {
	Bucket string
	Key    string
}

type Attrs struct {
	SizeBytes int64
	ETag      string
	MIMEType  string
}

type ObjectInfo struct {
	Ref   ObjectRef
	Attrs Attrs
}

type ListRequest struct {
	Bucket string
	Prefix string
}

type PutOptions struct {
	MIMEType string
	Size     int64
}

type PresignPutRequest struct {
	Ref        ObjectRef
	MIMEType   string
	Size       int64
	ExpiresIn  time.Duration
	PublicBase string
}

type PresignedPut struct {
	URL             string            `json:"upload_url"`
	Method          string            `json:"method"`
	RequiredHeaders map[string]string `json:"required_headers"`
	ExpiresAt       time.Time         `json:"expires_at"`
}

type Store interface {
	PresignPut(context.Context, PresignPutRequest) (PresignedPut, error)
	Put(context.Context, ObjectRef, io.Reader, PutOptions) (Attrs, error)
	Head(context.Context, ObjectRef) (Attrs, error)
	Get(context.Context, ObjectRef) (io.ReadCloser, Attrs, error)
	List(context.Context, ListRequest) ([]ObjectInfo, error)
	Delete(context.Context, ObjectRef) error
}

func AssetKey(identityID, assetID string) string {
	return "identity/" + identityID + "/asset/" + assetID + "/original"
}
