//go:build mcp_live_integration

package webauth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authula "github.com/Authula/authula"
	authulaconfig "github.com/Authula/authula/config"
	authulamodels "github.com/Authula/authula/models"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
)

const (
	liveCalendarResource = "http://127.0.0.1:8093/"
	liveWhatsAppResource = "http://127.0.0.1:8092/mcp/"
	liveMemoryResource   = "http://127.0.0.1:8096/mcp/"

	liveWhatsAppSubjectA = "d1111111-1111-4111-8111-111111111111"
	liveWhatsAppSubjectB = "d2222222-2222-4222-8222-222222222222"
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

func newLiveTokenIssuer(t *testing.T) *mcpTokenPlugin {
	t.Helper()
	dsn, err := ensureAuthulaSearchPath(liveEnvOrGate(t, "AURA_AUTHULA_DSN"))
	if err != nil {
		t.Fatalf("production Authula DSN: %v", err)
	}
	secret := liveEnvOrGate(t, "AURA_AUTHULA_SECRET")
	// Compose only Aura's production token plugin here. Authula v1.42.0's rate-limit
	// providers start cleanup workers whose Close methods are no-ops, so constructing
	// the full web provider would make this integration harness leak by upstream design.
	// https://github.com/Authula/authula/blob/v1.42.0/plugins/rate-limit/services/in_memory_provider.go
	config := authulaconfig.NewConfig(
		authulaconfig.WithAppName("Aura MCP live integration"),
		authulaconfig.WithBasePath(basePath),
		authulaconfig.WithSecret(secret),
		authulaconfig.WithDatabase(authulamodels.DatabaseConfig{
			Provider: "postgres", URL: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		}),
		authulaconfig.WithEventBus(authulamodels.EventBusConfig{
			Provider: "gochannel", GoChannel: &authulamodels.GoChannelConfig{BufferSize: 10},
		}),
		authulaconfig.WithPlugins(authulamodels.PluginsConfig{
			mcpTokenPluginID: map[string]any{"enabled": true},
		}),
	)
	tokens := newMCPTokenPlugin()
	auth := authula.New(&authula.AuthConfig{Config: config, Plugins: []authulamodels.Plugin{tokens}})
	t.Cleanup(func() {
		if err := auth.ClosePlugins(); err != nil {
			t.Errorf("close Authula token plugin: %v", err)
		}
		if err := auth.CloseSystems(); err != nil {
			t.Errorf("close Authula core systems: %v", err)
		}
		if closer, ok := auth.DB().(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				t.Errorf("close Authula token database: %v", err)
			}
		}
	})
	return tokens
}

func liveEnvOrGate(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("mcp_live_integration requires %s under CI", key)
	}
	t.Skipf("set %s and run against the production-like Compose stack", key)
	return ""
}

func liveSubjectToken(t *testing.T, tokens *mcpTokenPlugin, resource, subject string) string {
	t.Helper()
	pair, err := tokens.jwt.GenerateUserToken(t.Context(), "mcp-live-probe", uuid.NewString(), map[string]any{
		"iss":           auramcp.AuraAuthorizationServerIssuer,
		"aud":           resource,
		"scope":         auramcp.AuraOAuthToolsScope,
		"client_id":     "aura",
		mcpSubjectClaim: subject,
	})
	if err != nil {
		t.Fatalf("issue production Authula token for %s: %v", resource, err)
	}
	return pair.AccessToken
}

func testLiveCalendarIsolation(t *testing.T, tokens *mcpTokenPlugin) {
	subjectA := uuid.NewString()
	subjectB := uuid.NewString()
	tokenA := liveSubjectToken(t, tokens, liveCalendarResource, subjectA)
	tokenB := liveSubjectToken(t, tokens, liveCalendarResource, subjectB)
	sessionA := openLiveResourceSessionWithToken(t, liveCalendarResource, tokenA, "calendar-live-a")
	sessionB := openLiveResourceSessionWithToken(t, liveCalendarResource, tokenB, "calendar-live-b")

	accountA := createLiveCalendarAccount(t, tokenA, "live-"+subjectA[:8])
	accountB := createLiveCalendarAccount(t, tokenB, "live-"+subjectB[:8])
	t.Cleanup(func() { deleteLiveCalendarAccount(t, tokenA, accountA) })
	t.Cleanup(func() { deleteLiveCalendarAccount(t, tokenB, accountB) })

	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	resultA := awaitLiveToolMarker(t, ctx, sessionA, "calendar", map[string]any{"action": "list_accounts"}, accountA)
	resultB := awaitLiveToolMarker(t, ctx, sessionB, "calendar", map[string]any{"action": "list_accounts"}, accountB)
	assertLiveMarkersIsolated(t, "Calendar", resultA, resultB, accountA, accountB)
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

func createLiveCalendarAccount(t *testing.T, token, localID string) string {
	t.Helper()
	body := map[string]any{
		"id": localID, "displayName": localID, "provider": "json", "enabled": true, "priority": 1,
		"providerConfig": map[string]string{"source": "local", "filePath": "/app/data/fixture-calendar.json"},
	}
	response := liveBearerJSON(t, http.MethodPost, liveCalendarResource+"admin/accounts", token, body)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create Calendar account %s: HTTP %d: %s", localID, response.StatusCode, readLiveBody(response.Body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil || strings.TrimSpace(created.ID) == "" {
		t.Fatalf("decode created Calendar account %s: id=%q err=%v", localID, created.ID, err)
	}
	return created.ID
}

func deleteLiveCalendarAccount(t *testing.T, token, accountID string) {
	t.Helper()
	response := liveBearerJSON(t, http.MethodDelete,
		liveCalendarResource+"admin/accounts/"+url.PathEscape(accountID), token, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		t.Errorf("delete Calendar account %s: HTTP %d: %s", accountID, response.StatusCode, readLiveBody(response.Body))
	}
}

func liveBearerJSON(t *testing.T, method, endpoint, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	return response
}

func testLiveWhatsAppIsolation(t *testing.T, tokens *mcpTokenPlugin) {
	root := liveEnvOrGate(t, "AURA_WHATSAPP_STORE_ROOT")
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
	request.Header.Set("Authorization", "Bearer "+liveEnvOrGate(t, "WHATSAPP_BRIDGE_TOKEN"))
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

func testLiveMemoryIsolation(t *testing.T, tokens *mcpTokenPlugin) {
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
	upsertLiveMemoryFact(t, ctx, sessionA, entityA, "alpha isolation marker", statementA, "live-memory-a")
	upsertLiveMemoryFact(t, ctx, sessionB, entityB, "beta isolation marker", statementB, "live-memory-b")

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

func upsertLiveMemoryFact(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, subject, object, statement, runID string) {
	t.Helper()
	_ = callLiveToolText(t, ctx, session, "memory_upsert_fact", map[string]any{
		"subject": subject, "subject_kind": "test_entity", "predicate": "owns",
		"object": object, "object_kind": "test_marker", "statement": statement,
		"source": map[string]any{"run_id": runID, "memory_ids": []string{runID + "-message"}},
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
