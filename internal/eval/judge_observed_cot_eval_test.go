//go:build cot_eval

package eval

import (
	"strings"
	"testing"
)

func TestJudgeUserMessageIncludesObservedToolExecution(t *testing.T) {
	sc := scenario{
		id:           "tool-time",
		prompts:      []string{"Che ore sono adesso? Usa lo strumento giusto per ottenere l'ora corrente."},
		dimensions:   []dimension{dimToolLoop},
		expectedTool: "current_time",
	}
	c := &turnCapture{
		prose:        "Sono le 13:47 UTC del 2 luglio 2026.",
		finish:       "stop",
		toolNames:    []string{"current_time"},
		toolCallMS:   []float64{123.4},
		toolResults:  []string{`{"now":"2026-07-02T13:47:02Z","timezone":"UTC"}`},
		toolResultMS: []float64{150.5},
	}

	got := judgeUserMessage(sc, c)

	for _, want := range []string{
		"USER QUESTION:",
		"OBSERVED EXECUTION (ground truth):",
		"tool_calls: current_time",
		"tool_results:",
		"2026-07-02T13:47:02Z",
		"ASSISTANT ANSWER:",
		c.prose,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("judge user message missing %q:\n%s", want, got)
		}
	}
}

func TestJudgeUserMessageOmitsObservedExecutionForPlainReasoning(t *testing.T) {
	sc := scenario{
		id:         "cot-arith",
		prompts:    []string{"Quanto fa 47 moltiplicato per 23?"},
		dimensions: []dimension{dimReasoning},
	}
	c := &turnCapture{prose: "47 * 23 = 1081."}

	got := judgeUserMessage(sc, c)

	if strings.Contains(got, "OBSERVED EXECUTION") {
		t.Fatalf("plain reasoning judge should not include observed execution:\n%s", got)
	}
	if !strings.Contains(got, "ASSISTANT ANSWER:\n"+c.prose) {
		t.Fatalf("judge user message missing answer:\n%s", got)
	}
}

func TestJudgeUserMessageIncludesPriorUserTurnsForMemory(t *testing.T) {
	sc := scenario{
		id: "mem-2turn",
		prompts: []string{
			"Mi chiamo Giulio e il mio colore preferito e' il verde. Ricordalo.",
			"Come mi chiamo e qual e' il mio colore preferito?",
		},
		dimensions: []dimension{dimCachePrefix},
	}
	c := &turnCapture{prose: "Ti chiami Giulio e il tuo colore preferito e' il verde."}

	got := judgeUserMessage(sc, c)

	for _, want := range []string{
		"CONVERSATION CONTEXT (previous user turns):",
		"Mi chiamo Giulio",
		"verde",
		"USER QUESTION:\nCome mi chiamo",
		"ASSISTANT ANSWER:\n" + c.prose,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory judge user message missing %q:\n%s", want, got)
		}
	}
}
