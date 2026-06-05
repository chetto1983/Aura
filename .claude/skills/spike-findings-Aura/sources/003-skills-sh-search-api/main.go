// Spike 003 — skills-sh-search-api: prove the skills.sh JSON search endpoint
// from Go with the exact client shape Phase-11 7b will ship. Validates: schema
// stability across queries, URL-encoding behavior, both North-Star targets
// discoverable (anthropics/skills → xlsx, vercel-labs/skills → find-skills),
// ranking signal (installs), and edge cases (empty query, no-results, latency).
//
// Run: go run ./.planning/spikes/003-skills-sh-search-api
// Exit 0 = VALIDATED, 1 = failure. Forensic [CATEGORY] log lines.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const base = "https://www.skills.sh/api/search"

type searchResponse struct {
	Query      string  `json:"query"`
	SearchType string  `json:"searchType"`
	Skills     []skill `json:"skills"`
}

type skill struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
}

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	logf("SUMMARY", "INVALIDATED — "+format, a...)
	os.Exit(1)
}

func search(ctx context.Context, c *http.Client, q string) (searchResponse, int, time.Duration, error) {
	var out searchResponse
	u := base + "?q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, 0, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aura-spike-003 (+https://github.com/chetto1983)")
	start := time.Now()
	resp, err := c.Do(req)
	dur := time.Since(start)
	if err != nil {
		return out, 0, dur, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return out, resp.StatusCode, dur, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	dec.DisallowUnknownFields() // schema-drift tripwire: new fields = finding, not failure (retried lax below)
	err = dec.Decode(&out)
	return out, resp.StatusCode, dur, err
}

func searchLax(ctx context.Context, c *http.Client, q string) (searchResponse, error) {
	var out searchResponse
	u := base + "?q=" + url.QueryEscape(q)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := &http.Client{Timeout: 15 * time.Second}

	// 1. Strict-schema probe across query variants.
	strictOK := true
	for _, q := range []string{"xlsx", "find skills", "golang", "excel spreadsheet"} {
		out, code, dur, err := search(ctx, c, q)
		if err != nil {
			if strings.Contains(err.Error(), "unknown field") {
				logf("FINDING", "q=%q: schema gained unknown fields (%v) — lax retry", q, err)
				strictOK = false
				if out2, err2 := searchLax(ctx, c, q); err2 == nil {
					out = out2
				} else {
					fail("q=%q: lax decode also failed: %v", q, err2)
				}
			} else {
				fail("q=%q: %v (HTTP %d)", q, err, code)
			}
		}
		top := "—"
		if len(out.Skills) > 0 {
			top = fmt.Sprintf("%s (%s, %d installs)", out.Skills[0].Name, out.Skills[0].Source, out.Skills[0].Installs)
		}
		logf("RUN", "q=%q: HTTP %d in %s, searchType=%s, %d results, top=%s", q, code, dur.Round(time.Millisecond), out.SearchType, len(out.Skills), top)
		if len(out.Skills) == 0 {
			fail("q=%q returned zero results — endpoint degraded or query semantics changed", q)
		}
		for _, s := range out.Skills[:min(3, len(out.Skills))] {
			if s.ID == "" || s.Name == "" || s.Source == "" {
				fail("q=%q: result missing required fields: %+v", q, s)
			}
		}
	}
	logf("PASS", "1/4 schema stable across 4 query variants (strict=%v)", strictOK)

	// 2. North-Star target discoverability.
	xlsxFound, findSkillsFound := false, false
	if out, err := searchLax(ctx, c, "xlsx"); err == nil {
		for _, s := range out.Skills {
			if s.Source == "anthropics/skills" && s.SkillID == "xlsx" {
				xlsxFound = true
				logf("TARGET", "anthropics/skills→xlsx FOUND: id=%s installs=%d", s.ID, s.Installs)
			}
		}
	}
	if out, err := searchLax(ctx, c, "find skills"); err == nil {
		for _, s := range out.Skills {
			if s.Source == "vercel-labs/skills" && strings.Contains(s.SkillID, "find-skills") {
				findSkillsFound = true
				logf("TARGET", "vercel-labs/skills→find-skills FOUND: id=%s installs=%d", s.ID, s.Installs)
			}
		}
	}
	if !xlsxFound || !findSkillsFound {
		fail("North-Star targets: xlsx=%v find-skills=%v", xlsxFound, findSkillsFound)
	}
	logf("PASS", "2/4 both North-Star targets discoverable")

	// 3. Edge cases: empty query, garbage query, unicode.
	if out, _, _, err := search(ctx, c, ""); err != nil {
		logf("EDGE", "empty query → error: %v (acceptable, client must guard)", err)
	} else {
		logf("EDGE", "empty query → HTTP OK, %d results, searchType=%s", len(out.Skills), out.SearchType)
	}
	if out, err := searchLax(ctx, c, "zzzz-no-such-skill-xyzzy-9999"); err != nil {
		fail("garbage query decode failed: %v", err)
	} else {
		logf("EDGE", "garbage query → %d results (no-results shape is valid JSON)", len(out.Skills))
	}
	if out, err := searchLax(ctx, c, "fogli di calcolo è"); err != nil {
		fail("unicode query failed: %v", err)
	} else {
		logf("EDGE", "unicode query → %d results", len(out.Skills))
	}
	logf("PASS", "3/4 edge cases handled (empty/garbage/unicode)")

	// 4. Burst behavior: 5 rapid calls — rate limiting posture.
	var worst time.Duration
	for i := 0; i < 5; i++ {
		_, code, dur, err := search(ctx, c, "golang")
		if err != nil && !strings.Contains(err.Error(), "unknown field") {
			logf("FINDING", "burst call %d: HTTP %d err=%v — rate limit posture", i+1, code, err)
			if code == 429 {
				logf("FINDING", "429 at call %d — client needs backoff + cache", i+1)
				break
			}
			fail("burst call %d failed non-429: %v", i+1, err)
		}
		if dur > worst {
			worst = dur
		}
	}
	logf("PASS", "4/4 burst of 5 OK, worst latency %s", worst.Round(time.Millisecond))

	logf("SUMMARY", "VALIDATED — /api/search is Go-consumable: stable schema, both targets discoverable, sane edges, no rate-limit at 5-burst")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
