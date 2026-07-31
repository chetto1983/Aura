//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

const chaosBody = "aura-objectstore-chaos-sentinel\n"

type result struct {
	Mode      string `json:"mode"`
	Key       string `json:"key"`
	Items     int    `json:"items"`
	SHA256    string `json:"sha256,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

func main() {
	var mode, key string
	flag.StringVar(&mode, "mode", "", "seed|verify|delete|empty")
	flag.StringVar(&key, "key", "", "owned key below chaos/<id>/")
	flag.Parse()
	if !validKey(key) {
		fatal("key must have shape chaos/<owned-id>/<name>")
	}
	switch mode {
	case "seed", "verify", "delete", "empty":
	default:
		fatal("mode must be seed, verify, delete, or empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store, err := objectstore.NewS3(ctx, objectstore.S3Config{
		Endpoint:       requiredEnv("AURA_OBJECTSTORE_ENDPOINT"),
		Region:         requiredEnv("AURA_OBJECTSTORE_REGION"),
		AccessKey:      requiredEnv("AURA_OBJECTSTORE_ACCESS_KEY"),
		SecretKey:      requiredEnv("AURA_OBJECTSTORE_SECRET_KEY"),
		PathStyle:      true,
		PublicEndpoint: os.Getenv("AURA_OBJECTSTORE_PUBLIC_ENDPOINT"),
	})
	if err != nil {
		fatal(err.Error())
	}
	bucket := requiredEnv("AURA_OBJECTSTORE_BUCKET")
	started := time.Now()

	var items int
	var checksum string
	switch mode {
	case "seed":
		_, err = store.Put(
			ctx,
			objectstore.ObjectRef{Bucket: bucket, Key: key},
			strings.NewReader(chaosBody),
			objectstore.PutOptions{
				MIMEType: "application/octet-stream",
				Size:     int64(len(chaosBody)),
			},
		)
		if err == nil {
			items, checksum, err = verify(ctx, store, bucket, key)
		}
	case "verify":
		items, checksum, err = verify(ctx, store, bucket, key)
	case "delete":
		err = deleteOwnedPrefix(ctx, store, bucket, ownedPrefix(key))
		if err == nil {
			items, err = count(ctx, store, bucket, ownedPrefix(key))
		}
	case "empty":
		items, err = count(ctx, store, bucket, ownedPrefix(key))
		if err == nil && items != 0 {
			err = fmt.Errorf("owned prefix contains %d objects, want 0", items)
		}
	}
	if err != nil {
		fatal(err.Error())
	}
	encoded, err := json.Marshal(result{
		Mode: mode, Key: key, Items: items, SHA256: checksum,
		ElapsedMS: time.Since(started).Milliseconds(),
	})
	if err != nil {
		fatal(err.Error())
	}
	fmt.Println(string(encoded))
}

func verify(
	ctx context.Context,
	store objectstore.Store,
	bucket, key string,
) (int, string, error) {
	objects, err := store.List(ctx, objectstore.ListRequest{
		Bucket: bucket,
		Prefix: ownedPrefix(key),
		Limit:  10,
	})
	if err != nil {
		return 0, "", err
	}
	if len(objects) != 1 || objects[0].Ref.Key != key {
		return len(objects), "", fmt.Errorf(
			"owned prefix has %d objects and key %q, want exactly %q",
			len(objects),
			firstKey(objects),
			key,
		)
	}
	reader, _, err := store.Get(ctx, objects[0].Ref)
	if err != nil {
		return 1, "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, int64(len(chaosBody)+1)))
	closeErr := reader.Close()
	if readErr != nil {
		return 1, "", readErr
	}
	if closeErr != nil {
		return 1, "", closeErr
	}
	if !bytes.Equal(data, []byte(chaosBody)) {
		return 1, "", fmt.Errorf("restored object bytes changed")
	}
	sum := sha256.Sum256(data)
	return 1, hex.EncodeToString(sum[:]), nil
}

func deleteOwnedPrefix(
	ctx context.Context,
	store objectstore.Store,
	bucket, prefix string,
) error {
	objects, err := store.List(ctx, objectstore.ListRequest{
		Bucket: bucket,
		Prefix: prefix,
		Limit:  10,
	})
	if err != nil {
		return err
	}
	for _, object := range objects {
		if !strings.HasPrefix(object.Ref.Key, prefix) {
			return fmt.Errorf("refusing key outside owned prefix: %q", object.Ref.Key)
		}
		if err := store.Delete(ctx, object.Ref); err != nil {
			return err
		}
	}
	return nil
}

func count(
	ctx context.Context,
	store objectstore.Store,
	bucket, prefix string,
) (int, error) {
	objects, err := store.List(ctx, objectstore.ListRequest{
		Bucket: bucket,
		Prefix: prefix,
		Limit:  10,
	})
	return len(objects), err
}

func validKey(key string) bool {
	prefix := ownedPrefix(key)
	return strings.HasPrefix(prefix, "chaos/") &&
		strings.Count(prefix, "/") == 2 &&
		!strings.Contains(key, "..") &&
		path.Clean(key) == key &&
		path.Base(key) != "."
}

func ownedPrefix(key string) string {
	index := strings.LastIndex(key, "/")
	if index < 0 {
		return ""
	}
	return key[:index+1]
}

func firstKey(objects []objectstore.ObjectInfo) string {
	if len(objects) == 0 {
		return ""
	}
	return objects[0].Ref.Key
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fatal(name + " is required")
	}
	return value
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "objectstore chaos:", message)
	os.Exit(1)
}
