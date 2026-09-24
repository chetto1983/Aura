package main

import (
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

type runtimeToolHandles struct {
	BackgroundShells *tools.BackgroundShells
	ShellApprovals   *tools.ShellApprovals
	// SkillManage is retained so chat boot can attach the live identity capability
	// checker to the exact process-global catalog writer registered for agent turns.
	SkillManage   *tools.SkillManageTool
	Memory        *mcptools.MountedServer
	MemoryContext *mountedMemoryContext
	// ShellPoll / ShellKill are retained so serve boot can wire their .Caps to the live
	// capability store (VERIF-7 / D-18): the pool-free manifest paths construct them with a
	// nil Caps (owner-only fail-closed), and serve.go sets Caps = the identity store once it
	// exists, making the admin cross-session poll/kill recovery exemption reachable.
	ShellPoll *tools.ShellPoll
	ShellKill *tools.ShellKill
	// SendFile is retained so serve boot can wire its .Assets to the live *assets.Service
	// (VERIF-7 / WEBART-01): the pool-free manifest / CLI paths construct it with a nil Assets
	// (path-only degrade, D-02), and serve.go sets Assets = the asset service once buildAssetService
	// has run, so an authenticated channel-driven delivery becomes an owned Garage asset.
	SendFile *tools.SendFile
	// MCPViews is the process-wide MCP Apps document catalog the mounts fill, and
	// ViewCallers maps ONLY the servers that actually catalogued a document to their
	// mounted supervisor — so a view's callback can never name a server that never
	// served it one. Both are nil on the pool-free manifest paths, which render
	// nothing; every *ViewCatalog method tolerates that.
	MCPViews    *mcp.ViewCatalog
	ViewCallers mcptools.ViewCallers
	// MCPFiles materializes the files an MCP tool result carries into the calling
	// turn's box. Nil on the pool-free manifest paths, which mount no MCP server.
	MCPFiles mcptools.FileSink
	// Documents is retained so chat boot can route its query embedder through the live LLM key
	// once the runtime exists (wireDocumentQueryEmbedder).
	Documents *documentLibrary
	mediaToolHandles
}
