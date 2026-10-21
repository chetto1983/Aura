package documents

import (
	"context"
	"encoding/json"
	"math"
	"path"
	"strconv"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func (p *CraftPreviews) renderVideoPreview(ctx context.Context, h usersandbox.BoxHandle, dir, source string) error {
	cli := "ulimit -f 131072 && filmcraft-cli --data-dir " + dir
	run := func(arguments string) ([]byte, error) {
		return p.box.RunPreview(ctx, h, usersandbox.ExecRequest{Command: cli + " " + arguments, Dir: dir})
	}
	metadata, err := run("probe " + source)
	if err != nil {
		return err
	}
	var probe struct {
		Video    struct{ Width, Height float64 } `json:"video"`
		Duration float64                         `json:"duration"`
	}
	if json.Unmarshal(metadata, &probe) != nil {
		return ErrPreviewInvalid
	}
	w, ht := probe.Video.Width, probe.Video.Height
	if w <= 0 || ht <= 0 || math.IsNaN(w) || math.IsNaN(ht) || math.IsInf(w, 0) || math.IsInf(ht, 0) || math.Trunc(w) != w || math.Trunc(ht) != ht {
		return ErrPreviewInvalid
	}
	if w > 32768 || ht > 32768 || w > 67108864/ht {
		return ErrPreviewTooLarge
	}
	if probe.Duration <= 0 || math.IsNaN(probe.Duration) || math.IsInf(probe.Duration, 0) {
		return ErrPreviewInvalid
	}
	project := path.Join(dir, "project.fcproj")
	imported, err := run("--save-as " + project + " import " + source)
	if err != nil {
		return err
	}
	var result struct {
		Items  []uint64          `json:"items"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(imported, &result) != nil || len(result.Errors) != 0 || len(result.Items) != 1 || result.Items[0] == 0 {
		return ErrPreviewInvalid
	}
	params, _ := json.Marshal(map[string]any{"name": "Preview", "fromItem": result.Items[0]})
	if _, err := run("--project " + project + " --save exec file.newSequence '" + string(params) + "'"); err != nil {
		return err
	}
	// FilmCraft's native timebase is 254016000000 ticks/s. Avoid the black opening
	// frame where possible, using a tenth of the duration, capped at five seconds.
	seconds := min(5, probe.Duration/254016000000/10)
	scale := float64(previewEdge) / max(w, ht)
	_, err = run("--project " + project + " render --seconds " + strconv.FormatFloat(seconds, 'g', -1, 64) + " --scale " + strconv.FormatFloat(scale, 'g', -1, 64) + " --out " + path.Join(dir, "preview.png"))
	return err
}
