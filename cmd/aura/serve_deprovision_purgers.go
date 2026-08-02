package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/google/uuid"
)

var (
	_ agui.ConversationPurger = conversationPurgeAdapter{}
	_ agui.GraphPurger        = memoryGraphPurgeAdapter{}
	_ agui.MemoryPurger       = arcadeMemoryPurgeAdapter{}
)

type conversationPurgeStore interface {
	PurgeConversations(context.Context, string) error
}

type conversationPurgeAdapter struct {
	store conversationPurgeStore
}

func (a conversationPurgeAdapter) PurgeConversations(ctx context.Context, identityID string) error {
	if a.store == nil {
		return fmt.Errorf("conversation purge is not configured")
	}
	return a.store.PurgeConversations(ctx, identityID)
}

type memoryGraphClient interface {
	Read(context.Context, string, map[string]any) ([]map[string]any, error)
	Write(context.Context, string, map[string]any) ([]map[string]any, error)
	Close() error
}

type memoryGraphPurgeAdapter struct {
	cfg  *knowledge.Config
	open func(context.Context, *knowledge.Config) (memoryGraphClient, error)
}

func openMemoryGraphClient(ctx context.Context, cfg *knowledge.Config) (memoryGraphClient, error) {
	return knowledge.Open(ctx, cfg)
}

func (a memoryGraphPurgeAdapter) PurgeGraph(ctx context.Context, identityID string) error {
	if _, err := uuid.Parse(identityID); err != nil {
		return fmt.Errorf("memory graph purge identity %q: %w", identityID, err)
	}
	if a.cfg == nil {
		return fmt.Errorf("memory graph purge is not configured")
	}
	open := a.open
	if open == nil {
		open = openMemoryGraphClient
	}
	client, err := open(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("open memory graph purge client: %w", err)
	}
	defer func() { _ = client.Close() }()

	params := map[string]any{"identity_id": identityID}
	if _, err := client.Write(ctx, memoryGraphPurgeQuery, params); err != nil {
		return fmt.Errorf("purge memory graph: %w", err)
	}
	rows, err := client.Read(ctx, memoryGraphPurgeVerifyQuery, params)
	if err != nil {
		return fmt.Errorf("verify memory graph purge: %w", err)
	}
	if len(rows) != 1 || integerResult(rows[0]["remaining"]) != 0 {
		return fmt.Errorf("verify memory graph purge: owner-reachable data remains")
	}
	return nil
}

// arcadeMemoryPurgeAdapter erases one identity's long-term memory. Memory is one
// ArcadeDB database and one server user per identity, so the purge is two server
// commands rather than a sweep that has to find everything memory ever wrote.
//
// It carries no client for the tenant: the probe below is built per call, because
// its whole purpose is to be REFUSED, and a cached client would prove nothing
// about the credential the server holds now.
type arcadeMemoryPurgeAdapter struct {
	admin *arcadedb.Client
	// base has no credential. The admin one drops; the tenant one is derived per
	// call to be refused. Storing either in base would invite using the wrong one.
	base        arcadedb.Config
	credentials *arcadedb.TenantCredentials
}

// PurgeMemory drops the identity's database and its server user, then PROVES both.
//
// The commands' exit codes are deliberately not the assertion. ArcadeDB refuses a
// drop of something already gone — dropUser throws "User 'x' not found on server"
// — and the saga journal re-runs a step that failed, so a resumed purge would fail
// forever on work it had already finished. Nor is a successful exit proof: it says
// the server accepted a command, not that the data is unreachable. Both legs
// therefore run the command, keep its error only for the message, and assert the
// postcondition by reading the server back.
func (a arcadeMemoryPurgeAdapter) PurgeMemory(ctx context.Context, identityID string) error {
	if a.admin == nil || a.credentials == nil {
		return fmt.Errorf("memory purge is not configured")
	}
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		return fmt.Errorf("memory purge: %w", err)
	}
	if err := a.dropDatabase(ctx, database); err != nil {
		return err
	}
	return a.dropUser(ctx, database)
}

