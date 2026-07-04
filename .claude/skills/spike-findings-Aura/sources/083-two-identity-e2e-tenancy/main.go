// Spike 083 — 2-identity E2E tenancy: Garage + agent-memory planes.
//
// Proves per-identity isolation across TWO of the three storage planes that
// Phase 36/37 must enforce together, keyed on the SAME identities A and B as
// the box plane (see run.sh). The box plane (078) is proven in run.sh with
// plain docker named volumes; this harness proves the two planes that benefit
// from Aura's real production seams:
//
//	Garage plane  (closes 080's open PUT/GET-deny tier) — via internal/objectstore
//	Memory plane  (closes 081's open 2-identity recall tier) — via internal/mcp
//
// Forensic log: ISO-stamped [CATEGORY] lines, [SUMMARY] verdict, exit 0/1.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/objectstore"
)

func ts() string { return time.Now().UTC().Format(time.RFC3339) }
func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

var failures int

func check(cat, desc string, ok bool, detail string) {
	if ok {
		logf(cat, "PASS %s — %s", desc, detail)
	} else {
		failures++
		logf(cat, "FAIL %s — %s", desc, detail)
	}
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func newStore(endpoint, ak, sk string) (*objectstore.S3Store, error) {
	return objectstore.NewS3(context.Background(), objectstore.S3Config{
		Endpoint:  endpoint,
		Region:    "garage",
		AccessKey: ak,
		SecretKey: sk,
		PathStyle: true, // Garage requires path-style
	})
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "accessdenied") || strings.Contains(m, "access denied") ||
		strings.Contains(m, "forbidden") || strings.Contains(m, "403")
}

func garagePlane() {
	endpoint := env("GARAGE_ENDPOINT", "http://127.0.0.1:3900")
	bucketA := env("BUCKET_A", "spike083-a")
	bucketB := env("BUCKET_B", "spike083-b")
	keyAID, keyASec := os.Getenv("KEY_A_ID"), os.Getenv("KEY_A_SECRET")
	keyBID, keyBSec := os.Getenv("KEY_B_ID"), os.Getenv("KEY_B_SECRET")

	logf("GARAGE", "endpoint=%s bucketA=%s bucketB=%s (via internal/objectstore.S3Store)", endpoint, bucketA, bucketB)

	storeA, err := newStore(endpoint, keyAID, keyASec)
	if err != nil {
		check("GARAGE", "store-a init", false, err.Error())
		return
	}
	storeB, err := newStore(endpoint, keyBID, keyBSec)
	if err != nil {
		check("GARAGE", "store-b init", false, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	payloadA := []byte("AURA-SPIKE-083-ALICE-OBJECT")
	refA := objectstore.ObjectRef{Bucket: bucketA, Key: "identity-a/secret.txt"}
	refB := objectstore.ObjectRef{Bucket: bucketB, Key: "identity-b/secret.txt"}

	// A PUT + GET round-trip in its OWN bucket (closes 080's open tier).
	_, err = storeA.Put(ctx, refA, bytes.NewReader(payloadA), objectstore.PutOptions{MIMEType: "text/plain", Size: int64(len(payloadA))})
	check("GARAGE", "A PUT own bucket", err == nil, fmt.Sprintf("%v", err))

	body, _, err := storeA.Get(ctx, refA)
	got := ""
	if err == nil {
		b, _ := io.ReadAll(body)
		_ = body.Close()
		got = string(b)
	}
	check("GARAGE", "A GET own bucket round-trips", err == nil && got == string(payloadA), fmt.Sprintf("got=%q err=%v", got, err))

	// B PUT in its own bucket works (B is not broken, only scoped).
	payloadB := []byte("AURA-SPIKE-083-BOB-OBJECT")
	_, err = storeB.Put(ctx, refB, bytes.NewReader(payloadB), objectstore.PutOptions{MIMEType: "text/plain", Size: int64(len(payloadB))})
	check("GARAGE", "B PUT own bucket", err == nil, fmt.Sprintf("%v", err))

	// CROSS-DENY: B GET A's object must be AccessDenied (bucket-per-identity + scoped key).
	_, _, err = storeB.Get(ctx, refA)
	check("GARAGE", "B GET A's object DENIED", isAccessDenied(err), fmt.Sprintf("err=%v", err))

	// CROSS-DENY: A GET B's object must be denied.
	_, _, err = storeA.Get(ctx, refB)
	check("GARAGE", "A GET B's object DENIED", isAccessDenied(err), fmt.Sprintf("err=%v", err))

	// CROSS-DENY: B PUT into A's bucket must be denied.
	_, err = storeB.Put(ctx, refA, bytes.NewReader([]byte("intrusion")), objectstore.PutOptions{MIMEType: "text/plain", Size: 9})
	check("GARAGE", "B PUT into A's bucket DENIED", isAccessDenied(err), fmt.Sprintf("err=%v", err))
}

func memoryPlane() {
	url := env("MEMORY_URL", "http://127.0.0.1:8091/mcp/")
	idA := env("MEM_ID_A", "spike083-alice")
	idB := env("MEM_ID_B", "spike083-bob")
	logf("MEMORY", "url=%s idA=%s idB=%s (via internal/mcp.OpenServer, real bridge)", url, idA, idB)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cli, err := mcp.OpenServer(ctx, "spike083-memory", mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   url,
		Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
	if err != nil {
		check("MEMORY", "open memory MCP", false, err.Error())
		return
	}
	defer func() { _ = cli.Close() }()

	subject := fmt.Sprintf("Spike083 Subject %d", time.Now().UnixNano())

	// A writes a fact scoped to identity A.
	factText, err := cli.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject": subject, "predicate": "has_marker", "object_value": "alice-only",
		"user_identifier": idA,
	})
	check("MEMORY", "A add_fact stored", err == nil && strings.Contains(factText, `"stored": true`), fmt.Sprintf("err=%v resp=%s", err, truncate(factText)))

	// A recalls its own fact.
	aText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{"subject": subject, "user_identifier": idA})
	check("MEMORY", "A recalls own fact", err == nil && strings.Contains(aText, `"fact_count": 1`), fmt.Sprintf("err=%v resp=%s", err, truncate(aText)))

	// B (different identity scope) must NOT see A's fact.
	bText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{"subject": subject, "user_identifier": idB})
	check("MEMORY", "B does NOT see A's fact", err == nil && strings.Contains(bText, `"fact_count": 0`), fmt.Sprintf("err=%v resp=%s", err, truncate(bText)))
}

func truncate(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

func main() {
	logf("START", "spike 083 — 2-identity E2E tenancy (Garage + Memory planes; box plane in run.sh)")
	garagePlane()
	memoryPlane()
	if failures == 0 {
		logf("SUMMARY", "VALIDATED — all Garage + Memory per-identity isolation checks passed")
		os.Exit(0)
	}
	logf("SUMMARY", "FAILED — %d check(s) failed", failures)
	os.Exit(1)
}
