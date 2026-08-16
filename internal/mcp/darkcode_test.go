// darkcode_test.go is the guard that makes MCPC-03's "deleted, not left dormant"
// checkable rather than aspirational. TestNoHandRolledJSONRPCFraming walks internal/
// and cmd/ and fails the build on any non-test .go file whose CODE (comments
// stripped) contains a token naming a symbol this phase deleted.
//
// Scope decisions, stated so a future reader does not mistake them for a gap:
//
//   - Test files are excluded from the walk. The SDK-era test fixtures this phase's
//     own plan 45.1-01/02/03 wrote (mcp_status_test.go, mcp_test.go, ...) legitimately
//     construct HTTP responses carrying "Mcp-Session-Id" and route on
//     "notifications/initialized" — they are emulating a THIRD-PARTY peer speaking
//     the pre-2026-07-28 wire dialect, which is exactly what the SDK client must stay
//     compatible with (RESEARCH Correction 3, D-105's mixed-fleet concern). That is
//     categorically different from AURA reimplementing a hand-rolled client, which is
//     what this guard exists to catch.
//   - Comments are stripped before matching (go/scanner, COMMENT tokens dropped).
//     internal/mcp/sdkclient.go documents that SEP-2575 removes the Mcp-Session-Id
//     header, and result.go documents what DecodeToolResult replaced — both name a
//     deleted symbol in PROSE, which CLAUDE.md's documentation-first rule wants
//     (cite the surprising behaviour), not in code. Rewriting that prose to dodge a
//     naive substring match would be exactly the "special-casing" the plan's own
//     doc comment warns is how a guard gets quietly disabled; making the guard
//     comment-aware is the more honest fix.
//
// The token list is a FLOOR, not a CEILING: it catches the specific symbols deleted
// in this plan, and a future hand-rolled framing helper under an unpredicted name
// would pass it silently. The real guarantee is the prohibition recorded in this
// plan's must_haves, which a reviewer enforces; a reviewer who sees framing-shaped
// code in a diff must treat the guard as incomplete, never the code as permitted.
package mcp

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenFramingTokens returns the deleted-symbol token -> description map. Every
// key is built by string concatenation so this file's own source text cannot
// contain a forbidden token as a contiguous substring — a guard whose own token
// list trips it is a guard that has to be special-cased, and special-casing is how
// it later gets disabled.
func forbiddenFramingTokens() map[string]string {
	tokens := map[string]string{}
	tokens["readResponse"+"Context"] = "Client.readResponseContext (internal/mcp/client.go, deleted)"
	tokens["roundtrip"+"Context"] = "Client.roundtripContext (internal/mcp/client.go, deleted)"
	tokens["session"+"Gate"] = "sessionGate (internal/mcp/lifecycle.go, deleted)"
	tokens["newStdio"+"Scanner"] = "newStdioScanner (internal/mcp/client.go, deleted)"
	tokens["rpc"+"Resp"] = "rpcResp (internal/mcp/client.go, deleted)"
	tokens["rpc"+"Req"] = "rpcReq (internal/mcp/client.go, deleted)"
	tokens["httpSSEMax"+"LineBytes"] = "httpSSEMaxLineBytes (internal/mcp/http_client.go, deleted)"
	tokens["Mcp-Session"+"-Id"] = "the Mcp-Session-Id header, hand-tracked (internal/mcp/http_client.go, deleted; SEP-2567 removes it)"
	tokens["notifications/"+"initialized"] = "the initialized handshake notification, hand-sent (internal/mcp/client.go + http_client.go, deleted; SEP-2575 removes the handshake)"
	tokens["callTool"+"With"] = "callToolWith (internal/mcp/tool_methods.go, deleted)"
	tokens["listTools"+"With"] = "listToolsWith (internal/mcp/tool_methods.go, deleted)"
	// Lowercase initial only, compared case-sensitively: the exported
	// DecodeToolResult (result.go) is the SDK-era survivor and must not match.
	tokens["decode"+"ToolResult"] = "decodeToolResult (internal/mcp/client.go, deleted — not the exported DecodeToolResult)"
	return tokens
}

// stripGoComments removes // and /* */ comments from src using go/scanner, so a
// deleted symbol's name in explanatory prose does not read as its code reappearing.
// String and identifier literal text is preserved verbatim, which is what keeps the
// guard able to catch a reintroduced literal like "Mcp-Session-Id" in real code.
func stripGoComments(src []byte) string {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	// A nil error handler makes the scanner skip lexical errors silently rather than
	// panicking — this guard must degrade to "did not strip comments perfectly" on a
	// malformed file, never crash the test run.
	s.Init(file, src, nil, scanner.ScanComments)
	var b strings.Builder
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			continue
		}
		if lit != "" {
			b.WriteString(lit)
		} else {
			b.WriteString(tok.String())
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func TestNoHandRolledJSONRPCFraming(t *testing.T) {
	tokens := forbiddenFramingTokens()
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}

	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			base := filepath.Base(path)
			if !strings.HasSuffix(base, ".go") {
				return nil
			}
			// Excluded by PATH, not by content (per plan): darkcode_test.go names
			// every forbidden token in its own doc comment and token map.
			if base == "darkcode_test.go" {
				return nil
			}
			// Excluded by PATH: every other _test.go legitimately emulates a
			// third-party peer's wire dialect (see the package doc comment above).
			if strings.HasSuffix(base, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			content := stripGoComments(data)
			for tok, deletedSymbol := range tokens {
				if strings.Contains(content, tok) {
					t.Errorf("%s: contains forbidden token %q (stands for %s) — hand-rolled MCP framing must not reappear", path, tok, deletedSymbol)
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}
}
