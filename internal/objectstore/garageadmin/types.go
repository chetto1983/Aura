// Package garageadmin is a stdlib net/http client for the Garage Admin API v2
// (Phase 36 D-08). It provisions and de-provisions a per-identity object-store
// bucket plus a scoped access key so each identity's asset bucket is storage-
// isolated — Garage grants are per-bucket, NOT per-prefix (F-007), so a shared
// bucket with key-prefixes would be a silent hole. There is no external
// dependency: the client speaks the /v2/ admin endpoints over net/http with a
// bearer admin token that reaches the admin API on the INTERNAL compose network
// only (never host-published — Pitfall 3).
//
// Wire-shape note (RESEARCH A1, LOW confidence): the Garage v2 request/response
// JSON field names (globalAlias / accessKeyId / secretAccessKey / bucketId) are
// taken from the cited garagehq admin-api + garage-webui docs. They are pinned by
// the struct tags below and CONFIRMED against the live API by the garage_integration
// test (client_integration_test.go); a wrong field name is a struct-tag fix, not a
// design change.
package garageadmin

// Permissions is the Garage read/write/owner grant triple carried by
// AllowBucketKey / DenyBucketKey. A per-identity key is granted read+write with
// owner=false on ONLY its own bucket (never owner — an identity must not be able
// to re-grant or delete its bucket via the S3 data plane).
type Permissions struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
	Owner bool `json:"owner"`
}

// ReadWrite is the canonical per-identity grant: read+write, owner=false.
var ReadWrite = Permissions{Read: true, Write: true, Owner: false}

// createBucketRequest is the POST /v2/CreateBucket body: a single global alias
// (the bucket name aura-<identity>).
type createBucketRequest struct {
	GlobalAlias string `json:"globalAlias"`
}

// createKeyRequest is the POST /v2/CreateKey body: a human label for the key.
type createKeyRequest struct {
	Name string `json:"name"`
}

// bucketKeyRequest is the POST /v2/AllowBucketKey and /v2/DenyBucketKey body: the
// target bucket id, the access key id, and the permission triple.
type bucketKeyRequest struct {
	BucketID    string      `json:"bucketId"`
	AccessKeyID string      `json:"accessKeyId"`
	Permissions Permissions `json:"permissions"`
}

// bucketInfo is the subset of the Garage v2 bucket object we consume: the internal
// bucket id (used by AllowBucketKey/DeleteBucket) and its global aliases (used to
// resolve an already-existing bucket by name for idempotency).
type bucketInfo struct {
	ID            string   `json:"id"`
	GlobalAliases []string `json:"globalAliases"`
}

// keyInfo is the subset of the Garage v2 CreateKey response we consume. The
// secretAccessKey is shown ONCE at creation and is unrecoverable afterwards — the
// caller must persist it immediately (Task 3, encrypted at rest).
type keyInfo struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}
