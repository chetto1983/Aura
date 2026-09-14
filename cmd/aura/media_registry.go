package main

import "github.com/chetto1983/aura/internal/agent/tools"

// mediaToolHandles retains the media generation tools so serve boot can inject their
// live dependencies. Every other registry keeps the zero values: the tools stay
// discoverable and refuse at call time.
type mediaToolHandles struct {
	ImageGenerate *tools.ImageGenerate
}

func registerMediaTools(reg *tools.Registry) mediaToolHandles {
	handles := mediaToolHandles{ImageGenerate: &tools.ImageGenerate{}}
	reg.Register(handles.ImageGenerate)
	return handles
}
