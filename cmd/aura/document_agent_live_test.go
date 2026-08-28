//go:build document_live_e2e

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/agenteval"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

type recordingDocumentLibrary struct {
	inner tools.DocumentLibrary
	mu    sync.Mutex
	calls []documents.RetrievalRequest
}

func (l *recordingDocumentLibrary) Retrieve(
	ctx context.Context, request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	l.mu.Lock()
	l.calls = append(l.calls, request)
	l.mu.Unlock()
	return l.inner.Retrieve(ctx, request)
}

func (l *recordingDocumentLibrary) requests() []documents.RetrievalRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]documents.RetrievalRequest(nil), l.calls...)
}

func requiredDocumentLiveEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("document_live_e2e requires %s", name)
	}
	return value
}

func TestDocumentProductionAgentE2E(t *testing.T) {
	identityA := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_IDENTITY_A")
	identityB := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_IDENTITY_B")
	markerA := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_MARKER_A")
	markerB := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_MARKER_B")
	requiredDocumentLiveEnv(t, "OPENROUTER_API_KEY")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	for label, identityID := range map[string]string{"a": identityA, "b": identityB} {
		registerDocumentE2EIdentity(t, ctx, pool, identityID, label)
	}

	library := newDocumentLibrary(pool, cfg)
	assertDocumentMarker(t, library, identityA, "private launch code", markerA)
	assertDocumentMarker(t, library, identityB, "private launch code", markerB)
	foreign, err := library.Retrieve(identityctx.WithIdentityID(ctx, identityA), documents.RetrievalRequest{
		IdentityID: identityA, Query: markerB, Limit: 8,
	})
	if err != nil {
		t.Fatalf("cross-identity probe: %v", err)
	}
	if responseContains(foreign, markerB) {
		t.Fatalf("identity A recalled identity B marker %q", markerB)
	}

	prompt := "Use document_search to answer this question from my uploaded alpha document: " +
		"what is the private launch code? Return the code exactly."
	reply, sawSearch, calls := runDocumentAgentTurn(t, ctx, pool, cfg, identityA, prompt)
	if !sawSearch {
		t.Fatal("real agent did not execute document_search")
	}
	if !strings.Contains(reply, markerA) || strings.Contains(reply, markerB) {
		t.Fatalf("agent reply did not stay in identity A: %q", reply)
	}
	if len(calls) == 0 {
		t.Fatal("document_search did not reach the production library")
	}
	for _, call := range calls {
		if call.IdentityID != identityA {
			t.Fatalf("agent search used identity %q, want %q", call.IdentityID, identityA)
		}
	}
}

func TestMediaDocumentProductionAgentE2E(t *testing.T) {
	identityID := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_MEDIA_IDENTITY")
	requiredDocumentLiveEnv(t, "OPENROUTER_API_KEY")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	registerDocumentE2EIdentity(t, ctx, pool, identityID, "media")

	if _, err := newRuntimeDocumentIndex(cfg, nil, true); err != nil {
		t.Fatalf("configure media document index: %v", err)
	}
	library := newDocumentLibrary(pool, cfg)
	assertDocumentMarker(t, library, identityID, "customer reconciliation", "Pilot workspace")
	assertDocumentMarker(t, library, identityID, "progetto Fenice", "approvazione manuale")
	prompt := "Use document_search on my uploaded image and audio. State exactly the workflow name, " +
		"the pilot workspace option, the expired status, the spoken project, and the approval phrase."
	reply, sawSearch, calls := runDocumentAgentTurn(t, ctx, pool, cfg, identityID, prompt)
	if !sawSearch || len(calls) == 0 {
		t.Fatalf("media agent did not use production document_search: saw=%v calls=%d", sawSearch, len(calls))
	}
	normalized := strings.ToLower(reply)
	for _, marker := range []string{
		"customer-reconciliation", "pilot workspace", "expired", "fenice", "approvazione manuale",
	} {
		if !strings.Contains(normalized, marker) {
			t.Fatalf("media agent score below 10.0; missing %q in reply %q", marker, reply)
		}
	}
	for _, call := range calls {
		if call.IdentityID != identityID {
			t.Fatalf("media search used identity %q, want %q", call.IdentityID, identityID)
		}
	}
	t.Log("real-agent multimodal score: 10.0/10 (>9.8 PASS)")
}

