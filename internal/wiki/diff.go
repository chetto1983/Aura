package wiki

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const DefaultGraphDiffSince = "HEAD~1"

var errGraphDiffFoundCommit = errors.New("graph diff found commit")

type GraphDiff struct {
	NewNodes     []string    `json:"new_nodes"`
	RemovedNodes []string    `json:"removed_nodes"`
	NewEdges     []GraphEdge `json:"new_edges"`
	RemovedEdges []GraphEdge `json:"removed_edges"`
	Summary      string      `json:"summary"`
}

func DiffStoreFromGitRef(store *Store, since string) (GraphDiff, string, error) {
	if store == nil {
		return GraphDiff{}, "", fmt.Errorf("wiki graph diff: store unavailable")
	}
	current, err := BuildGraphFromReader(store)
	if err != nil {
		return GraphDiff{}, "", fmt.Errorf("wiki graph diff: current graph: %w", err)
	}
	previous, resolved, err := GraphAtGitRef(store.Dir(), since)
	if err != nil {
		return GraphDiff{}, "", err
	}
	return DiffGraphs(previous, current), resolved, nil
}

func GraphAtGitRef(wikiDir, since string) (Graph, string, error) {
	repo, err := git.PlainOpen(wikiDir)
	if err != nil {
		return Graph{}, "", fmt.Errorf("wiki graph diff: open git repo: %w", err)
	}
	hash, resolved, err := resolveGraphDiffRevision(repo, since)
	if err != nil {
		return Graph{}, "", err
	}
	if hash == nil {
		return BuildGraph(nil), resolved, nil
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return Graph{}, "", fmt.Errorf("wiki graph diff: read commit %s: %w", hash.String(), err)
	}
	pages, err := pagesFromCommit(commit)
	if err != nil {
		return Graph{}, "", err
	}
	return BuildGraph(pages), resolved, nil
}

func DiffGraphs(previous, current Graph) GraphDiff {
	oldNodes := graphNodeSet(previous)
	newNodes := graphNodeSet(current)
	oldEdges := graphEdgeSet(previous)
	newEdges := graphEdgeSet(current)

	diff := GraphDiff{
		NewNodes:     stringSetDiff(newNodes, oldNodes),
		RemovedNodes: stringSetDiff(oldNodes, newNodes),
		NewEdges:     edgeSetDiff(newEdges, oldEdges),
		RemovedEdges: edgeSetDiff(oldEdges, newEdges),
	}
	diff.Summary = graphDiffSummary(diff)
	return diff
}

func resolveGraphDiffRevision(repo *git.Repository, since string) (*plumbing.Hash, string, error) {
	since = strings.TrimSpace(since)
	if since == "" {
		since = DefaultGraphDiffSince
	}
	if ts, ok := parseGraphDiffTime(since); ok {
		return commitAtOrBefore(repo, ts, since)
	}
	if since == DefaultGraphDiffSince {
		head, err := repo.Head()
		if err != nil {
			return nil, since, fmt.Errorf("wiki graph diff: resolve HEAD: %w", err)
		}
		commit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return nil, since, fmt.Errorf("wiki graph diff: read HEAD: %w", err)
		}
		parent, err := commit.Parent(0)
		if err == object.ErrParentNotFound {
			return nil, since, nil
		}
		if err != nil {
			return nil, since, fmt.Errorf("wiki graph diff: read HEAD parent: %w", err)
		}
		hash := parent.Hash
		return &hash, since, nil
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(since))
	if err != nil {
		return nil, since, fmt.Errorf("wiki graph diff: resolve %q: %w", since, err)
	}
	return hash, since, nil
}

func commitAtOrBefore(repo *git.Repository, ts time.Time, label string) (*plumbing.Hash, string, error) {
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, label, fmt.Errorf("wiki graph diff: log: %w", err)
	}
	defer iter.Close()
	var found *plumbing.Hash
	err = iter.ForEach(func(c *object.Commit) error {
		if !c.Committer.When.After(ts) {
			hash := c.Hash
			found = &hash
			return errGraphDiffFoundCommit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errGraphDiffFoundCommit) {
		return nil, label, err
	}
	return found, label, nil
}

func pagesFromCommit(commit *object.Commit) (map[string]*Page, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("wiki graph diff: commit tree: %w", err)
	}
	pages := map[string]*Page{}
	err = tree.Files().ForEach(func(f *object.File) error {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, "/") {
			return nil
		}
		slug, ext, ok := graphDiffPageName(name)
		if !ok || IsOperationalSlug(slug) {
			return nil
		}
		content, err := f.Contents()
		if err != nil {
			return fmt.Errorf("wiki graph diff: read %s: %w", name, err)
		}
		var page *Page
		if ext == ".md" {
			page, err = ParseMD([]byte(content))
		} else {
			page, err = ParseYAML([]byte(content))
		}
		if err != nil {
			return nil
		}
		pages[slug] = page
		return nil
	})
	return pages, err
}

func graphDiffPageName(name string) (slug string, ext string, ok bool) {
	switch {
	case strings.HasSuffix(name, ".md"):
		return strings.TrimSuffix(name, ".md"), ".md", true
	case strings.HasSuffix(name, ".yaml"):
		return strings.TrimSuffix(name, ".yaml"), ".yaml", true
	default:
		return "", "", false
	}
}

func graphNodeSet(g Graph) map[string]bool {
	out := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.ID != "" {
			out[n.ID] = true
		}
	}
	return out
}

func graphEdgeSet(g Graph) map[string]GraphEdge {
	out := make(map[string]GraphEdge, len(g.Edges))
	for _, e := range g.Edges {
		out[graphEdgeKey(e)] = e
	}
	return out
}

func stringSetDiff(a, b map[string]bool) []string {
	out := make([]string, 0)
	for s := range a {
		if !b[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func edgeSetDiff(a, b map[string]GraphEdge) []GraphEdge {
	out := make([]GraphEdge, 0)
	for key, edge := range a {
		if _, ok := b[key]; !ok {
			out = append(out, edge)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Type < out[j].Type
	})
	return out
}

func graphEdgeKey(e GraphEdge) string {
	return e.Source + "\x00" + e.Target + "\x00" + e.Type
}

func graphDiffSummary(d GraphDiff) string {
	total := len(d.NewNodes) + len(d.RemovedNodes) + len(d.NewEdges) + len(d.RemovedEdges)
	if total == 0 {
		return "no changes"
	}
	return fmt.Sprintf("%d new nodes, %d removed nodes, %d new edges, %d removed edges",
		len(d.NewNodes), len(d.RemovedNodes), len(d.NewEdges), len(d.RemovedEdges))
}

func parseGraphDiffTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}
