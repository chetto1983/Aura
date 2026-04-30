package skills

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultCatalogURL = "https://skills.sh/"

var catalogItemRE = regexp.MustCompile(`\\?"source\\?":\\?"([^"\\]+)\\?",\\?"skillId\\?":\\?"([^"\\]+)\\?",\\?"name\\?":\\?"([^"\\]+)\\?",\\?"installs\\?":([0-9]+)`)

// CatalogItem is one skill entry from skills.sh.
type CatalogItem struct {
	Source   string `json:"source"`
	SkillID  string `json:"skill_id"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
}

// InstallCommand returns the skills.sh CLI install command for this item.
func (i CatalogItem) InstallCommand() string {
	if i.SkillID == "" {
		return fmt.Sprintf("npx skills add %s", i.Source)
	}
	return fmt.Sprintf("npx skills add %s --skill %s", i.Source, i.SkillID)
}

// CatalogClient fetches and searches the public skills.sh catalog.
type CatalogClient struct {
	url    string
	client *http.Client
}

// NewCatalogClient creates a skills.sh catalog client.
func NewCatalogClient(rawURL string) *CatalogClient {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		rawURL = DefaultCatalogURL
	}
	return &CatalogClient{
		url: rawURL,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Search returns catalog entries matching query. Empty query returns the
// leaderboard order exposed by skills.sh.
func (c *CatalogClient) Search(ctx context.Context, query string, limit int) ([]CatalogItem, error) {
	if c == nil {
		return nil, fmt.Errorf("skills catalog unavailable")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills catalog HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	items := parseCatalogItems(string(body))
	if query = strings.TrimSpace(strings.ToLower(query)); query != "" {
		filtered := items[:0]
		for _, item := range items {
			haystack := strings.ToLower(item.Name + " " + item.SkillID + " " + item.Source)
			if strings.Contains(haystack, query) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func parseCatalogItems(raw string) []CatalogItem {
	matches := catalogItemRE.FindAllStringSubmatch(raw, -1)
	seen := make(map[string]bool, len(matches))
	items := make([]CatalogItem, 0, len(matches))
	for _, match := range matches {
		if len(match) != 5 {
			continue
		}
		source := unescapeCatalogString(match[1])
		skillID := unescapeCatalogString(match[2])
		name := unescapeCatalogString(match[3])
		key := source + "\x00" + skillID
		if seen[key] {
			continue
		}
		seen[key] = true
		installs, _ := strconv.Atoi(match[4])
		items = append(items, CatalogItem{
			Source:   source,
			SkillID:  skillID,
			Name:     name,
			Installs: installs,
		})
	}
	return items
}

func unescapeCatalogString(s string) string {
	s = strings.ReplaceAll(s, `\/`, `/`)
	if decoded, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return decoded
	}
	return s
}
