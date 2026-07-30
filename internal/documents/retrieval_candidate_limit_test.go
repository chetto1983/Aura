package documents

import "testing"

func TestRetrievalCandidateLimitBoundsRerankWorkToRequestedResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "default", limit: 0, want: 10},
		{name: "small", limit: 1, want: 8},
		{name: "typical", limit: 5, want: 8},
		{name: "default explicit", limit: 8, want: 10},
		{name: "large", limit: 13, want: 15},
		{name: "maximum", limit: 20, want: 15},
		{name: "clamped", limit: 99, want: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := retrievalCandidateLimit(test.limit); got != test.want {
				t.Fatalf(
					"retrievalCandidateLimit(%d) = %d, want %d",
					test.limit, got, test.want,
				)
			}
		})
	}
}