func registerDocumentE2EIdentity(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, label string,
) {
	t.Helper()
	name := fmt.Sprintf("document-e2e-%s-%s@example.test", label, identityID[:8])
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`,
		identityID, name,
	); err != nil {
		t.Fatalf("insert identity %s: %v", label, err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, grantErr := tx.Exec(ctx,
			`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, 'agent.run')`,
			identityID,
		)
		return grantErr
	}); err != nil {
		t.Fatalf("grant identity %s: %v", label, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1::uuid`, identityID)
	})
}

func runDocumentAgentTurn(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg *config.Config,
	identityID, prompt string,
) (string, bool, []documents.RetrievalRequest) {
	t.Helper()
	recorded := &recordingDocumentLibrary{inner: newDocumentLibrary(pool, cfg)}
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&tools.DocumentSearch{Library: recorded})
	registry.Register(&tools.ReadToolOutput{})
	runDir := t.TempDir()
	conv := conversations.New(pool, conversations.Config{RunDir: runDir, TurnCapBytes: 65_536})
	pause := askuser.New(pool)
	r := runner.New(runner.Deps{
		Conv: conv, Pause: pause, ApprovalExpiry: pause,
		Identity: identity.New(pool), CacheMetrics: cachemetrics.New(pool),
		ToolInvocations: toolinvocations.New(pool),
		Client:          openai_compat.New(cfg.LLM), Registry: registry, LLM: cfg.LLM,
		RunDir: runDir, PreviewCap: 4096, TitleTimeout: 30 * time.Second, StopTimeout: 45 * time.Second,
	})
	ownerCtx := identityctx.WithIdentityID(ctx, identityID)
	conversationID := uuid.Must(uuid.NewV7()).String()
	if _, err := r.NewConversationWithID(ownerCtx, conversationID); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Stop(context.Background(), conversationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id = $1`, conversationID)
	})

	var reply strings.Builder
	sawSearch := false
	for event, turnErr := range r.Turn(ownerCtx, conversationID, &prompt) {
		if turnErr != nil {
			t.Fatalf("agent turn: %v", turnErr)
		}
		if event.LLMResponse != nil {
			reply.WriteString(event.LLMResponse.Content)
		}
		if invocation := event.Actions.ToolInvocation; invocation != nil &&
			invocation.Event == "end" && invocation.ToolName == "document_search" {
			sawSearch = true
		}
	}
	return reply.String(), sawSearch, recorded.requests()
}

func assertDocumentMarker(
	t *testing.T, library *documentLibrary, identityID, query, marker string,
) {
	t.Helper()
	response, err := library.Retrieve(
		identityctx.WithIdentityID(t.Context(), identityID),
		documents.RetrievalRequest{IdentityID: identityID, Query: query, Limit: 8},
	)
	if err != nil {
		t.Fatalf("retrieve for %s: %v", identityID, err)
	}
	if !responseContains(response, marker) {
		t.Fatalf("marker %q absent from identity %s response: %#v", marker, identityID, response)
	}
}

func responseContains(response documents.RetrievalResponse, marker string) bool {
	for _, document := range response.Documents {
		if strings.Contains(document.Card, marker) {
			return true
		}
		for _, passage := range document.Passages {
			if strings.Contains(passage.Text, marker) {
				return true
			}
		}
	}
	return false
}

func TestDocumentCorpusAgentEval(t *testing.T) {
	identityID := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_IDENTITY_A")
	requiredDocumentLiveEnv(t, "OPENROUTER_API_KEY")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// This is a release oracle, not a sampling study. Keep repeated runs comparable
	// while still exercising the configured production model and real Runner.
	cfg.LLM.Temperature = 0
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	name := fmt.Sprintf("document-corpus-eval-%s@example.test", identityID[:8])
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`,
		identityID, name,
	); err != nil {
		t.Fatalf("insert eval identity: %v", err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, grantErr := tx.Exec(ctx,
			`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, 'agent.run')`,
			identityID,
		)
		return grantErr
	}); err != nil {
		t.Fatalf("grant eval identity: %v", err)
	}
	cfg.ObjectStoreEndpoint = requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_OBJECTSTORE_ENDPOINT")
	// The lifecycle already created and populated this disposable bucket. Bind those
	// exact credentials into Aura's production per-identity resolver instead of asking
	// the host-side test process to reach Garage's Compose-only admin hostname.
	cfg.GarageAdminEndpoint = ""
	identityObjects, err := objectstore.NewIdentityStore(pool, cfg.AuthulaSecret, objectstore.Credentials{
		Bucket: cfg.ObjectStoreBucket, AccessKey: cfg.ObjectStoreAccessKey, SecretKey: cfg.ObjectStoreSecretKey,
	}, localSeededIdentityID)
	if err != nil {
		t.Fatalf("build eval object-store resolver: %v", err)
	}
	if err := identityObjects.Put(ctx, identityID,
		requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_BUCKET"),
		requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_ACCESS_KEY"),
		requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_SECRET_KEY"),
	); err != nil {
		t.Fatalf("bind eval object-store credentials: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1::uuid`, identityID)
	})

	recorded := &recordingDocumentLibrary{inner: newDocumentLibrary(pool, cfg)}
	sandboxRouter := buildSandboxRouter(cfg)
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&tools.ToolSearch{Registry: registry})
	registry.Register(&tools.DocumentSearch{Library: recorded})
	registry.Register(&tools.DocumentOpen{
		Documents: newRuntimeDocumentOpener(cfg, pool),
		Router:    sandboxRouter,
	})
	registry.Register(&tools.ReadFile{Router: sandboxRouter})
	registry.Register(&tools.ShellExec{
		Background: tools.NewBackgroundShells(sandboxRouter),
		Approvals:  tools.NewShellApprovals(),
		Router:     sandboxRouter,
	})
	// Keep the production privacy choice observable without making an external request:
	// the real specs are visible, while their nil engines fail if the model calls them.
	registry.Register(&tools.WebSearch{})
	registry.Register(&tools.WebFetch{})
	registry.Register(&tools.ReadToolOutput{})
	runDir := t.TempDir()
	conv := conversations.New(pool, conversations.Config{RunDir: runDir, TurnCapBytes: 65_536})
	pause := askuser.New(pool)
	r := runner.New(runner.Deps{
		Conv: conv, Pause: pause, ApprovalExpiry: pause,
		Identity: identity.New(pool), CacheMetrics: cachemetrics.New(pool),
		ToolInvocations: toolinvocations.New(pool),
		Client:          openai_compat.New(cfg.LLM), Registry: registry, LLM: cfg.LLM,
		RunDir: runDir, PreviewCap: 4096, TitleTimeout: 30 * time.Second, StopTimeout: 45 * time.Second,
	})
	ownerCtx := identityctx.WithIdentityID(ctx, identityID)

	cases := []agenteval.Case{
		{
			Name: "invoice-011", Question: "Usa document_search sui miei documenti. Per la fattura numero 011, " +
				"dammi ID fattura, cliente e totale documento; scrivi l'importo solo con cifre, senza separatori.",
			AnswerContains: []string{"2026_011", "Gialli Import", "9272"},
		},
		{
			Name: "acme-total", Question: "Usa document_search sui miei documenti. Quanto abbiamo fatturato ad ACME " +
				"nel 2026 come totale imponibile? Scrivi l'importo solo con cifre, senza separatori.",
			AnswerContains: []string{"58350"},
		},
		{
			Name: "year-total", Question: "Usa document_search sui miei documenti. Dammi per il 2026 sia l'imponibile " +
				"complessivo sia il totale documenti IVA inclusa; scrivi entrambi solo con cifre, senza separatori.",
			AnswerContains: []string{"167900", "204838"},
		},
		{
			Name: "highest-verdi", Question: "Usa document_search sui miei documenti. Qual e' la fattura piu alta emessa " +
				"a Verdi Costruzioni? Dammi ID, imponibile e totale, con importi solo in cifre senza separatori.",
			AnswerContains: []string{"2026_009", "31200", "38064"},
		},
		{
			Name: "not-paid", Question: "Usa document_search sui miei documenti. Elenca tutti gli ID delle fatture il cui " +
				"stato e' diverso da pagata, incluse contestata e scaduta.",
			AnswerContains: []string{"2026_005", "2026_006", "2026_009", "2026_011", "2026_013"},
		},
		{
			Name: "bianchi-contest", Question: "Usa document_search sui miei documenti. Perche Bianchi ha contestato la " +
				"fattura? Cita l'ID e cosa si attende per correggerla.",
			AnswerContains: []string{"2026_006", "nota di credito"},
		},
		{
			Name: "verdi-payment-terms", Question: "Usa document_search sui miei documenti. Quali sono i termini di pagamento " +
				"di Verdi Costruzioni? Rispondi con cliente e numero di giorni.",
			AnswerContains: []string{"Verdi Costruzioni", "90"},
		},
		{
			Name: "rossi-vat", Question: "Usa document_search sui miei documenti. Qual e' la partita IVA di Rossi SRL?",
			AnswerContains: []string{"IT02345678901"},
		},
		{
			Name: "march-invoices", Question: "Usa document_search sui miei documenti. Elenca tutti gli ID delle fatture " +
				"emesse a marzo 2026.",
			AnswerContains: []string{"2026_005", "2026_006", "2026_007"},
		},
		{
			Name: "full-workbook-aggregate", Question: "Usa document_search per trovare il workbook movimenti analitici, " +
				"poi document_open per aprire il file originale e shell_exec con Python/openpyxl per calcolare la somma di " +
				"Quantita per PrezzoUnitario su tutte le righe in cui Centro e' ALFA e Stato e' VALIDO. " +
				"Non stimare dai passaggi indicizzati. Rispondi con il totale solo in cifre, senza separatori.",
			AnswerContains:       []string{"533030"},
			RequiredTools:        []string{"document_search"},
			RequiredToolSequence: []string{"document_search", "document_open", "shell_exec"},
		},
		{
			Name: "scanned-pdf-ocr", Question: "Usa document_search sul verbale scansionato italiano. " +
				"Dammi codice pratica, importo approvato e responsabile esattamente come compaiono nel documento.",
			AnswerContains: []string{"ITALIA-7391", "48270", "GIULIA BIANCHI"},
		},
	}
	if expected := strings.TrimSpace(os.Getenv("AURA_DOCUMENT_E2E_PRIVATE_WORKBOOK_EXPECTED")); expected != "" {
		cases = append(cases, agenteval.Case{
			Name: "private-workbook-full-file", Question: "Usa document_search per trovare il grande workbook clienti " +
				"caricato, poi document_open e shell_exec con Python/openpyxl sul file originale. Conta tutte le righe " +
				"dati in cui Prov. e' AL, CN oppure AT e la Descriz. Pagamento, dopo trim, e' lunga almeno 20 caratteri. " +
				"Non mostrare dati cliente e non stimare dai passaggi indicizzati. Rispondi solo con il conteggio in cifre.",
			AnswerContains:       []string{expected},
			RequiredTools:        []string{"document_search"},
			RequiredToolSequence: []string{"document_search", "document_open", "shell_exec"},
		})
	}
	if os.Getenv("AURA_DOCUMENT_E2E_PUBLIC_CONSTITUTION") == "1" {
		cases = append(cases, agenteval.Case{
			Name: "public-constitution-pdf", Question: "Usa document_search soltanto per individuare il PDF " +
				"della Costituzione caricato, poi document_open per aprire l'originale e un solo shell_exec con " +
				"pdftotext per cercare la nota (1) dell'articolo 58 nel testo completo. Considera le parole " +
				"sillabate a fine riga. Indica la data della legge costituzionale che ha modificato l'articolo e " +
				"il numero e la data della Gazzetta Ufficiale. Rispondi solo con questi dati.",
			AnswerContains:       []string{"18 ottobre 2021", "251", "20 ottobre 2021"},
			RequiredTools:        []string{"document_search"},
			RequiredToolSequence: []string{"document_search", "document_open", "shell_exec"},
		})
	}
	for i := range cases {
		if len(cases[i].RequiredTools) == 0 {
			cases[i].RequiredTools = []string{"document_search"}
		}
		cases[i].ForbiddenTools = []string{"web_search", "web_fetch"}
		// The complete production-agent measurement peaked at seven durable
		// assistant turns. Nine leaves bounded recovery room without rejecting
		// a correct multi-document answer or permitting an unbounded search loop.
		cases[i].MaxLLMCalls = 9
	}

	for _, evalCase := range cases {
		t.Run(evalCase.Name, func(t *testing.T) {
			conversationID := uuid.Must(uuid.NewV7()).String()
			if _, err := r.NewConversationWithID(ownerCtx, conversationID); err != nil {
				t.Fatalf("create conversation: %v", err)
			}
			t.Cleanup(func() {
				_ = r.Stop(context.Background(), conversationID)
				_, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id = $1`, conversationID)
			})

			prompt := evalCase.Question
			for _, turnErr := range r.Turn(ownerCtx, conversationID, &prompt) {
				if turnErr != nil {
					t.Fatalf("agent turn: %v", turnErr)
				}
			}
			history, err := conv.LoadHistory(ownerCtx, conversationID)
			if err != nil {
				t.Fatalf("load durable evidence: %v", err)
			}
			evidence := agenteval.EvidenceFromMessages(history)
			t.Logf("answer=%q calls=%d tools=%v", evidence.Answer, evidence.LLMCalls, evidence.Tools)
			for _, failure := range evalCase.Check(evidence) {
				t.Error(failure)
			}
		})
	}
}
