-- Spike 067 - Stage D: GDS-equivalent probe + indicative latency on AGE.
\set ON_ERROR_STOP off
\timing on
LOAD 'age'; SET search_path = ag_catalog, "$user", public;

\echo '== D1: Neo4j GDS PageRank (gds.pageRank.stream) - EXPECT FAIL (AGE has no GDS) =='
SELECT * FROM cypher('aura', $$ CALL gds.pageRank.stream('g') YIELD nodeId, score RETURN nodeId, score $$) as (n agtype, s agtype);

\echo '== D2: Leiden community detection (gds.leiden) - EXPECT FAIL =='
SELECT * FROM cypher('aura', $$ CALL gds.leiden.stream('g') YIELD nodeId, communityId RETURN nodeId, communityId $$) as (n agtype, c agtype);

\echo '== D3: degree centrality is hand-rollable in plain Cypher (works) =='
SELECT * FROM cypher('aura', $$ MATCH (e:Entity)-[r]-() RETURN e.name AS name, count(r) AS degree ORDER BY degree DESC $$) as (name agtype, degree agtype);

\echo '== D4: indicative latency - 2-hop traversal x200 via generate_series (AGE, warm) =='
SELECT count(*) AS runs FROM generate_series(1,200) g,
  LATERAL cypher('aura', $$ MATCH (c:Conversation {session_id:'s1'})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity) RETURN e.name $$) as (nm agtype);
