// Package documents holds what is left of Aura's document plane after the two-store
// convergence: the generic asset ingestion queue, the retrieval front-end over ArcadeDB,
// and the object-name resolution that turns a bucket key back into a person's file name.
//
// It no longer holds a catalog. Garage owns the bytes and their names, CocoIndex reconciles
// the bucket, and ArcadeDB holds the passages and the one row per indexed object. The
// Postgres tables that used to mirror all of that went in migration 0098.
package documents
