//go:build mcp_live_integration

package webauth

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/testenv"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

const (
	liveCalendarResource = "http://127.0.0.1:8093/"
	liveWhatsAppResource = "http://127.0.0.1:8092/mcp/"
	liveMemoryResource   = "http://127.0.0.1:8096/mcp/"

	liveWhatsAppSubjectA = "d1111111-1111-4111-8111-111111111111"
	liveWhatsAppSubjectB = "d2222222-2222-4222-8222-222222222222"

	// The PIM refuses a tenant-written account that reads the server's own disk (fork
	// c5db95e), so the calendar accounts are the operator's: the PIM under test runs with
	// internal/mcp/testdata/pim-appsettings.json, which gives each subject one fixture account.
	liveCalendarSubjectA = "00000000-0000-0000-0000-000000000001"
	liveCalendarSubjectB = "00000000-0000-0000-0000-000000000002"
	liveCalendarAccountA = "00000000000000000000000000000001__fixture"
	liveCalendarAccountB = "00000000000000000000000000000002__fixture"
)

func TestMCPResourcesLiveCrossSubjectIsolation(t *testing.T) {
	ignoreExisting := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, ignoreExisting) })
	t.Cleanup(http.DefaultClient.CloseIdleConnections)

	tokens := newLiveTokenIssuer(t)
	t.Run("calendar", func(t *testing.T) { testLiveCalendarIsolation(t, tokens) })
	t.Run("whatsapp", func(t *testing.T) { testLiveWhatsAppIsolation(t, tokens) })
	t.Run("memory", func(t *testing.T) { testLiveMemoryIsolation(t, tokens) })
}

func newLiveTokenIssuer(t *testing.T) *LiveMCPTokenIssuer {
	t.Helper()
	issuer, err := NewLiveMCPTokenIssuer(
		testenv.Require(t, "AURA_AUTHULA_DSN"),
		testenv.Require(t, "AURA_AUTHULA_SECRET"),
	)
	if err != nil {
		t.Fatalf("production MCP token issuer: %v", err)
	}
	t.Cleanup(func() {
		if err := issuer.Close(); err != nil {
			t.Errorf("close production MCP token issuer: %v", err)
		}
	})
	return issuer
}

func liveSubjectToken(t *testing.T, tokens *LiveMCPTokenIssuer, resource, subject string) string {
	t.Helper()
	token, err := tokens.AccessToken(t.Context(), resource, subject)
	if err != nil {
		t.Fatalf("issue production Authula token for %s: %v", resource, err)
	}
	return token
}

func testLiveCalendarIsolation(t *testing.T, tokens *LiveMCPTokenIssuer) {
	tokenA := liveSubjectToken(t, tokens, liveCalendarResource, liveCalendarSubjectA)
	tokenB := liveSubjectToken(t, tokens, liveCalendarResource, liveCalendarSubjectB)
	sessionA := openLiveResourceSessionWithToken(t, liveCalendarResource, tokenA, "calendar-live-a")
	sessionB := openLiveResourceSessionWithToken(t, liveCalendarResource, tokenB, "calendar-live-b")

	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	resultA := awaitLiveToolMarker(t, ctx, sessionA, "list_accounts", map[string]any{}, liveCalendarAccountA)
	resultB := awaitLiveToolMarker(t, ctx, sessionB, "list_accounts", map[string]any{}, liveCalendarAccountB)
	assertLiveMarkersIsolated(t, "Calendar", resultA, resultB, liveCalendarAccountA, liveCalendarAccountB)
}

func openLiveResourceSessionWithToken(t *testing.T, resource, token, name string) *sdkmcp.ClientSession {
	t.Helper()
	managed := auramcp.ManagedServer{
		Type:  auramcp.ServerTypeStreamableHTTP,
		URL:   resource,
		Env:   []string{"MCP_BEARER_TOKEN=" + token},
		Trust: auramcp.ManagedTrust{Class: auramcp.TrustTrustedRecipe},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	session, err := auramcp.OpenSDKSession(ctx, name, managed, auramcp.EgressPolicy{}, auramcp.SessionOptions{})
	if err != nil {
		t.Fatalf("initialize %s at %s: %v", name, resource, err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close %s: %v", name, err)
		}
	})
	return session
}

