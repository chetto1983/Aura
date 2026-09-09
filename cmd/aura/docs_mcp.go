package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This server exists so an MCP client can exercise the document tools THE AGENT RUNS.
// It therefore serves those tools themselves, taken from the production registry: the
// name, the description and the parameter schema come from each tool's own Spec(), and a
// call goes straight to its Execute with the arguments the client sent. Nothing is written
// twice here, so there is nothing that can drift.
//
// It used to be a second implementation -- hand-written descriptions over handlers that
// rebuilt `aura docs` CLI flags -- and the two drifted, as a copy does. Measured
// 2026-09-09: this server's document_search said only "Retrieve full passage evidence,
// citations and index status", naming neither citation_token, nor requires_open, nor the
// neighbours parameter, which its schema did not even offer, while the agent-side
// description explained all three. A client validating that server validated the copy.
//
// It serves read_tool_output alongside them for the same reason. A result larger than the
// preview cap is truncated on a rune boundary and spilled to a sidecar, and its footer
// tells the reader to page the rest back with read_tool_output -- so a server that did not
// publish that tool named one its client could not call. Measured 2026-09-09:
// document_search with neighbours:2 over the default limit produced 54,936 bytes, of which
// the client got 30,000 ending mid-JSON, and the structured content decoded from it was
// therefore nothing. Serving the whole result here instead would have been the easier fix
// and the wrong one: the cap is what the AGENT meets, so lifting it for this transport
// would measure an answer the agent never receives.
const (
	documentToolPrefix = "document_"
	// readToolOutputName is the paging tool every truncation footer names.
	readToolOutputName = "read_tool_output"
)

// docsMCPSessionID is a valid, reserved UUID identifying this server's tool calls,
// mirroring toolPipeSessionID: conversation-aware tools parse the session as a UUID.
const docsMCPSessionID = "00000000-0000-0000-0000-000000000043"

// docsMCPCallSeq names each call. The id becomes the sidecar file name when a result is
// larger than the preview cap, so two calls must not share one.
var docsMCPCallSeq atomic.Uint64

type docsMCPIngestInput struct {
	Path     string `json:"path" jsonschema:"Path to an existing file inside Aura's configured workspace"`
	SourceID string `json:"source_id,omitempty" jsonschema:"Optional stable source identifier"`
}

func runDocsMCP(ctx context.Context, factory docsServiceFactory) error {
	cfg := config.LoadDB()
	runtime, err := openProductionToolPipeRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close() }()
	server, err := newDocsMCPServer(
		identityctx.IdentityID(runtime.Context), runtime.Registry, cfg, factory,
	)
	if err != nil {
		return err
	}
	return server.Run(runtime.Context, &mcp.StdioTransport{})
}

func newDocsMCPServer(
	operatorID string,
	registry *tools.Registry,
	cfg *config.Config,
	factory docsServiceFactory,
) (*mcp.Server, error) {
	if strings.TrimSpace(operatorID) == "" || registry == nil || cfg == nil || factory == nil {
		return nil, fmt.Errorf(
			"documents MCP requires the operator identity, the agent tool registry, " +
				"the loaded configuration and the document service",
		)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "aura-documents", Version: "1.0.0"}, &mcp.ServerOptions{
		// What a RESULT means belongs to the tools, not here: a client shows these
		// instructions once and a model reads a tool description every time it considers
		// calling it. This says only what is true of the server itself.
		Instructions: "This server exposes the operator's document library through Aura's own " +
			"agent tools, under one fixed operator identity. Ingest with document_ingest, " +
			"find with document_search, and read the file itself with document_open. Accepting an " +
			"ingest is not the same as having indexed it: document_search is what reports the " +
			"indexed passages and any degradation. A result too large for the preview cap comes " +
			"back truncated, with a footer naming the sidecar holding the rest: page it with " +
			"read_tool_output until the output is complete, which is what the agent itself does.",
	})
	served, err := addNativeTools(server, operatorID, registry, cfg)
	if err != nil {
		return nil, err
	}
	if !served["document_ingest"] {
		addDocsMCPIngest(server, operatorID, factory)
	}
	return server, nil
}

// docsMCPServes selects what this server publishes: the document tools it exists for, plus
// the one that reads back whatever they spilled. read_tool_output is not a document tool and
// is served anyway, because a truncated document result cannot be finished without it.
func docsMCPServes(name string) bool {
	return strings.HasPrefix(name, documentToolPrefix) || name == readToolOutputName
}

