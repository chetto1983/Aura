package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/wiki"
)

// extractor_prompt_version names the prompt schema used by the LLM
// extractor. Bump when the prompt body or the expected JSON shape
// changes so wiki pages emitted under an old prompt can be
// re-extracted on demand. Matches the wiki schema regex for
// PromptVersion: prefix `ingest_v` + integer.
const extractorPromptVersion = "ingest_v2"

// extractorMaxItems caps the total entities + concepts the LLM may
// return per source. Higher numbers add cost without proportional
// recall improvement at Aura's personal-corpus scale; below 5 means
// the pipeline degenerates to a single summary card.
const extractorMaxItems = 15

// entityTypes is the closed set of entity categories the extractor
// will accept. Anything outside this set is rejected at validation —
// keeps the wiki taxonomy stable across sources instead of letting
// the LLM invent new categories per ingest.
var entityTypes = map[string]bool{
	"person":  true,
	"project": true,
	"org":     true,
	"concept": true,
}

// linkTypes is the closed set of relation types between extracted nodes.
var linkTypes = map[string]bool{
	"mentions":     true,
	"uses":         true,
	"extends":      true,
	"contradicts":  true,
	"derived_from": true,
}

// slugRe mirrors the wiki [[wiki-link]] regex (internal/wiki/schema.go:15)
// so extracted slugs cleanly become wiki page filenames. Lowercase
// alphanumerics plus hyphens, no leading/trailing/consecutive hyphens
// (the latter enforced at validation, not regex).
var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// ExtractedEntity is one named-thing fact extracted from a source: a
// person, project, organization, or stable technical concept that
// deserves its own wiki page. Title is human-readable; slug is the
// stable wiki-page identifier the wiki layer will accept.
type ExtractedEntity struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	Type             string   `json:"type"`
	ShortDescription string   `json:"short_description"`
	RelatesTo        []string `json:"relates_to,omitempty"`
}

// ExtractedConcept is a theme, topic, or process the source discusses
// at length — eligible for its own wiki page distinct from the
// canonical entity pages. Key claims preserve verbatim quotes so
// provenance can be traced back without re-reading the source.
type ExtractedConcept struct {
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyClaims []string `json:"key_claims,omitempty"`
}

// ExtractedLink names a directed relation between two extracted slugs
// (or between an extracted slug and an existing wiki slug). The
// multi-page-touch pipeline (Wave 2.4) turns each link into a
// frontmatter `related:` entry on the source side and a body wikilink
// on the target side.
type ExtractedLink struct {
	From string `json:"from_slug"`
	To   string `json:"to_slug"`
	Type string `json:"type"`
}

// ExtractionDelta is the structured result of one LLM extraction call.
// The multi-page-touch pipeline (Wave 2.4) treats it as a graph patch:
// for each entity/concept, write or update the wiki page; for each
// link, ensure the corresponding edge exists in the wiki graph.
type ExtractionDelta struct {
	Entities []ExtractedEntity  `json:"entities"`
	Concepts []ExtractedConcept `json:"concepts"`
	Links    []ExtractedLink    `json:"links"`
}

// TotalItems returns the combined count of entities and concepts. Used
// by the validator to enforce extractorMaxItems and by tests to
// snapshot extraction breadth.
func (d ExtractionDelta) TotalItems() int {
	return len(d.Entities) + len(d.Concepts)
}

// Extractor turns a source's body text into a structured graph patch.
// The interface lets the pipeline layer accept a deterministic stub in
// tests and the LLMExtractor in production.
type Extractor interface {
	Extract(ctx context.Context, req ExtractionRequest) (ExtractionDelta, error)
}

// ExtractionRequest carries everything the extractor needs from the
// caller. ExistingSlugs lists slugs the wiki already owns so the LLM
// can reuse them instead of minting near-duplicates; SourceID feeds
// the provenance prompt section.
type ExtractionRequest struct {
	SourceID      string
	Body          string
	ExistingSlugs []string
}

// LLMExtractor is the production extractor. It issues one
// temperature-0 LLM call per Extract and parses the JSON envelope the
// model returns. Errors from the LLM are surfaced as-is — the caller
// (pipeline.Compile in Wave 2.4) decides whether to fall back to a
// single-page summary write.
type LLMExtractor struct {
	client llm.Client
	model  string
}

