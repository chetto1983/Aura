package main

import (
	"os"
	"strings"
	"testing"
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
		if !strings.Contains(boot, contract) {
			t.Errorf("chat_boot.go lacks capture ownership contract %q", contract)
		}
	}
}
