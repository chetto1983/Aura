// Spike 068 - Stage A: Aura's REAL graphview queries (internal/knowledge/graphview_intent.go)
// run UNMODIFIED over ArcadeDB Bolt+native-Cypher. --fail-at-end => failures recorded, not fatal.

// == A1: load representative Aura subgraph (auto-create vertex/edge types?) ==
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
RETURN count(*) AS created;

// == A2: multi-label node (POLE+O :Entity:OBJECT:VEHICLE) ==
CREATE (e:Entity:OBJECT:VEHICLE {id:'e3', name:'Model 3'}) RETURN labels(e) AS labels;

// == A3: compileSchema (Neo4j CALL db.labels/relationshipTypes/propertyKeys + apoc) ==
CALL db.labels() YIELD label RETURN collect(label) AS labels;

// == A4: compileSeed AS-WRITTEN (elementId + apoc.convert.toJson(labels())) ==
MATCH (c:Conversation {session_id:'s1'})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
OPTIONAL MATCH (e)-[r]-(n)
RETURN elementId(e) AS s_id, apoc.convert.toJson(labels(e)) AS s_labels,
       coalesce(e.name, e.canonical_name) AS s_caption, type(r) AS r_type;

// == A4b: compileSeed ported (elementId + labels() + coalesce + properties) ==
MATCH (c:Conversation {session_id:'s1'})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
OPTIONAL MATCH (e)-[r]-(n)
RETURN elementId(e) AS s_id, labels(e) AS s_labels, coalesce(e.name,e.canonical_name) AS s_caption,
       properties(e) AS s_props, elementId(n) AS n_id, type(r) AS r_type,
       elementId(startNode(r)) AS r_src, elementId(endNode(r)) AS r_dst;

// == A5: compileSeed scope guard EXISTS { MATCH ... } subquery ==
MATCH (c:Conversation {session_id:'s1'})
WHERE EXISTS { MATCH (:User {identifier:'aura-local'})-[:HAS_CONVERSATION]->(c) }
RETURN c.id AS cid;

// == A6: list-membership filter (any(l IN labels(n) WHERE l IN [...])) ==
MATCH (s)-[r]->(n) WHERE type(r) IN ['MENTIONS','RELATED_TO']
RETURN type(r) AS rt, count(*) AS c;
