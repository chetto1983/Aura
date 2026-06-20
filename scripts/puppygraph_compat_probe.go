//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type probe struct {
	name     string
	category string
	query    string
	params   map[string]any
	blocking bool
	why      string
}

type result struct {
	probe  probe
	status string
	detail string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cfg := config{
		url:      env("AURA_PUPPYGRAPH_BOLT_URL", "bolt://127.0.0.1:17688"),
		user:     env("AURA_PUPPYGRAPH_USER", "puppygraph"),
		password: env("AURA_PUPPYGRAPH_PASSWORD", "puppygraph123"),
		database: os.Getenv("AURA_PUPPYGRAPH_DATABASE"),
	}

	fmt.Printf("PuppyGraph compatibility probe\n")
	fmt.Printf("bolt=%s user=%s database=%s\n\n", cfg.url, cfg.user, displayDB(cfg.database))

	probeID := fmt.Sprintf("puppygraph-compat-%d", time.Now().UnixNano())
	conversationID := probeID + "-conversation"
	messageID := probeID + "-message"
	entityID := probeID + "-entity"
	targetEntityID := probeID + "-target"
	locationID := probeID + "-location"
	constraintName := "compat_probe_entity_id"
	propertyIndexName := "compat_probe_message_session"
	vectorIndexName := "compat_probe_message_embedding"

	driver, err := neo4j.NewDriverWithContext(cfg.url, neo4j.BasicAuth(cfg.user, cfg.password, ""))
	if err != nil {
		exitf("driver init failed: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		exitf("connectivity failed: %v", err)
	}

	probes := []probe{
		{
			name:     "Bolt Cypher handshake",
			category: "driver",
			query:    "RETURN 1 AS ok",
			blocking: true,
			why:      "Aura's current Neo4j path uses Bolt/Cypher through the Neo4j driver and MCP adapter.",
		},
		{
			name:     "Sample node query",
			category: "cypher",
			query:    "MATCH (p:person {name: 'marko'}) RETURN p.name AS name, p.age AS age",
			blocking: false,
			why:      "Validates basic openCypher over a PuppyGraph-modeled relational source.",
		},
		{
			name:     "Sample path traversal",
			category: "cypher",
			query:    "MATCH (p:person {name: 'marko'})-[:knows]->(:person)-[:created]->(s:software) RETURN collect(s.name) AS software",
			blocking: false,
			why:      "Validates graph traversal once a schema has been uploaded.",
		},
		{
			name:     "Neo4j label metadata procedure",
			category: "metadata",
			query:    "CALL db.labels() YIELD label RETURN collect(label) AS labels",
			blocking: true,
			why:      "Aura GraphView discovers labels through Neo4j db.* procedures.",
		},
		{
			name:     "Neo4j relationship metadata procedure",
			category: "metadata",
			query:    "CALL db.relationshipTypes() YIELD relationshipType RETURN collect(relationshipType) AS relationships",
			blocking: true,
			why:      "Aura GraphView discovers relationship types through Neo4j db.* procedures.",
		},
		{
			name:     "PuppyGraph component metadata",
			category: "metadata",
			query:    "CALL dbms.components() YIELD name, versions, edition RETURN name, versions, edition",
			blocking: false,
			why:      "Records the PuppyGraph server version and edition exposed over Cypher.",
		},
		{
			name:     "Neo4j property metadata procedure",
			category: "metadata",
			query:    "CALL db.propertyKeys() YIELD propertyKey RETURN collect(propertyKey) AS properties",
			blocking: true,
			why:      "Aura GraphView discovers available properties through Neo4j db.* procedures.",
		},
		{
			name:     "APOC JSON helper",
			category: "apoc",
			query:    "RETURN apoc.convert.toJson(['person', 'software']) AS json",
			blocking: true,
			why:      "Aura GraphView currently emits apoc.convert.toJson in its Cypher projection.",
		},
		{
			name:     "Schema/index inventory",
			category: "schema",
			query:    "SHOW INDEXES YIELD name, type RETURN collect({name: name, type: type}) AS indexes",
			blocking: true,
			why:      "Aura migrations and status checks rely on Neo4j constraints and vector/fulltext indexes.",
		},
		{
			name:     "Neo4j vector search procedure",
			category: "vector",
			query:    "CALL db.index.vector.queryNodes('chunk_embedding', 1, [0.1, 0.2]) YIELD node, score RETURN score",
			blocking: true,
			why:      "Aura's memory smoke test depends on Neo4j native vector search.",
		},
		{
			name:     "Neo4j GDS extension",
			category: "gds",
			query:    "RETURN gds.version() AS gds",
			blocking: false,
			why:      "The current stack includes GDS; parity depends on which graph algorithms are actually required.",
		},
		{
			name:     "Cypher write semantics",
			category: "write",
			query:    "CREATE (n:CompatProbe {probe_id: $probe_id}) RETURN n.probe_id AS probe_id",
			params: map[string]any{
				"probe_id": probeID,
			},
			blocking: true,
			why:      "Aura writes memory nodes and relationships to Neo4j; a read-only graph layer is not a full replacement.",
		},
		{
			name:     "Agent-memory constraint DDL",
			category: "agent-memory",
			query: fmt.Sprintf(`CREATE CONSTRAINT %s IF NOT EXISTS
FOR (e:CompatProbeEntity)
REQUIRE e.id IS UNIQUE`, constraintName),
			blocking: true,
			why:      "neo4j-agent-memory setup creates unique constraints for core node IDs.",
		},
		{
			name:     "Agent-memory property index DDL",
			category: "agent-memory",
			query: fmt.Sprintf(`CREATE INDEX %s IF NOT EXISTS
FOR (m:CompatProbeMessage)
ON (m.session_id)`, propertyIndexName),
			blocking: true,
			why:      "neo4j-agent-memory setup creates regular property indexes for common reads.",
		},
		{
			name:     "Agent-memory vector index DDL",
			category: "agent-memory",
			query:    fmt.Sprintf("CREATE VECTOR INDEX %s IF NOT EXISTS\nFOR (m:CompatProbeMessage)\nON (m.embedding)\nOPTIONS {indexConfig: {`vector.dimensions`: 2, `vector.similarity_function`: 'cosine'}}", vectorIndexName),
			blocking: true,
			why:      "neo4j-agent-memory semantic search depends on Neo4j vector indexes.",
		},
		{
			name:     "Agent-memory conversation write",
			category: "agent-memory",
			query: `CREATE (c:CompatProbeConversation:Conversation {
  probe_id: $probe_id,
  id: $conversation_id,
  session_id: $session_id,
  title: $title,
  created_at: datetime(),
  updated_at: datetime(),
  metadata: $metadata
})
RETURN c.id AS id`,
			params: map[string]any{
				"probe_id":        probeID,
				"conversation_id": conversationID,
				"session_id":      "puppygraph-compat-session",
				"title":           "PuppyGraph compat conversation",
				"metadata":        `{"source":"puppygraph_compat_probe"}`,
			},
			blocking: true,
			why:      "neo4j-agent-memory stores short-term memory as Conversation nodes with datetime fields.",
		},
		{
			name:     "Agent-memory message append",
			category: "agent-memory",
			query: `MATCH (c:CompatProbeConversation {id: $conversation_id})
OPTIONAL MATCH (c)-[:HAS_MESSAGE]->(last:CompatProbeMessage)
WHERE NOT (last)-[:NEXT_MESSAGE]->()
CREATE (m:CompatProbeMessage:Message {
  probe_id: $probe_id,
  id: $message_id,
  role: $role,
  content: $content,
  embedding: $embedding,
  timestamp: datetime(),
  metadata: $metadata
})
CREATE (c)-[:HAS_MESSAGE]->(m)
FOREACH (_ IN CASE WHEN last IS NOT NULL THEN [1] ELSE [] END |
  CREATE (last)-[:NEXT_MESSAGE]->(m)
)
FOREACH (_ IN CASE WHEN last IS NULL THEN [1] ELSE [] END |
  CREATE (c)-[:FIRST_MESSAGE]->(m)
)
SET c.updated_at = datetime()
RETURN m.id AS id`,
			params: map[string]any{
				"probe_id":        probeID,
				"conversation_id": conversationID,
				"message_id":      messageID,
				"role":            "user",
				"content":         "Can PuppyGraph run agent-memory short-term writes?",
				"embedding":       []float64{0.1, 0.2},
				"metadata":        `{"source":"puppygraph_compat_probe"}`,
			},
			blocking: true,
			why:      "neo4j-agent-memory append semantics use OPTIONAL MATCH, FOREACH, CASE, relationships, list vector properties, and datetime().",
		},
		{
			name:     "Agent-memory entity merge",
			category: "agent-memory",
			query: `MERGE (e:CompatProbeEntity:Entity {name: $name, type: $type, deduplication_scope: $deduplication_scope})
ON CREATE SET
  e.probe_id = $probe_id,
  e.id = $entity_id,
  e.canonical_name = $name,
  e.description = $description,
  e.embedding = $embedding,
  e.confidence = $confidence,
  e.created_at = datetime()
ON MATCH SET
  e.updated_at = datetime()
RETURN e.id AS id`,
			params: map[string]any{
				"probe_id":            probeID,
				"entity_id":           entityID,
				"name":                "PuppyGraph",
				"type":                "SOFTWARE",
				"deduplication_scope": "compat",
				"description":         "Compatibility probe entity",
				"embedding":           []float64{0.2, 0.1},
				"confidence":          1.0,
			},
			blocking: true,
			why:      "neo4j-agent-memory long-term memory writes use MERGE plus ON CREATE/ON MATCH SET and vector properties.",
		},
		{
			name:     "Agent-memory relationship merge",
			category: "agent-memory",
			query: `MERGE (source:CompatProbeEntity:Entity {id: $source_id})
ON CREATE SET source.probe_id = $probe_id, source.name = $source_name, source.type = 'SOFTWARE'
MERGE (target:CompatProbeEntity:Entity {id: $target_id})
ON CREATE SET target.probe_id = $probe_id, target.name = $target_name, target.type = 'DATABASE'
MERGE (source)-[r:RELATED_TO]->(target)
ON CREATE SET r.relation_type = $relation_type, r.confidence = $confidence, r.created_at = datetime()
ON MATCH SET r.updated_at = datetime()
RETURN type(r) AS rel_type`,
			params: map[string]any{
				"probe_id":      probeID,
				"source_id":     entityID,
				"source_name":   "Agent Memory",
				"target_id":     targetEntityID,
				"target_name":   "PuppyGraph",
				"relation_type": "ADAPTS_TO",
				"confidence":    0.9,
			},
			blocking: true,
			why:      "neo4j-agent-memory extraction and consolidation rely on idempotent relationship MERGE.",
		},
		{
			name:     "Agent-memory mention link",
			category: "agent-memory",
			query: `MATCH (m:CompatProbeMessage {id: $message_id})
MATCH (e:CompatProbeEntity {id: $entity_id})
MERGE (m)-[r:MENTIONS]->(e)
ON CREATE SET r.confidence = $confidence
RETURN type(r) AS rel_type`,
			params: map[string]any{
				"message_id": messageID,
				"entity_id":  entityID,
				"confidence": 1.0,
			},
			blocking: true,
			why:      "neo4j-agent-memory connects short-term messages to extracted long-term entities with MENTIONS edges.",
		},
		{
			name:     "Agent-memory graph export",
			category: "agent-memory",
			query: `MATCH (c)-[r]->(m)
RETURN
  elementId(c) AS c_id,
  labels(c) AS c_labels,
  properties(c) AS c_props,
  type(r) AS rel_type,
  elementId(r) AS rel_id,
  elementId(m) AS m_id,
  labels(m) AS m_labels,
  properties(m) AS m_props
LIMIT 1`,
			blocking: true,
			why:      "neo4j-agent-memory graph APIs and Aura's explorer need labels(), properties(), elementId(), and type().",
		},
		{
			name:     "Agent-memory APOC redaction",
			category: "agent-memory",
			query:    "RETURN apoc.map.removeKeys({embedding: [0.1, 0.2], content: 'hello'}, ['embedding']) AS props",
			blocking: true,
			why:      "neo4j-agent-memory graph export redacts embeddings with apoc.map.removeKeys unless the query layer is adapted.",
		},
		{
			name:     "Agent-memory vector lookup",
			category: "agent-memory",
			query:    "CALL db.index.vector.queryNodes($index_name, 1, $embedding) YIELD node, score RETURN node.id AS id, score",
			params: map[string]any{
				"index_name": vectorIndexName,
				"embedding":  []float64{0.1, 0.2},
			},
			blocking: true,
			why:      "neo4j-agent-memory search APIs call db.index.vector.queryNodes against managed vector indexes.",
		},
		{
			name:     "Agent-memory point property",
			category: "agent-memory",
			query: `MERGE (e:CompatProbeEntity:Entity {id: $location_id})
ON CREATE SET e.probe_id = $probe_id, e.name = 'Caraglio', e.type = 'LOCATION'
SET e.location = point({latitude: $latitude, longitude: $longitude})
RETURN e.location.latitude AS latitude, e.location.longitude AS longitude`,
			params: map[string]any{
				"probe_id":    probeID,
				"location_id": locationID,
				"latitude":    44.4197,
				"longitude":   7.4321,
			},
			blocking: false,
			why:      "neo4j-agent-memory includes optional geospatial location APIs; not every deployment needs them.",
		},
		{
			name:     "Cleanup compat artifacts",
			category: "cleanup",
			query:    "MATCH (n {probe_id: $probe_id}) DETACH DELETE n RETURN count(n) AS deleted",
			params: map[string]any{
				"probe_id": probeID,
			},
			blocking: false,
			why:      "Best-effort cleanup for probe-created nodes; failure means manual cleanup may be needed.",
		},
	}

	var results []result
	for _, p := range probes {
		results = append(results, runProbe(ctx, driver, cfg.database, p))
	}

	maxName := 0
	for _, r := range results {
		if len(r.probe.name) > maxName {
			maxName = len(r.probe.name)
		}
	}

	counts := map[string]int{}
	for _, r := range results {
		counts[r.status]++
		fmt.Printf("%-8s %-*s  %s\n", r.status, maxName, r.probe.name, r.detail)
	}

	fmt.Printf("\nSummary: OK=%d WARN=%d BLOCKER=%d\n", counts["OK"], counts["WARN"], counts["BLOCKER"])
	if counts["BLOCKER"] > 0 {
		fmt.Println("Decision signal: not a drop-in Neo4j replacement for Aura while blockers remain.")
	} else {
		fmt.Println("Decision signal: no blocking compatibility gaps found by this probe.")
	}
}

type config struct {
	url      string
	user     string
	password string
	database string
}

func runProbe(ctx context.Context, driver neo4j.DriverWithContext, database string, p probe) result {
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	res, err := session.Run(queryCtx, p.query, p.params)
	if err != nil {
		return failed(p, err)
	}

	rows := make([]string, 0, 3)
	for res.Next(queryCtx) {
		rows = append(rows, formatMap(res.Record().AsMap()))
		if len(rows) == 3 {
			break
		}
	}
	if err := res.Err(); err != nil {
		return failed(p, err)
	}
	if _, err := res.Consume(queryCtx); err != nil {
		return failed(p, err)
	}

	detail := fmt.Sprintf("rows=%d", len(rows))
	if len(rows) > 0 {
		detail += " first=" + rows[0]
	}
	return result{probe: p, status: "OK", detail: detail}
}

func failed(p probe, err error) result {
	status := "WARN"
	if p.blocking {
		status = "BLOCKER"
	}
	return result{
		probe:  p,
		status: status,
		detail: fmt.Sprintf("%s; %s", truncate(oneLine(err.Error()), 220), p.why),
	}
}

func formatMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func displayDB(db string) string {
	if db == "" {
		return "(server default)"
	}
	return db
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
