package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

const (
	wikiSuggestQuestionsDefaultTopK = 5
	wikiSuggestQuestionsMaxTopK     = 7
)

type WikiSuggestQuestionsTool struct {
	store *wiki.Store
}

func NewWikiSuggestQuestionsTool(store *wiki.Store) *WikiSuggestQuestionsTool {
	if store == nil {
		return nil
	}
	return &WikiSuggestQuestionsTool{store: store}
}

func (t *WikiSuggestQuestionsTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("wiki_suggest_questions: tool unavailable")
	}
	topK := intArg(args, "top_k", wikiSuggestQuestionsDefaultTopK, 1, wikiSuggestQuestionsMaxTopK)
	return renderSuggestedQuestions(t.store.SuggestQuestions(topK)), nil
}

func renderSuggestedQuestions(questions []wiki.Question) string {
	if len(questions) == 0 {
		return "No suggested questions for the current wiki."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d suggested question(s) for the current wiki:\n", len(questions))
	fmt.Fprintln(&b, "Use these question texts as the final answer; keep the wiki slugs and do not describe the tool call.")
	for _, q := range questions {
		fmt.Fprintf(&b, "\n- [%s] %s", q.Type, q.Text)
		if q.Why != "" {
			fmt.Fprintf(&b, "\n  why: %s", q.Why)
		}
	}
	return b.String()
}
