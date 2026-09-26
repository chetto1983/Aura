package mcp

import (
	"strings"
	"testing"
	"time"
)

// A box server is written like any stdio server plus runtime.kind "box". What the write
// path owes it: a command, no credentials (the box drops them and the agent's shell could
// read them), and a default trust class that says it is confined.
func TestManagedConfigValidatesBoxServers(t *testing.T) {
	box := ManagedRuntime{Kind: RuntimeKindBox}
	for name, tc := range map[string]struct {
		server ManagedServer
		want   string
	}{
		"ok":                 {ManagedServer{Command: "agent-browser", Args: []string{"mcp"}, Env: []string{"LANG=C"}, Runtime: box}, ""},
		"no command":         {ManagedServer{Runtime: box, Type: ServerTypeStdio}, "command cannot be empty"},
		"secret in env":      {ManagedServer{Command: "x", Env: []string{"GITHUB_TOKEN=ghp_x"}, Runtime: box}, "box servers take no secrets"},
		"unknown kind":       {ManagedServer{Command: "x", Runtime: ManagedRuntime{Kind: "vm"}}, "unknown runtime kind"},
		"init timeout":       {ManagedServer{Command: "uvx", Args: []string{"mcp-server-fetch"}, Runtime: ManagedRuntime{Kind: RuntimeKindBox, InitTimeoutSec: 90}}, ""},
		"local init timeout": {ManagedServer{Command: "uvx", Runtime: ManagedRuntime{InitTimeoutSec: 90}}, "only a box server's first start"},
		"negative timeout":   {ManagedServer{Command: "uvx", Runtime: ManagedRuntime{Kind: RuntimeKindBox, InitTimeoutSec: -1}}, "between 1 and 600"},
		"huge timeout":       {ManagedServer{Command: "uvx", Runtime: ManagedRuntime{Kind: RuntimeKindBox, InitTimeoutSec: 601}}, "between 1 and 600"},
	} {
		doc := ManagedConfig{MCPServers: map[string]ManagedServer{"s": tc.server}}
		err := PrepareForWrite(&doc)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: PrepareForWrite = %v, want accepted", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: PrepareForWrite = %v, want %q", name, err, tc.want)
		}
	}
}

func TestIsBoxRuntimeAndItsDefaultTrust(t *testing.T) {
	box := ManagedServer{Command: "agent-browser", Runtime: ManagedRuntime{Kind: " box "}}
	if !IsBoxRuntime(box) {
		t.Fatal("a stdio server with runtime kind box is not a box server")
	}
	for name, s := range map[string]ManagedServer{
		"local":          {Command: "uvx"},
		"explicit local": {Command: "uvx", Runtime: ManagedRuntime{Kind: RuntimeKindLocal}},
		"http":           {Type: ServerTypeStreamableHTTP, URL: "https://x.test/mcp", Runtime: ManagedRuntime{Kind: RuntimeKindBox}},
	} {
		if IsBoxRuntime(s) {
			t.Errorf("IsBoxRuntime(%s) = true", name)
		}
	}
	if _, trust, err := Classify(box); err != nil || trust != TrustSandboxedLocal {
		t.Fatalf("box server trust = %q, %v; want %q", trust, err, TrustSandboxedLocal)
	}
	stated := box
	stated.Trust.Class = TrustTrustedLocal
	if _, trust, _ := Classify(stated); trust != TrustTrustedLocal {
		t.Fatalf("an explicit trust class lost to the box default: %q", trust)
	}
}

func TestBoxInitTimeout(t *testing.T) {
	box := ManagedServer{Command: "uvx", Runtime: ManagedRuntime{Kind: RuntimeKindBox}}
	if got := BoxInitTimeout(box); got != DefaultBoxInitTimeout {
		t.Fatalf("undeclared = %v, want the default %v", got, DefaultBoxInitTimeout)
	}
	box.Runtime.InitTimeoutSec = 90
	if got := BoxInitTimeout(box); got != 90*time.Second {
		t.Fatalf("declared 90 = %v", got)
	}
	if got := BoxInitTimeout(ManagedServer{Command: "uvx"}); got != 0 {
		t.Fatalf("a local server = %v, want none", got)
	}
}
