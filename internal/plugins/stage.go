package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
)

// Where a plugin lives in a box: its tree, mirrored by the sandbox, and its data directory on
// the identity's workspace volume, which outlives the box container.
const (
	BoxPluginsRoot = "/plugins"
	BoxDataRoot    = "/workspace/.plugin-data"
	// boxInitTimeoutSec bounds a stdio server's first start in a box, which may fetch it
	// (`uvx`, `npx -y`) into that identity's caches.
	boxInitTimeoutSec = 120
)

var errPlaceholder = errors.New("no endpoint declared (placeholder)")

// DataDir is a plugin's PLUGIN_DATA in the box. The suffix hashes the source, so a plugin of
// the same name from another publisher never receives the previous one's data (spec §6b).
func DataDir(name string, ref packs.Ref) string {
	sum := sha256.Sum256([]byte(ref.String()))
	return BoxDataRoot + "/" + name + "-" + hex.EncodeToString(sum[:])[:12]
}

// Expand replaces ${PLUGIN_ROOT} and ${PLUGIN_DATA} in one pass, so text a replacement brings
// in is never expanded again, and any other ${…} stays literal (Agent Plugins §9.2).
func Expand(s, root, data string) string {
	return strings.NewReplacer("${PLUGIN_ROOT}", root, "${PLUGIN_DATA}", data).Replace(s)
}

// StageServer turns one of a plugin's connectors into the registry entry Aura saves. A stdio
// server runs in the calling identity's box, in the plugin root; a remote one keeps its URL.
// The entry then passes the same write checks as any other, so a shape the registry would
// refuse is refused here, at review.
func StageServer(s packs.Server, plugin string, ref packs.Ref, trust mcp.ManagedTrust) (mcp.ManagedServer, error) {
	transport, err := transportOf(s)
	if err != nil {
		return mcp.ManagedServer{}, err
	}
	server := mcp.ManagedServer{Type: transport, Enabled: new(true), Source: packs.SourceMarker(ref), Trust: trust}
	if transport == mcp.ServerTypeStdio {
		err = stageStdio(&server, s, plugin, ref)
	} else {
		err = stageRemote(&server, s)
	}
	if err != nil {
		return mcp.ManagedServer{}, err
	}
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{s.Name: server}}
	if err := mcp.PrepareForWrite(&doc); err != nil {
		return mcp.ManagedServer{}, err
	}
	return server, nil
}

// transportOf reads the declared transport, inferring a remote one from a bare URL as the
// registry does (mcp.Classify), and refusing an entry that declares nothing to reach.
func transportOf(s packs.Server) (string, error) {
	hasURL, hasCmd := strings.TrimSpace(s.URL) != "", strings.TrimSpace(s.Command) != ""
	switch {
	case !hasURL && !hasCmd:
		return "", errPlaceholder
	case strings.TrimSpace(s.Type) != "":
		return packs.NormalizeTransport(s.Type)
	case hasURL && hasCmd:
		return "", errors.New("declares both a url and a command, and no type to choose between them")
	case hasURL:
		return mcp.ServerTypeStreamableHTTP, nil
	}
	return mcp.ServerTypeStdio, nil
}

func stageStdio(server *mcp.ManagedServer, s packs.Server, plugin string, ref packs.Ref) error {
	root := BoxPluginsRoot + "/" + plugin
	data := DataDir(plugin, ref)
	command := s.Command
	if rest, ok := strings.CutPrefix(command, "./"); ok {
		command = pathpkg.Join(root, rest)
		if !within(command, root) {
			return fmt.Errorf("command %q leaves the plugin root", s.Command)
		}
	}
	dir := root
	if s.Cwd != "" {
		dir = Expand(s.Cwd, root, data)
		if !pathpkg.IsAbs(dir) {
			dir = pathpkg.Join(root, dir)
		}
		dir = pathpkg.Clean(dir)
		if !within(dir, root) && !within(dir, data) {
			return fmt.Errorf("cwd %q is neither in the plugin root nor in its data directory", s.Cwd)
		}
	}
	env, err := stageEnv(s.Env, root, data)
	if err != nil {
		return err
	}
	args := make([]string, 0, len(s.Args))
	for _, a := range s.Args {
		args = append(args, Expand(a, root, data))
	}
	server.Command = command
	server.Args = args
	server.Env = env
	server.Runtime = mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox, InitTimeoutSec: boxInitTimeoutSec, Dir: dir, DataDir: data}
	return nil
}

// stageEnv is the declared env, sorted and expanded, then PLUGIN_ROOT and PLUGIN_DATA. A plugin
// may not set those two itself: a process reads the first of two equal names.
func stageEnv(declared map[string]string, root, data string) ([]string, error) {
	keys := make([]string, 0, len(declared))
	for k := range declared {
		if k == "PLUGIN_ROOT" || k == "PLUGIN_DATA" {
			return nil, fmt.Errorf("env sets %s, which Aura sets", k)
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	env := make([]string, 0, len(keys)+2)
	for _, k := range keys {
		env = append(env, k+"="+Expand(declared[k], root, data))
	}
	return append(env, "PLUGIN_ROOT="+root, "PLUGIN_DATA="+data), nil
}

// stageRemote keeps the URL. Declared headers refuse the connector: Aura's static-header
// transport sends them on every request with no origin check, and Agent Plugins §7.2.1 forbids
// forwarding them to another origin.
func stageRemote(server *mcp.ManagedServer, s packs.Server) error {
	if len(s.Headers) > 0 {
		return errors.New("declares headers, which Aura cannot hold to the server's origin")
	}
	server.URL = s.URL
	return nil
}

func within(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+"/")
}
