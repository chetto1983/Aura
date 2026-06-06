package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

// runToolPipe is a NON-LLM harness for measuring Aura's tool-layer latency in
// isolation. It builds the SAME tool registry the agent uses (buildBaseRegistry)
// and executes NDJSON tool calls read from stdin — one JSON object per line,
// {"tool":"fs_write","args":{...}} — printing each call's wall-clock and a total.
// Blank lines and lines starting with '#' are ignored. It deliberately drives the
// real tool Execute paths (fs_write, shell_exec, fs_read, …) with no model loop,
// so the number it prints is pure tool cost, not agent-roundtrip overhead.
func runToolPipe(_ []string) {
	cfg := config.LoadDB()
	reg := buildBaseRegistry(cfg, nil)
	ctx := context.Background()

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // allow large file-content args

	var total time.Duration
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var call struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			fmt.Printf("[parse error] %v\n", err)
			continue
		}
		tool, ok := reg.Get(call.Tool)
		if !ok {
			fmt.Printf("[%s] UNKNOWN TOOL\n", call.Tool)
			continue
		}
		n++
		tctx := tools.WithToolCallContext(ctx, "toolpipe", fmt.Sprintf("pipe-%d", n), cfg.RunDir, cfg.ToolPreviewCap)
		start := time.Now()
		res, err := tool.Execute(tctx, []byte(call.Args))
		dur := time.Since(start)
		total += dur
		if err != nil {
			fmt.Printf("[%s] ERR %dms: %v\n", call.Tool, dur.Milliseconds(), err)
			continue
		}
		preview := res.Preview
		if len(preview) > 500 {
			preview = preview[:500] + "…"
		}
		fmt.Printf("[%s] OK %dms: %s\n", call.Tool, dur.Milliseconds(), preview)
	}
	fmt.Printf("=== TOTAL TOOL TIME: %dms across %d calls ===\n", total.Milliseconds(), n)
}
