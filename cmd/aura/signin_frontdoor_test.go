package main

import (
	"net/url"
	"regexp"
	"testing"
)

func TestCockpitFrontdoorsRefuseTheProgrammableSandbox(t *testing.T) {
	root := repoRootForTest(t)
	request, err := url.Parse("/mcp-sandbox?host=https%3A%2F%2Fattacker.example")
	if err != nil {
		t.Fatal(err)
	}
	snippet := regexp.MustCompile(`(?ms)^\(aura_frontdoor\)\s*\{\n(.*?)^\}`)
	deny := regexp.MustCompile(`(?s)handle\s+` + regexp.QuoteMeta(request.Path) + `\s*\{\s*respond\s+404\s*\}`)
	apps := regexp.MustCompile(`(?s)handle\s+/mcp-sandbox\s*\{\s*reverse_proxy\s+aura:9080\s*\}`)
	for _, file := range []string{"caddy/Caddyfile", "caddy/Caddyfile.domain"} {
		t.Run(file, func(t *testing.T) {
			config := readProjectFile(t, root, file)
			frontdoor := snippet.FindStringSubmatch(config)
			if len(frontdoor) != 2 || !deny.MatchString(frontdoor[1]) {
				t.Fatalf("%s: collapsed Apps origin exposes the raw sandbox through the cockpit", file)
			}
			if !apps.MatchString(snippet.ReplaceAllString(config, "")) {
				t.Fatalf("%s: dedicated Apps origin lost its sandbox route", file)
			}
		})
	}
}
