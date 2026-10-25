package main

import (
	"context"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/settings"
)

const (
	visionCapabilityTimeout = 5 * time.Second
	visionCapabilityTTL     = time.Minute
)

// liveVision reads each upload image with the chat route the operator runs right now, so a
// model switched in the cockpit reaches the summary without a restart (spec §4.4, amended
// 2026-10-25). The sidecar's URL, model and the timeout stay boot settings.
type liveVision struct {
	cfg     *config.Config
	creds   settings.Credentials
	runtime *llm.Runtime
	plan    *chatgptplan.Service

	mu    sync.Mutex
	route liveVisionRoute
}

// liveVisionRoute is what one runtime snapshot reads images with. The reader, the capability
// source and the key all come from the same snapshot, so they never disagree; the source keeps
// its own cache between switches.
type liveVisionRoute struct {
	version uint64
	llm     llm.Config
	source  llm.ContentCapabilitySource
	reader  multimodal.ImageReader
}

func liveImageReader(chat *chatEnv) *liveVision {
	return &liveVision{cfg: chat.cfg, creds: chat.credentials, runtime: chat.llmRuntime, plan: chat.chatGPTPlan}
}

func (v *liveVision) Describe(ctx context.Context, image []byte, mimeType, prompt string) (text, model string, err error) {
	route := v.current()
	probeCtx, cancel := context.WithTimeout(ctx, visionCapabilityTimeout)
	acceptsImages := multimodal.PrimaryAcceptsImages(probeCtx, route.source)
	cancel()
	cfg := *v.cfg
	cfg.LLM = route.llm
	visionCfg := multimodal.VisionConfigFrom(&cfg, acceptsImages, v.creds.PrimaryRouteKey(route.llm.Provider, route.llm.BaseURL))
	visionCfg.PrimaryReader = route.reader
	return multimodal.NewVisionClient(visionCfg).Describe(ctx, image, mimeType, prompt)
}

// current rebuilds the route when the runtime has published a newer one. A read that took its
// snapshot before a concurrent read rebuilt the route uses the newer route, never the older.
func (v *liveVision) current() liveVisionRoute {
	snapshot := v.runtime.Snapshot()
	v.mu.Lock()
	defer v.mu.Unlock()
	if snapshot.Version > v.route.version {
		v.route = liveVisionRoute{
			version: snapshot.Version,
			llm:     snapshot.Config,
			source:  llm.NewContentCapabilitySource(snapshot.Config, visionCapabilityTTL),
			reader:  chatGPTImageReader(snapshot.Config, v.plan),
		}
	}
	return v.route
}
