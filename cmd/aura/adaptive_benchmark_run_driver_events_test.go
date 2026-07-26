package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	auraeval "github.com/chetto1983/aura/internal/eval"
)

func TestAdaptiveBenchmarkStateIntegerAcceptsExactNonnegativeNumbers(
	t *testing.T,
) {
	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{name: "int", value: int(1), want: 1},
		{name: "int32", value: int32(2), want: 2},
		{name: "int64", value: int64(3), want: 3},
		{name: "float64", value: float64(4), want: 4},
		{name: "json number", value: json.Number("5"), want: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := adaptiveBenchmarkStateInteger(
				map[string]any{"tokens": test.value},
				"tokens",
			)
			if err != nil || got != test.want {
				t.Fatalf("value=%v got=%d error=%v", test.value, got, err)
			}
		})
	}
	if got, err := adaptiveBenchmarkStateInteger(nil, "tokens"); err != nil ||
		got != 0 {
		t.Fatalf("nil state got=%d error=%v", got, err)
	}
	if got, err := adaptiveBenchmarkStateInteger(
		map[string]any{},
		"tokens",
	); err != nil || got != 0 {
		t.Fatalf("missing state got=%d error=%v", got, err)
	}
}

func TestAdaptiveBenchmarkStateIntegerRejectsInexactNumbers(t *testing.T) {
	tests := []any{
		int(-1),
		int32(-1),
		int64(-1),
		float64(-1),
		float64(1.5),
		math.NaN(),
		math.Inf(1),
		float64(math.MaxInt64),
		json.Number("-1"),
		json.Number("1.5"),
		"1",
	}
	for _, value := range tests {
		if _, err := adaptiveBenchmarkStateInteger(
			map[string]any{"tokens": value},
			"tokens",
		); err == nil {
			t.Fatalf("accepted %#v", value)
		}
	}
}

func TestAdaptiveBenchmarkTerminalUsagePreservesUnknownCost(t *testing.T) {
	prompt, completion, cost, err := adaptiveBenchmarkTerminalUsage(
		map[string]any{
			"prompt_tokens":     json.Number("11"),
			"completion_tokens": int32(7),
		},
	)
	if err != nil ||
		prompt != 11 ||
		completion != 7 ||
		cost != nil {
		t.Fatalf(
			"usage prompt=%d completion=%d cost=%v error=%v",
			prompt,
			completion,
			cost,
			err,
		)
	}
	for _, invalid := range []any{
		"0.1",
		float64(-1),
		math.Copysign(0, -1),
		math.NaN(),
		math.Inf(1),
	} {
		if _, _, _, err := adaptiveBenchmarkTerminalUsage(
			map[string]any{"cost_usd": invalid},
		); err == nil {
			t.Fatalf("accepted cost %#v", invalid)
		}
	}
	if _, _, _, err := adaptiveBenchmarkTerminalUsage(map[string]any{
		"prompt_tokens":     int64(math.MaxInt64),
		"completion_tokens": int64(1),
	}); err == nil {
		t.Fatal("accepted overflowing token total")
	}
}

func TestAdaptiveBenchmarkStateNumberAcceptsProviderNumericShapes(
	t *testing.T,
) {
	tests := []struct {
		value any
		want  float64
		ok    bool
	}{
		{value: int(1), want: 1, ok: true},
		{value: int32(2), want: 2, ok: true},
		{value: int64(3), want: 3, ok: true},
		{value: float64(4), want: 4, ok: true},
		{value: json.Number("5.5"), want: 5.5, ok: true},
		{value: json.Number("bad")},
		{value: "6"},
	}
	for _, test := range tests {
		got, ok := adaptiveBenchmarkStateNumber(test.value)
		if got != test.want || ok != test.ok {
			t.Fatalf("value=%#v got=%v,%v", test.value, got, ok)
		}
	}
}

func TestValidateAdaptiveBenchmarkDispatchAllowsOnlyRegisteredReadPaths(
	t *testing.T,
) {
	if err := validateAdaptiveBenchmarkDispatch(&agent.Event{}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{
		"text_response",
		"tool_search",
		"document_search",
		"read_tool_output",
	} {
		event := adaptiveBenchmarkToolStart(tool, `{}`)
		if err := validateAdaptiveBenchmarkDispatch(event); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	if err := validateAdaptiveBenchmarkDispatch(
		adaptiveBenchmarkToolStart(
			"skill",
			`{"action":"list","query":"golang tests"}`,
		),
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range []*agent.Event{
		adaptiveBenchmarkToolStart("shell_exec", `{}`),
		adaptiveBenchmarkToolStart("skill", `{}`),
		adaptiveBenchmarkToolStart(
			"skill",
			`{"action":"use","query":"golang tests"}`,
		),
		adaptiveBenchmarkToolStart(
			"skill",
			`{"action":"list","query":" spaced "}`,
		),
		adaptiveBenchmarkToolStart(
			"skill",
			`{"action":"list","query":"x","extra":true}`,
		),
	} {
		if !errors.Is(
			validateAdaptiveBenchmarkDispatch(event),
			auraeval.ErrAdaptiveBenchmarkUnsafeDispatch,
		) {
			t.Fatalf("unsafe dispatch was accepted: %#v", event)
		}
	}
	end := adaptiveBenchmarkToolStart("shell_exec", `{}`)
	end.Actions.ToolInvocation.Event = agent.ToolInvocationEnd
	if err := validateAdaptiveBenchmarkDispatch(end); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptiveBenchmarkTurnFailureReasonIsBounded(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: io.EOF, want: auraeval.AdaptiveBenchmarkReasonModelTransportFailure},
		{
			err:  &net.DNSError{IsTimeout: true},
			want: auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
		},
		{
			err:  &json.UnmarshalTypeError{},
			want: auraeval.AdaptiveBenchmarkReasonModelParserFailure,
		},
		{
			err:  adaptive.ErrPayloadConflict,
			want: auraeval.AdaptiveBenchmarkReasonControlFailed,
		},
		{
			err:  errors.New("unclassified"),
			want: auraeval.AdaptiveBenchmarkReasonDriverFailure,
		},
	}
	for _, test := range tests {
		if got := adaptiveBenchmarkTurnFailureReason(test.err); got != test.want {
			t.Fatalf("error=%v reason=%q want=%q", test.err, got, test.want)
		}
	}
}

func TestCloneAdaptiveBenchmarkCostPreservesNilAndValue(t *testing.T) {
	if cloneAdaptiveBenchmarkCost(nil) != nil {
		t.Fatal("nil cost became known")
	}
	value := 0.5
	cloned := cloneAdaptiveBenchmarkCost(&value)
	if cloned == nil || *cloned != value || cloned == &value {
		t.Fatalf("cloned cost = %v", cloned)
	}
}

func adaptiveBenchmarkToolStart(
	name string,
	arguments string,
) *agent.Event {
	return &agent.Event{Actions: agent.Actions{
		ToolInvocation: &agent.ToolInvocation{
			Event:    agent.ToolInvocationStart,
			ToolName: name, Arguments: arguments,
		},
	}}
}