// NewLLMExtractor builds an LLMExtractor bound to a specific LLM
// client and model. The model is passed through to each Request so
// the extraction layer can run on a smaller / cheaper model than the
// main conversation loop (e.g. DeepSeek nano) when configured.
func NewLLMExtractor(client llm.Client, model string) *LLMExtractor {
	return &LLMExtractor{client: client, model: strings.TrimSpace(model)}
}

// Extract performs the LLM call and returns a validated ExtractionDelta.
// On parse / validation failure the caller receives the error and an
// empty delta — there is no partial-result accumulation because half a
// graph patch is worse than none (entities without their links are an
// orphan minefield).
func (e *LLMExtractor) Extract(ctx context.Context, req ExtractionRequest) (ExtractionDelta, error) {
	if e == nil || e.client == nil {
		return ExtractionDelta{}, fmt.Errorf("extractor: client not configured")
	}
	if strings.TrimSpace(req.Body) == "" {
		return ExtractionDelta{}, fmt.Errorf("extractor: empty body")
	}

	prompt := buildExtractionPrompt(req)
	zero := 0.0
	resp, err := e.client.Send(ctx, llm.Request{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Model:       e.model,
		Temperature: &zero,
	})
	if err != nil {
		return ExtractionDelta{}, fmt.Errorf("extractor: llm call: %w", err)
	}
	delta, err := parseExtraction(resp.Content)
	if err != nil {
		return ExtractionDelta{}, fmt.Errorf("extractor: parse response: %w", err)
	}
	if err := validateExtraction(&delta, req.ExistingSlugs); err != nil {
		return ExtractionDelta{}, fmt.Errorf("extractor: validate: %w", err)
	}
	return delta, nil
}

// buildExtractionPrompt is the single source of truth for the LLM
// instruction text. Exported as a function (not a const) so the
// existing-slugs and source-id sections can interpolate cleanly.
func buildExtractionPrompt(req ExtractionRequest) string {
	var existingBlock string
	if len(req.ExistingSlugs) == 0 {
		existingBlock = "  (none yet — every slug you emit is new)"
	} else {
		var sb strings.Builder
		for _, slug := range req.ExistingSlugs {
			sb.WriteString("  - ")
			sb.WriteString(slug)
			sb.WriteByte('\n')
		}
		existingBlock = strings.TrimRight(sb.String(), "\n")
	}

	var b strings.Builder
	b.WriteString("You are building a personal knowledge graph from a source document.\n")
	b.WriteString("Extract entities (named persons, projects, organizations, technical concepts)\n")
	b.WriteString("and themes (concepts, topics, processes) so a Markdown wiki can compound\n")
	b.WriteString("around them.\n\n")
	b.WriteString("Rules:\n")
	fmt.Fprintf(&b, "- Maximum %d entities + concepts COMBINED.\n", extractorMaxItems)
	b.WriteString("- Each item has a stable slug: lowercase, words joined by hyphens, no spaces.\n")
	b.WriteString("- Reuse an existing wiki slug verbatim when the entity matches — do NOT mint a near-duplicate.\n")
	b.WriteString("- entity.type must be one of: person, project, org, concept.\n")
	b.WriteString("- link.type must be one of: mentions, uses, extends, contradicts, derived_from.\n")
	b.WriteString("- Links may reference slugs from this output or from the existing list.\n")
	b.WriteString("- Return strict JSON. No prose. No markdown fences. No commentary.\n\n")
	b.WriteString("Existing wiki slugs you may link to:\n")
	b.WriteString(existingBlock)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Source ID: %s\n\n", req.SourceID)
	b.WriteString("Source content (markdown):\n---\n")
	b.WriteString(req.Body)
	b.WriteString("\n---\n\n")
	b.WriteString("Output schema (return JSON matching this exactly):\n")
	b.WriteString(`{
  "entities": [
    {"slug": "...", "title": "...", "type": "person|project|org|concept", "short_description": "1-2 sentences"}
  ],
  "concepts": [
    {"slug": "...", "title": "...", "summary": "2-3 sentences", "key_claims": ["verbatim quote", "..."]}
  ],
  "links": [
    {"from_slug": "...", "to_slug": "...", "type": "mentions|uses|extends|contradicts|derived_from"}
  ]
}`)
	return b.String()
}

