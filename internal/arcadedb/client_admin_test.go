package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCypherAndScriptMethodsUseTheirEndpointAndLanguage(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[{"ok":true}]}`}
	})
	ctx := context.Background()
	if _, err := client.Read(ctx, "MATCH (n) RETURN n", nil); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := client.Write(ctx, "CREATE (n:X)", nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := client.Script(ctx, "BEGIN; SELECT 1; COMMIT;", nil); err != nil {
		t.Fatalf("Script: %v", err)
	}
	want := []struct{ path, language string }{
		{"/api/v1/query/aura", "cypher"},
		{"/api/v1/command/aura", "cypher"},
		{"/api/v1/command/aura", "sqlscript"},
	}
	for i, expected := range want {
		if (*requests)[i].Path != expected.path || (*requests)[i].Payload["language"] != expected.language {
			t.Fatalf("request %d = %+v", i, (*requests)[i])
		}
	}
}

func TestDatabaseAndUserLifecycleUseServerCommands(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{}`}
	})
	ctx := context.Background()
	if _, err := client.CreateDatabase(ctx, "mem_a"); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if _, err := client.DropDatabase(ctx, "mem_a"); err != nil {
		t.Fatalf("DropDatabase: %v", err)
	}
	if err := client.CreateUser(ctx, "u_a", "pw", map[string][]string{"mem_a": {"admin"}}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := client.DropUser(ctx, "u_a"); err != nil {
		t.Fatalf("DropUser: %v", err)
	}
	commands := []string{
		"create database mem_a", "drop database mem_a", "create user ", "drop user u_a",
	}
	for i, prefix := range commands {
		request := (*requests)[i]
		if request.Path != "/api/v1/server" || !strings.HasPrefix(request.Payload["command"].(string), prefix) || request.Auth == "" {
			t.Fatalf("request %d = %+v", i, request)
		}
	}
	if command := (*requests)[2].Payload["command"].(string); !strings.Contains(command, `"databases":{"mem_a":["admin"]}`) {
		t.Fatalf("scoped user command = %s", command)
	}
}

func TestServerCommandErrorsAreNotReportedAsSuccess(t *testing.T) {
	srv := fakeServer(t, http.StatusForbidden, `{"detail":"root required"}`, nil)
	if _, err := mustClient(t, srv.URL).CreateDatabase(context.Background(), "mem_a"); err == nil || !strings.Contains(err.Error(), "root required") {
		t.Fatalf("server refusal = %v", err)
	}
	url := srv.URL
	srv.Close()
	if _, err := mustClient(t, url).DropDatabase(context.Background(), "mem_a"); err == nil {
		t.Fatal("unreachable server reported success")
	}
}

func TestServerVersionAndSecurityBoundary(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "patched", status: http.StatusOK, body: `{"version":"26.7.3 (build x)"}`},
		{name: "affected", status: http.StatusOK, body: `{"version":"26.4.1"}`, wantErr: true},
		{name: "unreadable", status: http.StatusOK, body: `{"version":"unknown"}`, wantErr: true},
		{name: "malformed body", status: http.StatusOK, body: `{"version":`, wantErr: true},
		{name: "refused", status: http.StatusForbidden, body: `{"detail":"denied"}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var seen captured
			srv := fakeServer(t, tt.status, tt.body, &seen)
			client := mustClient(t, srv.URL)
			err := client.VerifySecureVersion(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%t", err, tt.wantErr)
			}
			if seen.path != "/api/v1/server" || seen.auth == "" {
				t.Fatalf("version request = %+v", seen)
			}
		})
	}
}

func TestWithEmbedderHandlesNilAndServerErrorKindFallback(t *testing.T) {
	var none *Client
	if none.WithEmbedder(nil) != nil {
		t.Fatal("nil client became non-nil")
	}
	client := mustClient(t, "http://example.test")
	embedder := &stubEmbedder{}
	if client.WithEmbedder(embedder) != client || client.embedder != embedder {
		t.Fatal("embedder not attached")
	}
	if got := (&ServerError{Status: 500, Kind: "failure"}).Error(); !strings.Contains(got, "failure") {
		t.Fatalf("Error() = %q", got)
	}
}