func (a arcadeMemoryPurgeAdapter) dropDatabase(ctx context.Context, database string) error {
	_, dropErr := a.admin.DropDatabase(ctx, database)
	exists, err := a.admin.DatabaseExists(ctx, database)
	if err != nil {
		return fmt.Errorf("verify memory database %s erased: %w", database, err)
	}
	if exists {
		return fmt.Errorf("verify memory database %s erased: the server still holds it%s",
			database, becauseOf(dropErr))
	}
	return nil
}

// dropUser removes the tenant's server user and proves it by BINDING as that user
// and being refused. ArcadeDB publishes no list-users endpoint, so there is no
// read that answers "does this user exist"; a negative bind is the only proof
// available.
//
// It is not ceremony. The user name and its password are both derived — from the
// identity UUID and from the database name — so a surviving credential regains
// full access the moment that UUID's database comes back, and at least one
// identity UUID is fixed (localSeededIdentityID). A restore from backup or an
// operator re-seed would make a stale credential live again.
func (a arcadeMemoryPurgeAdapter) dropUser(ctx context.Context, database string) error {
	user := arcadedb.TenantUserFor(database)
	dropErr := a.admin.DropUser(ctx, user)

	probeCfg := a.base
	// The database is already gone; this names the credential's own database only
	// so the config reads honestly. The probe hits /api/v1/ready, which is a
	// server endpoint and never dereferences it.
	probeCfg.Database = database
	probeCfg.User = user
	probeCfg.Password = a.credentials.PasswordFor(database)
	probe, err := arcadedb.New(probeCfg)
	if err != nil {
		return fmt.Errorf("build memory credential probe for %s: %w", user, err)
	}
	accepted, err := probe.CredentialAccepted(ctx)
	if err != nil {
		return fmt.Errorf("verify memory credential %s revoked: %w", user, err)
	}
	if accepted {
		return fmt.Errorf("verify memory credential %s revoked: the server still accepts it%s",
			user, becauseOf(dropErr))
	}
	return nil
}

// becauseOf appends a failed command's own error to a postcondition failure. The
// command error alone is not a verdict, but when the postcondition also fails it
// is usually the reason.
func becauseOf(err error) string {
	if err == nil {
		return ""
	}
	return " (drop: " + err.Error() + ")"
}

// buildArcadeMemoryPurger wires the purger, or returns nil when the server-rights
// credential or the tenant derivation secret is missing.
//
// Nil, never a no-op: a purger that silently did nothing would let the identity
// row be deleted — cascading the whole Postgres catalog — while the ArcadeDB
// database survived with no owner row left pointing at it. The de-provisioning
// preflight refuses identity deletion when this is nil, which is the loud failure
// an operator can act on.
func buildArcadeMemoryPurger(cfg *config.Config) agui.MemoryPurger {
	if cfg == nil {
		return nil
	}
	server := cfg.ArcadeDB
	if strings.TrimSpace(server.AdminUser) == "" || strings.TrimSpace(server.AdminPassword) == "" {
		slog.Warn("aura serve: no ArcadeDB server credential — memory purge disabled, de-provisioning will refuse identity deletion")
		return nil
	}
	base := arcadedb.Config{BaseURL: server.BaseURL, Database: server.Database}
	adminCfg := base
	adminCfg.User = server.AdminUser
	adminCfg.Password = server.AdminPassword
	admin, err := arcadedb.New(adminCfg)
	if err != nil {
		slog.Warn("aura serve: ArcadeDB server credential unusable — memory purge disabled", "error", err)
		return nil
	}
	// Without the derivation secret the purge could still drop the user, but it
	// could not prove the drop took — and an unprovable erasure is not one.
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("aura serve: no ArcadeDB tenant secret — memory purge disabled", "error", err)
		return nil
	}
	return arcadeMemoryPurgeAdapter{admin: admin, base: base, credentials: credentials}
}

func integerResult(value any) int64 {
	switch n := value.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return -1
	}
}

