package main

import "testing"

func TestSameAnswer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		got      string
		expected string
		want     bool
	}{
		{name: "case and punctuation", got: "ROME.", expected: "rome", want: true},
		{name: "word number", got: "seven years", expected: "7 years", want: true},
		{name: "time filler", got: "Tuesday at 02:00 UTC", expected: "tuesday 02:00 utc", want: true},
		{name: "wrong answer", got: "48", expected: "47", want: false},
		{name: "unknown is wrong", got: "UNKNOWN", expected: "kestrel-47", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameAnswer(tt.got, tt.expected); got != tt.want {
				t.Fatalf("sameAnswer(%q, %q) = %t, want %t", tt.got, tt.expected, got, tt.want)
			}
		})
	}
}

func TestExtractSkillName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		arguments string
		toolName  string
		want      string
	}{
		{name: "valid tool call", arguments: `{"name":"web-research"}`, toolName: "activate_skill", want: "web-research"},
		{name: "wrong function", arguments: `{"name":"web-research"}`, toolName: "other", want: ""},
		{name: "fallback malformed arguments", arguments: `activate golang-testing`, toolName: "activate_skill", want: "golang-testing"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractSkillName(tt.arguments, tt.toolName); got != tt.want {
				t.Fatalf("extractSkillName(%q, %q) = %q, want %q", tt.arguments, tt.toolName, got, tt.want)
			}
		})
	}
}
