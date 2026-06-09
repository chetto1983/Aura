package eval

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChat50PromptFixtureContract(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "scripts", "fixtures", "chat_50_prompts.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open prompt fixture: %v", err)
	}
	defer f.Close() //nolint:errcheck

	var count, todayCount, toolCount int
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			t.Fatalf("fixture line must be id<TAB>flags<TAB>prompt, got %q", line)
		}
		id, flags, prompt := fields[0], fields[1], fields[2]
		if seen[id] {
			t.Fatalf("duplicate prompt id %q", id)
		}
		seen[id] = true
		if strings.TrimSpace(prompt) == "" {
			t.Fatalf("prompt %q is empty", id)
		}
		lower := strings.ToLower(prompt)
		for _, banned := range []string{"chain of thought", "catena di pensiero", "ragionamento interno", "cot"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("prompt %q asks for hidden reasoning via %q", id, banned)
			}
		}
		if strings.Contains(","+flags+",", ",today,") {
			todayCount++
		}
		if strings.Contains(","+flags+",", ",tool,") {
			toolCount++
		}
		count++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan prompt fixture: %v", err)
	}
	if count != 50 {
		t.Fatalf("prompt count = %d, want 50", count)
	}
	if todayCount < 10 {
		t.Fatalf("today prompt count = %d, want at least 10", todayCount)
	}
	if toolCount < 5 {
		t.Fatalf("tool-candidate prompt count = %d, want at least 5", toolCount)
	}
}
