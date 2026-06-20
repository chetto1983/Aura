-- Spike 067 — Stage A: port Aura's read-only graphview queries to Apache AGE.
-- Source queries: internal/knowledge/graphview_intent.go (compileSchema/Seed/Expand/Overview).
-- Goal: measure portability of the 4 read queries + surface the model mismatches.
-- ON_ERROR_STOP off so a failing statement is RECORDED, not fatal — the failures
-- ARE the finding.
\set ON_ERROR_STOP off
\timing on
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

\echo '== A0: create graph =='
SELECT create_graph('aura');

\echo '== A1: load representative Aura subgraph (mirrors live labels/rels) =='
SELECT * FROM cypher('aura', $$
  CREATE (u:User {identifier:'aura-local'})
  CREATE (c:Conversation {id:'c1', session_id:'s1', user_identifier:'aura-local'})
  CREATE (m1:Message {id:'m1', role:'user', content:'I drive a red Tesla'})
  CREATE (m2:Message {id:'m2', role:'assistant', content:'Nice EV'})
  CREATE (e1:Entity {id:'e1', name:'Tesla', type:'OBJECT', canonical_name:'Tesla'})
  CREATE (e2:Entity {id:'e2', name:'Davide', type:'PERSON', canonical_name:'Davide'})
  CREATE (u)-[:HAS_CONVERSATION]->(c)
  CREATE (c)-[:HAS_MESSAGE]->(m1)
  CREATE (c)-[:HAS_MESSAGE]->(m2)
  CREATE (m1)-[:NEXT_MESSAGE]->(m2)
  CREATE (m1)-[:MENTIONS]->(e1)
  CREATE (m1)-[:MENTIONS]->(e2)
  CREATE (e2)-[:RELATED_TO {type:'owns'}]->(e1)
  RETURN count(*)
$$) as (n agtype);

\echo '== A2: MULTI-LABEL node (Aura POLE+O uses :Entity:OBJECT:VEHICLE) — does AGE accept it? =='
SELECT * FROM cypher('aura', $$
  CREATE (e:Entity:OBJECT:VEHICLE {id:'e3', name:'Model 3'}) RETURN e
$$) as (e agtype);

\echo '== A3: compileSchema — Neo4j uses CALL db.labels(); AGE has none. AGE-native via ag_catalog: =='
SELECT name AS label FROM ag_catalog.ag_label
  WHERE graph = (SELECT graphid FROM ag_catalog.ag_graph WHERE name='aura') AND kind='v';

\echo '== A4: compileSeed core — Neo4j elementId()/apoc.convert.toJson(labels())/coalesce =='
\echo '-- A4a: as-written Neo4j (elementId + labels()+apoc) — EXPECT FAIL'
SELECT * FROM cypher('aura', $$
  MATCH (c:Conversation {session_id:'s1'})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
  RETURN elementId(e) AS s_id, apoc.convert.toJson(labels(e)) AS s_labels
$$) as (s_id agtype, s_labels agtype);

\echo '-- A4b: AGE-ported (id() + label() singular + coalesce) — EXPECT OK'
SELECT * FROM cypher('aura', $$
  MATCH (c:Conversation {session_id:'s1'})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
  OPTIONAL MATCH (e)-[r]-(n)
  RETURN id(e) AS s_id, label(e) AS s_label, coalesce(e.name, e.canonical_name) AS s_caption,
         id(n) AS n_id, label(n) AS n_label, id(r) AS r_id, type(r) AS r_type,
         id(startNode(r)) AS r_src, id(endNode(r)) AS r_dst
$$) as (s_id agtype, s_label agtype, s_caption agtype, n_id agtype, n_label agtype,
        r_id agtype, r_type agtype, r_src agtype, r_dst agtype);

\echo '== A5: compileSeed scope guard — Neo4j EXISTS { MATCH ... } subquery — EXPECT FAIL =='
SELECT * FROM cypher('aura', $$
  MATCH (c:Conversation {session_id:'s1'})
  WHERE EXISTS { MATCH (:User {identifier:'aura-local'})-[:HAS_CONVERSATION]->(c) }
  RETURN c.id
$$) as (cid agtype);

\echo '== A6: properties(n) projection (graphview returns properties(e)) — does AGE support it? =='
SELECT * FROM cypher('aura', $$ MATCH (e:Entity {id:'e1'}) RETURN properties(e) $$) as (p agtype);

\echo '== A7: list-param filter ($rel_types = [] OR type(r) IN $rel_types) — inline literal form =='
SELECT * FROM cypher('aura', $$
  MATCH (s)-[r]->(n)
  WHERE type(r) IN ['MENTIONS','RELATED_TO']
  RETURN type(r), count(*)
$$) as (rt agtype, c agtype);

\echo '== A8: can cypher() take a server-side PARAMETER (Aura D-05 param-map)? PREPARE form =='
PREPARE seed(agtype) AS
  SELECT * FROM cypher('aura', $$ MATCH (c:Conversation {session_id:$sess}) RETURN c.id $$, $1) as (cid agtype);
EXECUTE seed('{"sess":"s1"}');

\echo '== A8b: inline $param WITHOUT the PREPARE/agtype arg — EXPECT FAIL (proves params cannot be inlined) =='
SELECT * FROM cypher('aura', $$ MATCH (c:Conversation {session_id:$sess}) RETURN c.id $$) as (cid agtype);
