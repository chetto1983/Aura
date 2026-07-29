package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/google/uuid"
)

var (
	_ agui.ConversationPurger = conversationPurgeAdapter{}
	_ agui.GraphPurger        = memoryGraphPurgeAdapter{}
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
