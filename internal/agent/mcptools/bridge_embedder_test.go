package mcptools

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

// bagEmbedder is a deterministic, sidecar-free Embedder for the bridge tests: it
// embeds text as an L2-normalized bag-of-words vector over a fixed hashed vocabulary,
// so the cosine between a query and a tool's search document is high exactly when they
// share a token. tool_search ranks semantically (08.2-03), so the reconnect-refresh
// test needs an embedder wired to exercise the free-text path; this keeps it free and
// deterministic (no granite sidecar).
type bagEmbedder struct{}

func (bagEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	const dims = 256
	out := make([][]float64, len(texts))
	for ti, text := range texts {
		v := make([]float64, dims)
		for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
			return !isAlphaNum
		}) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(tok))
			v[h.Sum32()%dims] += 1
		}
		var n float64
		for _, x := range v {
			n += x * x
		}
		if n > 0 {
			n = math.Sqrt(n)
			for i := range v {
				v[i] /= n
			}
		}
		out[ti] = v
	}
	return out, nil
}
