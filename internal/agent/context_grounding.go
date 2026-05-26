package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/aura/aura/internal/storage/memoryindex"
)

const groundingCapsuleMaxChars = 1800

type groundingMemorySearcher interface {
	Search(ctx context.Context, query string, filter memoryindex.Filter) ([]memoryindex.Document, error)
}

// BuildTurnGroundingCapsule performs cheap deterministic recall before the LLM
// sees a turn. It is intentionally tiny: identity/location facts and directly
// relevant local sources only, so the model starts from known local state
// instead of spending several tool calls rediscovering it.
func BuildTurnGroundingCapsule(ctx context.Context, store groundingMemorySearcher, userText string) string {
	if store == nil || strings.TrimSpace(userText) == "" {
		return ""
	}
	userText = strings.TrimSpace(userText)
	var docs []memoryindex.Document
	seen := map[string]bool{}

	if hasPersonalCue(userText) {
		for _, doc := range searchGrounding(ctx, store, "residenza localita location vive citta domicilio", []string{memoryindex.KindUserMemory}, 3) {
			appendGroundingDoc(&docs, seen, doc)
		}
	}

	sourceQuery := userText
	if loc := bestLocationHint(docs); loc != "" {
		sourceQuery += " " + loc
	}
	if hasLocalSourceCue(userText) || len(docs) > 0 {
		for _, doc := range searchGrounding(ctx, store, sourceQuery, []string{memoryindex.KindSource}, 3) {
			appendGroundingDoc(&docs, seen, doc)
		}
	}
	if len(docs) == 0 {
		return ""
	}
	return renderGroundingCapsule(docs)
}

func searchGrounding(ctx context.Context, store groundingMemorySearcher, query string, kinds []string, limit int) []memoryindex.Document {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	docs, err := store.Search(ctx, query, memoryindex.Filter{Kinds: kinds, Limit: limit})
	if err != nil {
		return nil
	}
	out := docs[:0]
	for _, doc := range docs {
		if strings.TrimSpace(doc.Status) != "" && doc.Status != "active" {
			continue
		}
		out = append(out, doc)
	}
	return out
}

func appendGroundingDoc(out *[]memoryindex.Document, seen map[string]bool, doc memoryindex.Document) {
	if doc.ID == "" || seen[doc.ID] {
		return
	}
	seen[doc.ID] = true
	*out = append(*out, doc)
}

func renderGroundingCapsule(docs []memoryindex.Document) string {
	var sb strings.Builder
	sb.WriteString("## Turn Grounding Capsule\n")
	sb.WriteString("Local state recalled before the model turn. Use it as grounding; do not write it back unless the user explicitly asks.\n")
	for _, doc := range docs {
		line := fmt.Sprintf("- [%s] %s: %s", doc.Kind, firstNonEmpty(doc.Title, doc.ID), compactGroundingText(doc.Body, 260))
		if sb.Len()+len(line)+1 > groundingCapsuleMaxChars {
			sb.WriteString("[grounding truncated]")
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

func hasPersonalCue(s string) bool {
	s = strings.ToLower(s)
	for _, cue := range []string{" da me", "dove sono", "mia ", "mio ", "mie ", "miei ", "per me", "su di me"} {
		if strings.Contains(" "+s+" ", cue) {
			return true
		}
	}
	return false
}

func hasLocalSourceCue(s string) bool {
	s = strings.ToLower(s)
	for _, cue := range []string{"meteo", "tempo", "oggi", "domani", "fonte", "source", "documento"} {
		if strings.Contains(s, cue) {
			return true
		}
	}
	return false
}

func bestLocationHint(docs []memoryindex.Document) string {
	for _, doc := range docs {
		text := doc.Title + " " + doc.Body
		for _, marker := range []string{"vive a ", "residenza: ", "residenza ", "localita di riferimento e "} {
			if loc := wordAfterMarker(text, marker); loc != "" {
				return loc
			}
		}
		for _, entity := range doc.Entities {
			if isLikelyPlace(entity) {
				return entity
			}
		}
	}
	return ""
}

func wordAfterMarker(text, marker string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[idx+len(marker):])
	if rest == "" {
		return ""
	}
	var words []string
	for _, field := range strings.Fields(rest) {
		clean := strings.TrimFunc(field, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '\''
		})
		if clean == "" {
			break
		}
		words = append(words, clean)
		if len(words) >= 2 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(words, " "))
}

func isLikelyPlace(entity string) bool {
	entity = strings.TrimSpace(entity)
	return entity != "" && entity != "Davide Marchetto" && len([]rune(entity)) <= 40
}

func compactGroundingText(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