// addNativeTools publishes EVERY document tool the agent holds, whatever it is: the
// registry is the inventory, so one added to the agent tomorrow is served here without this
// file being touched. Sorted by name only so the manifest does not shuffle between runs --
// a map's order would.
func addNativeTools(
	server *mcp.Server,
	operatorID string,
	registry *tools.Registry,
	cfg *config.Config,
) (map[string]bool, error) {
	native := map[string]tools.Tool{}
	for _, tool := range registry.All() {
		if name := tool.Spec().Name; docsMCPServes(name) {
			native[name] = tool
		}
	}
	served := make(map[string]bool, len(native))
	for _, name := range slices.Sorted(maps.Keys(native)) {
		tool := native[name]
		spec := tool.Spec()
		var schema jsonschema.Schema
		if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
			return nil, fmt.Errorf("documents MCP: %s parameters: %w", name, err)
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: &schema,
			// The effect hints are the tool's own descriptors too, not a second opinion
			// about it: a client is told what the runtime itself believes.
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    !spec.Mutating,
				DestructiveHint: &spec.Destructive,
				OpenWorldHint:   new(false),
			},
		}, docsMCPNativeHandler(operatorID, cfg, tool))
		served[name] = true
	}
	return served, nil
}

// docsMCPNativeHandler hands the client's arguments to the tool unshaped and its result
// back unshaped. The tool-call context is the agent's own -- same run directory, same
// preview cap -- so a result too large for the model is exactly as large here.
func docsMCPNativeHandler(
	operatorID string,
	cfg *config.Config,
	tool tools.Tool,
) mcp.ToolHandlerFor[json.RawMessage, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, any, error) {
		ctx = tools.WithToolCallContext(
			identityctx.WithIdentityID(ctx, operatorID),
			docsMCPSessionID,
			fmt.Sprintf("docs-mcp-%d", docsMCPCallSeq.Add(1)),
			cfg.RunDir,
			cfg.ToolPreviewCap,
		)
		result, err := tool.Execute(ctx, args)
		if err != nil {
			return nil, nil, err
		}
		// The preview goes back verbatim, footer and all: a retained result says how many
		// bytes were kept and a spilled one says how to page the rest back, and both of
		// those sentences are part of what the agent is told. Reshaping them into something
		// tidier is the thing this server exists not to do.
		content := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result.Preview}}}
		if structured := docsMCPStructured(result.Preview); structured != nil {
			return content, structured, nil
		}
		return content, nil, nil
	}
}

// docsMCPStructured lifts the JSON document out of a preview so a client gets it as
// structured content too. It decodes the FIRST value rather than the whole string,
// because a preview carries the retained/truncated footer after it; a preview cut mid-JSON
// has no first value and yields nothing, which is the honest answer.
func docsMCPStructured(preview string) map[string]any {
	var structured map[string]any
	if json.NewDecoder(strings.NewReader(preview)).Decode(&structured) != nil {
		return nil
	}
	return structured
}

// addDocsMCPIngest serves the one tool here with no agent twin -- an agent receives
// documents, it does not place them -- over the `aura docs ingest` handler, so its
// description is written here. It says what the others say about what a RESULT means,
// because a client of this server has nothing else to read.
func addDocsMCPIngest(server *mcp.Server, operatorID string, factory docsServiceFactory) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "document_ingest",
		Description: "Places a file that already exists on this server's workspace filesystem into the " +
			"operator's document library, through the same pipeline every other route uses: the bytes are " +
			"stored, extracted, described and chunked, and only then become searchable. " +
			"Returns the asset id, the stored file name and the ingest status. " +
			"That status is ACCEPTANCE, not indexing: a document is not findable the moment this returns, " +
			"and document_search is the only thing that reports whether its passages exist yet. Re-ingesting " +
			"the same path replaces what is there rather than adding a copy. " +
			"path must name an existing file; source_id is an optional stable identifier for the place the " +
			"file came from, and is what keeps a re-ingest of the same origin one document instead of two.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input docsMCPIngestInput) (*mcp.CallToolResult, map[string]any, error) {
		args := []string{"ingest"}
		if input.SourceID != "" {
			args = append(args, "--source-id", input.SourceID)
		}
		args = append(args, "--", input.Path)
		var output bytes.Buffer
		if err := runDocsCommand(identityctx.WithIdentityID(ctx, operatorID), args, &output, factory); err != nil {
			return nil, nil, err
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			return nil, nil, fmt.Errorf("decode document result: %w", err)
		}
		return nil, result, nil
	})
}