// jsonEnvelopeRe matches the outermost {...} block in an LLM response
// so prose preamble / trailing commentary is tolerated when the
// model fails to obey the "no prose" instruction.
var jsonEnvelopeRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseExtraction tolerates a few common LLM output sins (markdown
// fences, leading prose, trailing commentary) and returns the
// embedded JSON object as an ExtractionDelta. Hard error when no
// JSON envelope is found.
func parseExtraction(raw string) (ExtractionDelta, error) {
	raw = strings.TrimSpace(raw)
	// Strip code fences quickly so the regex below sees clean JSON.
	if strings.HasPrefix(raw, "```") {
		// drop the opening fence (with optional "json" language hint)
		// and the closing fence.
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}
	match := jsonEnvelopeRe.FindString(raw)
	if match == "" {
		return ExtractionDelta{}, fmt.Errorf("no JSON envelope found in response")
	}
	var delta ExtractionDelta
	if err := json.Unmarshal([]byte(match), &delta); err != nil {
		return ExtractionDelta{}, fmt.Errorf("unmarshal: %w", err)
	}
	return delta, nil
}

// validateExtraction enforces the schema constraints documented in the
// prompt: slug shape, type whitelist, length cap, link references
// resolve to known slugs. Mutates delta in place to normalize trimmed
// strings + drop trailing empties — cheaper than returning a copy and
// the caller is the only owner of the delta until the multi-page-touch
// pipeline reads it.
//
// Pre-validation canonicalization: when the LLM emits slug ≠ Slug(title)
// (a common short-slug pattern, e.g. slug="davide" + title="Davide
// Marchetto"), we rewrite slug = Slug(title) and remap every link
// + relates_to reference that pointed at the old slug. The wiki Store
// derives the on-disk filename from page.Title via wiki.Slug() so the
// page MUST land at Slug(title) — leaving the LLM-provided slug in
// place would orphan every extracted link. This recovery preserves the
// full title (information dense) while making the slug consistent
// (wiki addressable).
func validateExtraction(delta *ExtractionDelta, existingSlugs []string) error {
	if delta == nil {
		return fmt.Errorf("nil delta")
	}
	canonicalizeSlugs(delta)
	if delta.TotalItems() > extractorMaxItems {
		return fmt.Errorf("delta has %d items, max %d", delta.TotalItems(), extractorMaxItems)
	}

	known := make(map[string]bool, len(existingSlugs)+delta.TotalItems())
	for _, slug := range existingSlugs {
		known[slug] = true
	}

	seenLocal := make(map[string]bool, delta.TotalItems())
	for i := range delta.Entities {
		ent := &delta.Entities[i]
		ent.Slug = strings.TrimSpace(ent.Slug)
		ent.Title = strings.TrimSpace(ent.Title)
		ent.Type = strings.TrimSpace(ent.Type)
		ent.ShortDescription = strings.TrimSpace(ent.ShortDescription)
		if !slugRe.MatchString(ent.Slug) || strings.Contains(ent.Slug, "--") {
			return fmt.Errorf("entity %d: invalid slug %q", i, ent.Slug)
		}
		// Canonicalize via wiki.Slug to catch any further drift.
		if ent.Slug != wiki.Slug(ent.Slug) {
			return fmt.Errorf("entity %d: slug %q not canonical", i, ent.Slug)
		}
		if ent.Title == "" {
			return fmt.Errorf("entity %d (%s): empty title", i, ent.Slug)
		}
		// Slug ↔ Title consistency is now enforced via canonicalizeSlugs
		// above the loop, so we never reach here with a mismatch.
		if !entityTypes[ent.Type] {
			return fmt.Errorf("entity %d (%s): unknown type %q", i, ent.Slug, ent.Type)
		}
		if ent.ShortDescription == "" {
			return fmt.Errorf("entity %d (%s): empty short_description", i, ent.Slug)
		}
		if seenLocal[ent.Slug] {
			return fmt.Errorf("entity %d: duplicate slug %q in this delta", i, ent.Slug)
		}
		seenLocal[ent.Slug] = true
		known[ent.Slug] = true
	}
	for i := range delta.Concepts {
		c := &delta.Concepts[i]
		c.Slug = strings.TrimSpace(c.Slug)
		c.Title = strings.TrimSpace(c.Title)
		c.Summary = strings.TrimSpace(c.Summary)
		if !slugRe.MatchString(c.Slug) || strings.Contains(c.Slug, "--") {
			return fmt.Errorf("concept %d: invalid slug %q", i, c.Slug)
		}
		if c.Slug != wiki.Slug(c.Slug) {
			return fmt.Errorf("concept %d: slug %q not canonical", i, c.Slug)
		}
		if c.Title == "" {
			return fmt.Errorf("concept %d (%s): empty title", i, c.Slug)
		}
		// Slug ↔ Title consistency enforced via canonicalizeSlugs above.
		if c.Summary == "" {
			return fmt.Errorf("concept %d (%s): empty summary", i, c.Slug)
		}
		if seenLocal[c.Slug] {
			return fmt.Errorf("concept %d: duplicate slug %q in this delta", i, c.Slug)
		}
		seenLocal[c.Slug] = true
		known[c.Slug] = true
	}
	for i := range delta.Links {
		ln := &delta.Links[i]
		ln.From = strings.TrimSpace(ln.From)
		ln.To = strings.TrimSpace(ln.To)
		ln.Type = strings.TrimSpace(ln.Type)
		if !linkTypes[ln.Type] {
			return fmt.Errorf("link %d: unknown type %q", i, ln.Type)
		}
		if !known[ln.From] {
			return fmt.Errorf("link %d: from_slug %q not in delta or existing slugs", i, ln.From)
		}
		if !known[ln.To] {
			return fmt.Errorf("link %d: to_slug %q not in delta or existing slugs", i, ln.To)
		}
		if ln.From == ln.To {
			return fmt.Errorf("link %d: self-loop %q -> %q", i, ln.From, ln.To)
		}
	}
	return nil
}

// canonicalizeSlugs ensures every entity / concept slug equals
// wiki.Slug(title). The wiki Store layer derives on-disk filenames
// from page.Title, so a slug that doesn't match would land the page
// at a different slug than every extracted link expects. Rather than
// reject the whole delta when the LLM emits a short slug with a
// fuller title (common pattern — slug="davide" + title="Davide
// Marchetto"), we rewrite the slug and remap every reference.
//
// Empty titles short-circuit; they're caught by the per-entity title
// emptiness check downstream.
func canonicalizeSlugs(delta *ExtractionDelta) {
	if delta == nil {
		return
	}
	rename := map[string]string{}
	for i := range delta.Entities {
		ent := &delta.Entities[i]
		title := strings.TrimSpace(ent.Title)
		if title == "" {
			continue
		}
		canonical := wiki.Slug(title)
		if canonical == "" || canonical == ent.Slug {
			continue
		}
		if ent.Slug != "" {
			rename[ent.Slug] = canonical
		}
		ent.Slug = canonical
	}
	for i := range delta.Concepts {
		c := &delta.Concepts[i]
		title := strings.TrimSpace(c.Title)
		if title == "" {
			continue
		}
		canonical := wiki.Slug(title)
		if canonical == "" || canonical == c.Slug {
			continue
		}
		if c.Slug != "" {
			rename[c.Slug] = canonical
		}
		c.Slug = canonical
	}
	if len(rename) == 0 {
		return
	}
	for i := range delta.Links {
		if newSlug, ok := rename[delta.Links[i].From]; ok {
			delta.Links[i].From = newSlug
		}
		if newSlug, ok := rename[delta.Links[i].To]; ok {
			delta.Links[i].To = newSlug
		}
	}
	for i := range delta.Entities {
		ent := &delta.Entities[i]
		for j, r := range ent.RelatesTo {
			if newSlug, ok := rename[r]; ok {
				ent.RelatesTo[j] = newSlug
			}
		}
	}
}

// PromptVersion returns the prompt schema identifier so pages written
// from an extraction can record which prompt produced them. Consumed
// by the multi-page-touch pipeline (Wave 2.4) when stamping
// wiki.Page.PromptVersion.
func PromptVersion() string { return extractorPromptVersion }
