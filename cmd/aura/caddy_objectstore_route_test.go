package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/idroot"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// Browser uploads PUT to a presigned /aura-<identity>/<key> URL on the Caddy origin. A matcher
// that knew only the shared aura-assets bucket sent every per-identity request to aura, which
// answered the CORS preflight 401 and failed every upload (live, 2026-09-19). Caddy's regexp
// restates idroot's identity charset, so the two are checked against each other here.
func TestCaddyfilesRouteEveryIdentityBucketToGarage(t *testing.T) {
	root := repoRootForTest(t)
	matcher := regexp.MustCompile(`(?m)^\s*@objectstore path_regexp (\S+)\s*$`)

	for _, rel := range []string{"caddy/Caddyfile", "caddy/Caddyfile.domain"} {
		caddyfile := readProjectFile(t, root, rel)
		found := matcher.FindAllStringSubmatch(caddyfile, -1)
		if len(found) == 0 {
			t.Fatalf("%s has no @objectstore path_regexp matcher", rel)
		}
		for _, m := range found {
			route := regexp.MustCompile(m[1])
			for _, identity := range []string{"assets", "davide", "a", "user_1.test-x", strings.Repeat("z", 64)} {
				if err := idroot.ValidateIdentity(identity); err != nil {
					t.Fatalf("fixture identity %q is not valid: %v", identity, err)
				}
				bucket, err := garageadmin.BucketForIdentity(identity)
				if err != nil {
					t.Fatalf("BucketForIdentity(%q): %v", identity, err)
				}
				if path := "/" + bucket + "/objects/f.png"; !route.MatchString(path) {
					t.Errorf("%s: %s does not route %s to garage", rel, m[1], path)
				}
			}
			for _, cockpit := range []string{"/", "/login", "/setup", "/api/health", "/assets/index-x.js", "/aura", "/aura-/x", "/aura-assets"} {
				if route.MatchString(cockpit) {
					t.Errorf("%s: %s would steal cockpit path %s from aura", rel, m[1], cockpit)
				}
			}
		}
	}
}
