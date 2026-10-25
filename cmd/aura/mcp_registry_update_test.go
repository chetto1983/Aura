package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
)

// slowLoads makes every registry read take a while, so two writers that did not wait for each
// other would both read the document before either saved it.
func slowLoads(t *testing.T) {
	t.Helper()
	prev := loadManagedMCPConfig
	t.Cleanup(func() { loadManagedMCPConfig = prev })
	loadManagedMCPConfig = func() (mcp.ManagedConfig, error) {
		doc, err := prev()
		time.Sleep(30 * time.Millisecond)
		return doc, err
	}
}

func remoteServer(name string) mcp.ManagedServer {
	return mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://" + name + ".example/mcp"}
}

func TestUpdateManagedMCPConfigSerializesWriters(t *testing.T) {
	withMemoryMCPRegistry(t)
	slowLoads(t)
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b"} {
		wg.Go(func() {
			err := updateManagedMCPConfig(context.Background(), nil, mcpmanager.MCPAuditInsert{Action: "add", ServerName: name},
				func(doc *mcp.ManagedConfig) error {
					doc.MCPServers[name] = remoteServer(name)
					return nil
				})
			if err != nil {
				t.Errorf("update %s: %v", name, err)
			}
		})
	}
	wg.Wait()
	doc := readMCPRegistry(t)
	for _, name := range []string{"a", "b"} {
		if _, ok := doc.MCPServers[name]; !ok {
			t.Errorf("%s was lost to a concurrent write", name)
		}
	}
}

func TestUpdateManagedMCPConfigSavesNothingWhenTheChangeFails(t *testing.T) {
	withMemoryMCPRegistry(t)
	refused := errors.New("refused")
	err := updateManagedMCPConfig(context.Background(), nil, mcpmanager.MCPAuditInsert{}, func(doc *mcp.ManagedConfig) error {
		doc.MCPServers["half"] = remoteServer("half")
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("update = %v, want the change's error", err)
	}
	if _, saved := readMCPRegistry(t).MCPServers["half"]; saved {
		t.Error("a refused change was saved")
	}
}

// An MCP install verifies for up to 120 s before it saves. A plugin install that saves in the
// meantime must not be erased by it: a save removes every server absent from its document.
func TestMCPInstallDuringAPluginInstallKeepsBoth(t *testing.T) {
	withMemoryMCPRegistry(t)
	entered, release := make(chan struct{}), make(chan struct{})
	prev := mcpInstallGuard
	t.Cleanup(func() { mcpInstallGuard = prev })
	mcpInstallGuard = func(_ context.Context, _ *mcpenv.Preparer, name string, s mcp.ManagedServer, _ mcp.BoxLauncher) (mcp.ManagedServer, mcpenv.Report, *mcp.ProbeResult, error) {
		close(entered)
		<-release
		return s, mcpenv.Report{}, &mcp.ProbeResult{Name: name, OK: true}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := (mcpWriteAdapter{}).InstallServer(context.Background(), "admin",
			agui.MCPInstallRequest{Name: "mine", URL: "https://mine.example/mcp"})
		done <- err
	}()
	<-entered
	err := pluginRegistry{}.Update(context.Background(), "admin", "plugin-install", "p", "plugin install o/r/p@x",
		func(doc *mcp.ManagedConfig) error {
			doc.MCPServers["plugged"] = remoteServer("plugged")
			return nil
		})
	if err != nil {
		t.Fatalf("plugin update: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("InstallServer: %v", err)
	}
	doc := readMCPRegistry(t)
	for _, name := range []string{"mine", "plugged"} {
		if _, ok := doc.MCPServers[name]; !ok {
			t.Errorf("%s is missing from the registry", name)
		}
	}
}

// The name is checked again inside the lock: a plugin may have taken it during the verify.
func TestMCPInstallRechecksTheNameAfterTheVerify(t *testing.T) {
	withMemoryMCPRegistry(t)
	prev := mcpInstallGuard
	t.Cleanup(func() { mcpInstallGuard = prev })
	mcpInstallGuard = func(_ context.Context, _ *mcpenv.Preparer, name string, s mcp.ManagedServer, _ mcp.BoxLauncher) (mcp.ManagedServer, mcpenv.Report, *mcp.ProbeResult, error) {
		seedMCPRegistry(t, mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{name: remoteServer("taken")}})
		return s, mcpenv.Report{}, &mcp.ProbeResult{Name: name, OK: true}, nil
	}
	_, err := (mcpWriteAdapter{}).InstallServer(context.Background(), "admin", agui.MCPInstallRequest{Name: "mine", URL: "https://mine.example/mcp"})
	if !errors.Is(err, agui.ErrMCPServerExists) {
		t.Fatalf("InstallServer = %v, want ErrMCPServerExists", err)
	}
}
