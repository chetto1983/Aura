package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

type AgentJobWikiReader interface {
	ReadPage(slug string) (*wiki.Page, error)
}

type AgentJobSourceReader interface {
	Get(id string) (*source.Source, error)
}

type AgentJobTaskReader interface {
	GetByName(ctx context.Context, name string) (*Task, error)
}

type AgentJobWakeDeps struct {
	Wiki    AgentJobWikiReader
	Sources AgentJobSourceReader
	Tasks   AgentJobTaskReader
}

func AgentJobWakeSignature(ctx context.Context, payload AgentJobPayload, deps AgentJobWakeDeps) (string, bool) {
	if len(payload.WakeIfChanged) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(payload.WakeIfChanged))
	for _, signal := range payload.WakeIfChanged {
		if part, ok := agentJobWakePart(ctx, signal, deps); ok {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), true
}

func agentJobWakePart(ctx context.Context, signal string, deps AgentJobWakeDeps) (string, bool) {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return "", false
	}
	if slug, ok := agentJobWikiSignalSlug(signal); ok {
		if isNilWakeDep(deps.Wiki) {
			return "", false
		}
		page, err := deps.Wiki.ReadPage(slug)
		if err != nil {
			return "wiki:" + slug + ":missing", true
		}
		return fmt.Sprintf("wiki:%s:%s:%s:%s", slug, page.UpdatedAt, page.Title, strings.Join(page.Related, ",")), true
	}
	if id, ok := strings.CutPrefix(signal, "source:"); ok {
		if isNilWakeDep(deps.Sources) {
			return "", false
		}
		id = strings.TrimSpace(id)
		src, err := deps.Sources.Get(id)
		if err != nil {
			return "source:" + id + ":missing", true
		}
		return fmt.Sprintf("source:%s:%s:%s:%d:%s:%s", src.ID, src.SHA256, src.Status, src.PageCount, strings.Join(src.WikiPages, ","), src.Error), true
	}
	if name, ok := AgentJobTaskAnchor(signal); ok {
		if isNilWakeDep(deps.Tasks) {
			return "", false
		}
		task, err := deps.Tasks.GetByName(ctx, name)
		if err != nil {
			return "task:" + name + ":missing", true
		}
		return fmt.Sprintf("task:%s:%s:%s:%s", task.Name, task.LastRunAt.UTC().Format(time.RFC3339), task.WakeSignature, task.LastError), true
	}
	return "", false
}

func isNilWakeDep(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func agentJobWikiSignalSlug(signal string) (string, bool) {
	if slug, ok := strings.CutPrefix(signal, "wiki:"); ok {
		slug = strings.TrimSpace(slug)
		return slug, slug != ""
	}
	if strings.HasPrefix(signal, "[[") && strings.HasSuffix(signal, "]]") {
		slug := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(signal, "[["), "]]"))
		return slug, slug != ""
	}
	return "", false
}

func AgentJobTaskAnchor(anchor string) (string, bool) {
	anchor = strings.TrimSpace(anchor)
	for _, prefix := range []string{"task:", "agent_job:"} {
		if name, ok := strings.CutPrefix(anchor, prefix); ok {
			name = strings.TrimSpace(name)
			return name, name != ""
		}
	}
	if anchor == "" || strings.Contains(anchor, ":") || strings.HasPrefix(anchor, "[[") {
		return "", false
	}
	return anchor, true
}
