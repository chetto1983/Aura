package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

func wikiSubgraphDeltaCase(stamp string) Case {
	safeStamp := wiki.Slug(stamp)
	fixture := &wikiSubgraphDeltaFixture{
		customerSlug: "probe-customer-delta-" + safeStamp,
		supplierSlug: "probe-supplier-acme-" + safeStamp,
		clientCode:   "PROBE_CODE_615827_" + safeStamp,
	}
	return Case{
		Name:     "wiki-subgraph-delta-automazioni",
		Category: "tools-wiki",
		Prompt: fmt.Sprintf(
			"Usa search con action=subgraph, query %q e depth 1. Dammi il codice cliente di Delta Automazioni.",
			"codice cliente delta automazioni "+safeStamp,
		),
		Setup:   fixture.setup,
		Verify:  fixture.verify,
		Cleanup: fixture.cleanup,
	}
}

type wikiSubgraphDeltaFixture struct {
	customerSlug string
	supplierSlug string
	clientCode   string
	startedAt    time.Time
}

func (f *wikiSubgraphDeltaFixture) setup(env *Env) error {
	now := time.Now().UTC().Format(time.RFC3339)
	customerBody, err := wiki.MarshalMD(&wiki.Page{
		Title:         "Delta Automazioni",
		Body:          fmt.Sprintf("Delta Automazioni è un cliente. Codice cliente: %s. [[%s]]", f.clientCode, f.supplierSlug),
		Category:      "customer",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		return fmt.Errorf("marshal customer page: %w", err)
	}
	supplierBody, err := wiki.MarshalMD(&wiki.Page{
		Title:         "Acme Supplier",
		Body:          fmt.Sprintf("Acme fornisce a [[%s]].", f.customerSlug),
		Category:      "supplier",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		return fmt.Errorf("marshal supplier page: %w", err)
	}
	f.startedAt = time.Now().UTC().Add(-2 * time.Second)
	if err := env.writeWikiFile(f.customerSlug+".md", string(customerBody)); err != nil {
		return fmt.Errorf("write customer page: %w", err)
	}
	if err := env.writeWikiFile(f.supplierSlug+".md", string(supplierBody)); err != nil {
		return fmt.Errorf("write supplier page: %w", err)
	}
	return nil
}

func (f *wikiSubgraphDeltaFixture) verify(r ChatReply, env *Env) []string {
	var miss []string
	if r.ToolCalls == 0 {
		miss = append(miss, "expected search action=subgraph tool call, got 0")
	}
	if !strings.Contains(r.Reply, f.clientCode) {
		miss = append(miss, fmt.Sprintf("reply missing client code %q", f.clientCode))
	}
	if !strings.Contains(strings.ToLower(r.Reply), "delta") {
		miss = append(miss, "reply missing 'delta'")
	}
	if env == nil || env.DB == nil {
		miss = append(miss, "DB unavailable for tool_attempts ground truth")
		return miss
	}
	outcomes, err := env.toolAttemptOutcomeCountsSince(f.startedAt, "search")
	if err != nil {
		miss = append(miss, fmt.Sprintf("tool_attempts outcome query: %v", err))
		return miss
	}
	if outcomes["ok"] == 0 {
		miss = append(miss, fmt.Sprintf("tool_attempts missing ok search row since %s", f.startedAt.Format(time.RFC3339Nano)))
	}
	for _, outcome := range []string{"recoverable", "error"} {
		if outcomes[outcome] > 0 {
			miss = append(miss, fmt.Sprintf("search tool_attempts had %d %s row(s) since %s", outcomes[outcome], outcome, f.startedAt.Format(time.RFC3339Nano)))
		}
	}
	return miss
}

func (f *wikiSubgraphDeltaFixture) cleanup(env *Env) {
	_ = env.deleteWikiFile(f.customerSlug + ".md")
	_ = env.deleteWikiFile(f.supplierSlug + ".md")
}
