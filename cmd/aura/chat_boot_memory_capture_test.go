package main

import (
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestMemoryCaptureBoot_ComposesExactlyOneQueueAndSink(t *testing.T) {
	memorySource, err := os.ReadFile("chat_boot_memory.go")
	if err != nil {
		t.Fatalf("read chat_boot_memory.go: %v", err)
	}
	bootSource, err := os.ReadFile("chat_boot.go")
	if err != nil {
		t.Fatalf("read chat_boot.go: %v", err)
	}
	memory := string(memorySource)
	boot := string(bootSource)
	compactBoot := strings.Join(strings.Fields(boot), " ")

	for _, contract := range []string{
		"func buildMemoryCaptureQueue(",
		"func (s tenantMemoryCaptureSink) ApplyAcceptedCapture(",
		"client.ApplyAcceptedCapture(ctx, capture)",
	} {
		if !strings.Contains(memory, contract) {
			t.Errorf("chat_boot_memory.go lacks production capture contract %q", contract)
		}
	}
	if got := strings.Count(boot, "buildMemoryCaptureQueue(cfg)"); got != 1 {
		t.Errorf("capture queue construction count = %d, want exactly 1", got)
	}
	for _, contract := range []string{
		"memoryCaptureQueue *runner.MemoryCaptureQueue",
		"MemoryCaptureQueue: memoryCaptureQueue",
		"memoryCaptureQueue: memoryCaptureQueue",
		"e.memoryCaptureQueue.Close(ctx)",
	} {
		if !strings.Contains(compactBoot, contract) {
			t.Errorf("chat_boot.go lacks capture ownership contract %q", contract)
		}
	}
}

func TestMemoryCaptureBoot_ConfiguredAndAbsentMemory(t *testing.T) {
	withoutMemory := &config.Config{}
	if queue := buildMemoryCaptureQueue(withoutMemory); queue != nil {
		t.Fatal("absent memory configuration created a capture fallback")
	}

	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", strings.Repeat("s", 32))
	configured := &config.Config{}
	configured.ArcadeDB.BaseURL = "http://127.0.0.1:2480"
	queue := buildMemoryCaptureQueue(configured)
	if queue == nil {
		t.Fatal("configured identity-scoped memory did not create the capture queue")
	}
	if err := queue.Close(t.Context()); err != nil {
		t.Fatalf("close idle configured queue: %v", err)
	}
}