func testLiveWhatsAppIsolation(t *testing.T, tokens *LiveMCPTokenIssuer) {
	root := testenv.Require(t, "AURA_WHATSAPP_STORE_ROOT")
	tokenA := liveSubjectToken(t, tokens, liveWhatsAppResource, liveWhatsAppSubjectA)
	tokenB := liveSubjectToken(t, tokens, liveWhatsAppResource, liveWhatsAppSubjectB)
	sessionA := openLiveResourceSessionWithToken(t, liveWhatsAppResource, tokenA, "whatsapp-live-a")
	sessionB := openLiveResourceSessionWithToken(t, liveWhatsAppResource, tokenB, "whatsapp-live-b")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	startLiveWhatsAppTenant(t, liveWhatsAppSubjectA)
	startLiveWhatsAppTenant(t, liveWhatsAppSubjectB)
	markerA := "live-wa-a-" + uuid.NewString()
	markerB := "live-wa-b-" + uuid.NewString()
	seedLiveWhatsAppChat(t, ctx, root, liveWhatsAppSubjectA, markerA)
	seedLiveWhatsAppChat(t, ctx, root, liveWhatsAppSubjectB, markerB)
	t.Cleanup(func() { deleteLiveWhatsAppChat(t, root, liveWhatsAppSubjectA, markerA) })
	t.Cleanup(func() { deleteLiveWhatsAppChat(t, root, liveWhatsAppSubjectB, markerB) })

	resultA := callLiveToolText(t, ctx, sessionA, "list_chats", map[string]any{"include_last_message": false})
	resultB := callLiveToolText(t, ctx, sessionB, "list_chats", map[string]any{"include_last_message": false})
	assertLiveMarkersIsolated(t, "WhatsApp", resultA, resultB, markerA, markerB)
}

func startLiveWhatsAppTenant(t *testing.T, subject string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://127.0.0.1:8094/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testenv.Require(t, "WHATSAPP_BRIDGE_TOKEN"))
	request.Header.Set("X-Tenant-ID", subject)
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("start WhatsApp tenant %s: %v", subject, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("start WhatsApp tenant %s: HTTP %d: %s", subject, response.StatusCode, readLiveBody(response.Body))
	}
}

func seedLiveWhatsAppChat(t *testing.T, ctx context.Context, root, subject, marker string) {
	t.Helper()
	db := openLiveWhatsAppDB(t, root, subject)
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		marker+"@s.whatsapp.net", marker, nil); err != nil {
		t.Fatalf("seed WhatsApp tenant %s: %v", subject, err)
	}
}

func deleteLiveWhatsAppChat(t *testing.T, root, subject, marker string) {
	t.Helper()
	db := openLiveWhatsAppDB(t, root, subject)
	defer db.Close()
	if _, err := db.Exec("DELETE FROM chats WHERE jid = ?", marker+"@s.whatsapp.net"); err != nil {
		t.Errorf("delete WhatsApp marker for %s: %v", subject, err)
	}
}