// memoryGraphPurgeQuery is the NEO4J plane, which is now two things: the document
// graph, still written live by internal/documents/indexer.go, and the memory
// subgraph written there before memory moved to ArcadeDB. The memory labels stay
// in this query on purpose — identities provisioned back then have that residue in
// Neo4j today, and trimming the labels before a verified residue sweep would mean
// every later de-provision walked past it.
//
// The owner anchor is detached last. Nodes shared through another explicit
// ownership edge survive; private dependants are removed with their parent.
const memoryGraphPurgeQuery = `
OPTIONAL MATCH (u:User {identifier: $identity_id})
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_CONVERSATION]->(c:Conversation)
  WHERE c IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_CONVERSATION]->(c)
      WHERE other <> u
    }
  OPTIONAL MATCH (c)-[:HAS_MESSAGE]->(m:Message)
  RETURN collect(DISTINCT c) AS conversations, collect(DISTINCT m) AS messages
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_PREFERENCE]->(p:Preference)
  WHERE p IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_PREFERENCE]->(p)
      WHERE other <> u
    }
  RETURN collect(DISTINCT p) AS preferences
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_FACT]->(f:Fact)
  WHERE f IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_FACT]->(f)
      WHERE other <> u
    }
  RETURN collect(DISTINCT f) AS facts
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_TRACE]->(t:ReasoningTrace)
  WHERE t IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_TRACE]->(t)
      WHERE other <> u
    }
  OPTIONAL MATCH (t)-[:HAS_STEP]->(s:ReasoningStep)
  RETURN collect(DISTINCT t) AS traces, collect(DISTINCT s) AS steps
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:PERFORMED_READ]->(a:MemoryReadAudit)
  WHERE a IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:PERFORMED_READ]->(a)
      WHERE other <> u
    }
  RETURN collect(DISTINCT a) AS read_audits
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_DOCUMENT]->(d:Document)
  WHERE d IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_DOCUMENT]->(d)
      WHERE other <> u
    }
  OPTIONAL MATCH (d)-[:HAS_CHUNK]->(ch:Chunk)
  RETURN collect(DISTINCT d) AS documents, collect(DISTINCT ch) AS chunks
}
CALL {
  WITH u
  OPTIONAL MATCH (u)-[:HAS_ENTITY]->(e:Entity)
  WHERE e IS NOT NULL
    AND NOT EXISTS {
      MATCH (other:User)-[:HAS_ENTITY]->(e)
      WHERE other <> u
    }
  RETURN collect(DISTINCT e) AS entities
}
FOREACH (n IN messages | DETACH DELETE n)
FOREACH (n IN steps | DETACH DELETE n)
FOREACH (n IN chunks | DETACH DELETE n)
FOREACH (n IN conversations | DETACH DELETE n)
FOREACH (n IN preferences | DETACH DELETE n)
FOREACH (n IN facts | DETACH DELETE n)
FOREACH (n IN traces | DETACH DELETE n)
FOREACH (n IN read_audits | DETACH DELETE n)
FOREACH (n IN documents | DETACH DELETE n)
FOREACH (n IN entities | DETACH DELETE n)
FOREACH (_ IN CASE WHEN u IS NULL THEN [] ELSE [1] END | DETACH DELETE u)
MERGE (revision:MemoryCorpusRevision {singleton: 'corpus'})
ON CREATE SET revision.epoch = 0
SET revision.epoch = revision.epoch + 1,
    revision.updated_at = datetime()
WITH conversations, messages, preferences, facts, traces, steps, entities,
     documents, chunks
MERGE (document_revision:DocumentCorpusRevision {singleton: 'corpus'})
ON CREATE SET document_revision.epoch = 0,
              document_revision.created_at = datetime()
SET document_revision.epoch = document_revision.epoch + 1,
    document_revision.updated_at = datetime()
RETURN size(conversations) AS conversations,
       size(messages) AS messages,
       size(preferences) AS preferences,
       size(facts) AS facts,
       size(traces) AS traces,
       size(steps) AS steps,
       size(entities) AS entities,
       size(documents) AS documents,
       size(chunks) AS chunks
`

const memoryGraphPurgeVerifyQuery = `
OPTIONAL MATCH (u:User {identifier: $identity_id})
WITH count(u) AS users
OPTIONAL MATCH (c:Conversation {user_identifier: $identity_id})
WITH users, count(c) AS conversations
OPTIONAL MATCH (t:ReasoningTrace {user_identifier: $identity_id})
WITH users, conversations, count(t) AS traces
OPTIONAL MATCH (a:MemoryReadAudit {user_identifier: $identity_id})
WITH users, conversations, traces, count(a) AS read_audits
RETURN users + conversations + traces + read_audits AS remaining
`
