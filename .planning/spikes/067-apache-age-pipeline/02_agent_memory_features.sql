-- Spike 067 — Stage B: probe the DEEP Neo4j features the agent-memory fork relies on
-- (docker/agent-memory/src/neo4j_agent_memory/graph/queries.py, ~80 templates).
-- Each block targets one feature family. ON_ERROR_STOP off → failures are the finding.
\set ON_ERROR_STOP off
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

\echo '== B1: datetime() temporal (CREATE_CONVERSATION, CREATE_MESSAGE, ...) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ CREATE (x:Probe {id:'p1', created_at: datetime()}) RETURN x $$) as (x agtype);

\echo '== B2: duration({days:N}) (GET_SESSION_CONTEXT preference window) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ RETURN datetime() - duration({days: 7}) $$) as (r agtype);

\echo '== B3: point()/point.distance (SEARCH_LOCATIONS_NEAR geospatial) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ RETURN point({latitude:45.0, longitude:9.0}) $$) as (p agtype);

\echo '== B4: MERGE ... ON CREATE SET ... ON MATCH SET (CREATE_ENTITY upsert) =='
SELECT * FROM cypher('aura', $$
  MERGE (e:Entity {name:'Tesla', type:'OBJECT'})
  ON CREATE SET e.created='new'
  ON MATCH  SET e.touched='again'
  RETURN e.name, e.touched, e.created
$$) as (n agtype, t agtype, c agtype);

\echo '== B5: FOREACH (_ IN CASE WHEN ... | ...) (CREATE_MESSAGE conditional link) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$
  MATCH (c:Conversation {id:'c1'})
  FOREACH (_ IN CASE WHEN c IS NOT NULL THEN [1] ELSE [] END | SET c.flag = true)
  RETURN c.flag
$$) as (f agtype);

\echo '== B6: CALL (a,b) { ... } scoped subquery (MERGE_ENTITIES) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$
  MATCH (s:Entity {id:'e1'}), (t:Entity {id:'e2'})
  CALL (s, t) { MATCH (s)<-[:MENTIONS]-(m:Message) RETURN count(m) AS mc }
  RETURN mc
$$) as (mc agtype);

\echo '== B7: db.index.vector.queryNodes (SEARCH_*_BY_EMBEDDING, 8+ queries) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$
  CALL db.index.vector.queryNodes('entity_embedding_idx', 5, [0.1,0.2,0.3])
  YIELD node, score RETURN node, score
$$) as (node agtype, score agtype);

\echo '== B8: CREATE VECTOR INDEX (migrate create_vector_index_query) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$
  CREATE VECTOR INDEX entity_embedding_idx FOR (e:Entity) ON (e.embedding)
$$) as (r agtype);

\echo '== B9: apoc.map.removeKeys (GET_GRAPH_* export strips embeddings) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ MATCH (e:Entity {id:'e1'}) RETURN apoc.map.removeKeys(properties(e), ['embedding']) $$) as (r agtype);

\echo '== B10: variable-length path *1..3 (GET_SAME_AS_CLUSTER) =='
SELECT * FROM cypher('aura', $$
  MATCH p = (e:Entity {id:'e2'})-[:RELATED_TO*1..3]-(other:Entity)
  RETURN id(other), length(p)
$$) as (oid agtype, len agtype);

\echo '== B11: SHOW INDEXES (SchemaManager introspection) — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ SHOW INDEXES YIELD name RETURN name $$) as (n agtype);

\echo '== B12: list/UNWIND/range/collect/head/toLower/substring (LIST_SESSIONS) =='
SELECT * FROM cypher('aura', $$
  UNWIND range(0,3) AS i WITH collect(i) AS xs
  RETURN head(xs), size(xs), toLower('ABC'), substring('abcdef',0,3)
$$) as (h agtype, s agtype, lo agtype, sub agtype);