func openLiveWhatsAppDB(t *testing.T, root, subject string) *sql.DB {
	t.Helper()
	path := filepath.Join(root, "tenants", subject, "store", "messages.db")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("WhatsApp tenant database was not created: %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open WhatsApp tenant database %s: %v", subject, err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func testLiveMemoryIsolation(t *testing.T, tokens *LiveMCPTokenIssuer) {
	subjectA := uuid.NewString()
	subjectB := uuid.NewString()
	t.Cleanup(func() { cleanupLiveMemoryTenants(t, subjectA, subjectB) })
	tokenA := liveSubjectToken(t, tokens, liveMemoryResource, subjectA)
	tokenB := liveSubjectToken(t, tokens, liveMemoryResource, subjectB)
	sessionA := openLiveResourceSessionWithToken(t, liveMemoryResource, tokenA, "memory-live-a")
	sessionB := openLiveResourceSessionWithToken(t, liveMemoryResource, tokenB, "memory-live-b")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	entityA := "Live memory alpha " + subjectA[:8]
	entityB := "Live memory beta " + subjectB[:8]
	statementA := entityA + " owns the alpha isolation marker."
	statementB := entityB + " owns the beta isolation marker."
	upsertLiveMemoryFact(t, ctx, sessionA, entityA, "alpha isolation marker", statementA, "live-memory-a-message")
	upsertLiveMemoryFact(t, ctx, sessionB, entityB, "beta isolation marker", statementB, "live-memory-b-message")

	resultA := callLiveToolText(t, ctx, sessionA, "memory_facts_about", map[string]any{"entity": entityA})
	resultB := callLiveToolText(t, ctx, sessionB, "memory_facts_about", map[string]any{"entity": entityB})
	assertLiveMarkersIsolated(t, "Memory", resultA, resultB, statementA, statementB)
	foreignA := callLiveToolText(t, ctx, sessionA, "memory_facts_about", map[string]any{"entity": entityB})
	foreignB := callLiveToolText(t, ctx, sessionB, "memory_facts_about", map[string]any{"entity": entityA})
	if strings.Contains(foreignA, statementB) || strings.Contains(foreignB, statementA) {
		t.Fatalf("Memory returned a foreign subject's fact: A=%s B=%s", foreignA, foreignB)
	}

	params := &sdkmcp.CallToolParams{Name: "memory_facts_about", Arguments: map[string]any{"entity": entityA}}
	params.SetMeta(map[string]any{"tenant": subjectB})
	response, err := sessionA.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Memory metadata-forgery probe: %v", err)
	}
	text, isError := auramcp.DecodeToolResult(response)
	if isError || !strings.Contains(text, statementA) || strings.Contains(text, statementB) {
		t.Fatalf("Memory client metadata changed the OAuth subject boundary: %s", text)
	}
}

// The write schema has no run_id since D-10: the memory MCP derives the writing run from the
// Aura-issued token itself (cmd/arcadedb-mcp/tool_memory.go hostDerivedActor).
func upsertLiveMemoryFact(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, subject, object, statement, memoryID string) {
	t.Helper()
	_ = callLiveToolText(t, ctx, session, "memory_upsert_fact", map[string]any{
		"subject": subject, "subject_kind": "test_entity", "predicate": "owns",
		"object": object, "object_kind": "test_marker", "statement": statement,
		"source": map[string]any{"memory_ids": []string{memoryID}},
	})
}

func cleanupLiveMemoryTenants(t *testing.T, subjects ...string) {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_PASSWORD"))
	if password == "" {
		password = strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	}
	if password == "" {
		t.Errorf("clean live Memory tenants: ARCADEDB_ADMIN_PASSWORD is unset")
		return
	}
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: strings.TrimSpace(os.Getenv("ARCADEDB_URL")), Database: "aura_memory",
		User: strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_USER")), Password: password,
		Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Errorf("open ArcadeDB admin for cleanup: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, subject := range subjects {
		database, err := arcadedb.DatabaseFor(subject)
		if err != nil {
			t.Errorf("derive Memory database for cleanup: %v", err)
			continue
		}
		var dropErr error
		if _, err := admin.DropDatabase(ctx, database); err != nil {
			dropErr = err
		}
		exists, err := admin.DatabaseExists(ctx, database)
		if err != nil {
			t.Errorf("verify disposable Memory database %s removed: %v", database, err)
		} else if exists {
			t.Errorf("disposable Memory database %s survived cleanup: %v", database, dropErr)
		}
		_ = admin.DropUser(ctx, arcadedb.TenantUserFor(database))
	}
}

func callLiveToolText(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, name string, arguments map[string]any) string {
	t.Helper()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	text, isError := auramcp.DecodeToolResult(result)
	if isError {
		t.Fatalf("call %s returned isError: %s", name, text)
	}
	return text
}

func awaitLiveToolMarker(
	t *testing.T,
	ctx context.Context,
	session *sdkmcp.ClientSession,
	name string,
	arguments map[string]any,
	marker string,
) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		text := callLiveToolText(t, ctx, session, name, arguments)
		if strings.Contains(text, marker) {
			return text
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not expose %q after its configuration reload: %s", name, marker, text)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertLiveMarkersIsolated(t *testing.T, resource, resultA, resultB, markerA, markerB string) {
	t.Helper()
	if !strings.Contains(resultA, markerA) || strings.Contains(resultA, markerB) {
		t.Fatalf("%s tenant A is not isolated: %s", resource, resultA)
	}
	if !strings.Contains(resultB, markerB) || strings.Contains(resultB, markerA) {
		t.Fatalf("%s tenant B is not isolated: %s", resource, resultB)
	}
}

func readLiveBody(body io.Reader) string {
	contents, err := io.ReadAll(io.LimitReader(body, 8<<10))
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(contents)
}
