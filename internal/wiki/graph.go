package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type GraphNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type GraphBrokenRef struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type,omitempty"`
}

type Graph struct {
	Nodes      []GraphNode      `json:"nodes"`
	Edges      []GraphEdge      `json:"edges"`
	BrokenRefs []GraphBrokenRef `json:"broken_refs"`
	Orphans    []string         `json:"orphans"`
}

func BuildGraph(pages map[string]*Page) Graph {
	if len(pages) == 0 {
		return Graph{
			Nodes:      []GraphNode{},
			Edges:      []GraphEdge{},
			BrokenRefs: []GraphBrokenRef{},
			Orphans:    []string{},
		}
	}

	slugs := make([]string, 0, len(pages))
	for slug := range pages {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	nodes := make([]GraphNode, 0, len(slugs))
	edges := make([]GraphEdge, 0)
	broken := make([]GraphBrokenRef, 0)
	degree := make(map[string]int, len(slugs))

	for _, slug := range slugs {
		page := pages[slug]
		title := slug
		category := ""
		if page != nil {
			if strings.TrimSpace(page.Title) != "" {
				title = page.Title
			}
			category = strings.TrimSpace(page.Category)
		}
		nodes = append(nodes, GraphNode{ID: slug, Title: title, Category: category})
		degree[slug] = 0
	}

	for _, slug := range slugs {
		page := pages[slug]
		if page == nil {
			continue
		}
		seenEdges := map[string]bool{}
		seenBroken := map[string]bool{}
		addRef := func(target, refType string) {
			target = Slug(target)
			if target == "" || target == slug {
				return
			}
			if _, ok := pages[target]; !ok {
				key := refType + "\x00" + target
				if seenBroken[key] {
					return
				}
				seenBroken[key] = true
				broken = append(broken, GraphBrokenRef{Source: slug, Target: target, Type: refType})
				return
			}
			if seenEdges[target] {
				return
			}
			seenEdges[target] = true
			edges = append(edges, GraphEdge{Source: slug, Target: target, Type: refType})
			degree[slug]++
			degree[target]++
		}
		for _, link := range ExtractWikiLinksTyped(page.Body) {
			addRef(link.Slug, link.Type)
		}
		for _, ref := range page.Related {
			addRef(ref.Slug, "related")
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Type < edges[j].Type
	})
	sort.Slice(broken, func(i, j int) bool {
		if broken[i].Source != broken[j].Source {
			return broken[i].Source < broken[j].Source
		}
		if broken[i].Target != broken[j].Target {
			return broken[i].Target < broken[j].Target
		}
		return broken[i].Type < broken[j].Type
	})

	orphans := make([]string, 0)
	for _, slug := range slugs {
		if degree[slug] == 0 {
			orphans = append(orphans, slug)
		}
	}

	return Graph{
		Nodes:      nodes,
		Edges:      edges,
		BrokenRefs: broken,
		Orphans:    orphans,
	}
}

func BuildGraphFromReader(reader PageReader) (Graph, error) {
	slugs, err := reader.ListPages()
	if err != nil {
		return Graph{}, err
	}
	pages := make(map[string]*Page, len(slugs))
	for _, slug := range slugs {
		page, err := reader.ReadPage(slug)
		if err != nil {
			continue
		}
		pages[slug] = page
	}
	return BuildGraph(pages), nil
}

func WriteGraphFiles(wikiDir string, graph Graph) error {
	graphDir := filepath.Join(wikiDir, "graph")
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		return fmt.Errorf("create graph dir: %w", err)
	}
	data, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal graph json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(graphDir, "graph.json"), data, 0o644); err != nil {
		return fmt.Errorf("write graph.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(graphDir, "context.md"), []byte(GraphContextMarkdown(graph)), 0o644); err != nil {
		return fmt.Errorf("write graph context: %w", err)
	}
	return nil
}

func ReadGraphFile(wikiDir string) (Graph, error) {
	data, err := os.ReadFile(filepath.Join(wikiDir, "graph", "graph.json"))
	if err != nil {
		return Graph{}, err
	}
	var graph Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		return Graph{}, err
	}
	normalizeGraphSlices(&graph)
	return graph, nil
}

func GraphContextMarkdown(graph Graph) string {
	degree := map[string]int{}
	for _, node := range graph.Nodes {
		degree[node.ID] = 0
	}
	for _, edge := range graph.Edges {
		degree[edge.Source]++
		degree[edge.Target]++
	}
	hubs := make([]GraphNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if degree[node.ID] > 0 {
			hubs = append(hubs, node)
		}
	}
	sort.Slice(hubs, func(i, j int) bool {
		if degree[hubs[i].ID] != degree[hubs[j].ID] {
			return degree[hubs[i].ID] > degree[hubs[j].ID]
		}
		return hubs[i].ID < hubs[j].ID
	})
	if len(hubs) > 10 {
		hubs = hubs[:10]
	}

	var sb strings.Builder
	sb.WriteString("# Wiki Graph Context\n\n")
	fmt.Fprintf(&sb, "- Pages: %d\n", len(graph.Nodes))
	fmt.Fprintf(&sb, "- Edges: %d\n", len(graph.Edges))
	fmt.Fprintf(&sb, "- Orphans: %d\n", len(graph.Orphans))
	fmt.Fprintf(&sb, "- Broken refs: %d\n\n", len(graph.BrokenRefs))
	sb.WriteString("## Hubs\n\n")
	if len(hubs) == 0 {
		sb.WriteString("- none\n")
	} else {
		for _, node := range hubs {
			fmt.Fprintf(&sb, "- [[%s]] degree=%d\n", node.ID, degree[node.ID])
		}
	}
	if len(graph.Orphans) > 0 {
		sb.WriteString("\n## Orphans\n\n")
		for _, slug := range graph.Orphans {
			fmt.Fprintf(&sb, "- [[%s]]\n", slug)
		}
	}
	if len(graph.BrokenRefs) > 0 {
		sb.WriteString("\n## Broken Refs\n\n")
		for _, ref := range graph.BrokenRefs {
			fmt.Fprintf(&sb, "- [[%s]] -> [[%s]]", ref.Source, ref.Target)
			if ref.Type != "" {
				fmt.Fprintf(&sb, " (%s)", ref.Type)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func normalizeGraphSlices(graph *Graph) {
	if graph.Nodes == nil {
		graph.Nodes = []GraphNode{}
	}
	if graph.Edges == nil {
		graph.Edges = []GraphEdge{}
	}
	if graph.BrokenRefs == nil {
		graph.BrokenRefs = []GraphBrokenRef{}
	}
	if graph.Orphans == nil {
		graph.Orphans = []string{}
	}
}
