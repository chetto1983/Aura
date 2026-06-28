// Spike 068 - Stage B: the agent-memory deep feature set (docker/agent-memory graph/queries.py)
// run over ArcadeDB Bolt. These are the 9 families that BLOCKED Apache AGE. --fail-at-end.

// == B1: datetime() temporal (CREATE_CONVERSATION/CREATE_MESSAGE created_at/timestamp) ==
CREATE (x:Probe {id:'p1', created_at: datetime()}) RETURN x.created_at AS ts;

// == B2: duration({days:N}) (GET_SESSION_CONTEXT preference window) ==
RETURN datetime() AS now, datetime() - duration({days: 7}) AS week_ago;

// == B3: point()/spatial (SEARCH_LOCATIONS_NEAR) ==
RETURN point({latitude: 45.07, longitude: 7.69}) AS p;

// == B4: MERGE ... ON CREATE SET ... ON MATCH SET (CREATE_ENTITY upsert) ==
MERGE (e:Entity {name:'Tesla', type:'OBJECT'})
ON CREATE SET e.created_flag = 'new'
ON MATCH  SET e.matched_flag = 'again'
RETURN e.name AS name, e.created_flag AS c, e.matched_flag AS m;

// == B5: FOREACH (_ IN CASE WHEN ... | ...) (CREATE_MESSAGE conditional link) ==
MATCH (c:Conversation {id:'c1'})
FOREACH (_ IN CASE WHEN c IS NOT NULL THEN [1] ELSE [] END | SET c.flag = true)
RETURN c.flag AS flag;

// == B6: CALL (a,b) { ... } scoped subquery (MERGE_ENTITIES, Cypher 25) ==
MATCH (s:Entity {id:'e1'}), (t:Entity {id:'e2'})
CALL (s, t) { MATCH (s)<-[:MENTIONS]-(m:Message) RETURN count(m) AS mc }
RETURN mc;

// == B6b: legacy CALL { WITH ... } subquery form ==
MATCH (s:Entity {id:'e1'})
CALL { WITH s MATCH (s)<-[:MENTIONS]-(m:Message) RETURN count(m) AS mc }
RETURN mc;

// == B7: db.index.vector.queryNodes (SEARCH_*_BY_EMBEDDING, 8+ queries) ==
CALL db.index.vector.queryNodes('entity_embedding_idx', 5, [0.1,0.2,0.3]) YIELD node, score
RETURN node, score;

// == B8: CREATE VECTOR INDEX (Neo4j syntax; create_vector_index_query) ==
CREATE VECTOR INDEX entity_embedding_idx FOR (e:Entity) ON (e.embedding)
OPTIONS { indexConfig: { `vector.dimensions`: 3, `vector.similarity_function`: 'cosine' } };

// == B9: apoc.map.removeKeys (GET_GRAPH_* embedding strip) ==
MATCH (e:Entity {id:'e1'}) RETURN apoc.map.removeKeys(properties(e), ['embedding']) AS clean;

// == B10: variable-length path *1..3 (GET_SAME_AS_CLUSTER) ==
MATCH p = (e:Entity {id:'e2'})-[:RELATED_TO*1..3]-(other:Entity)
RETURN elementId(other) AS oid, length(p) AS len;

// == B11: SHOW INDEXES (SchemaManager introspection) ==
SHOW INDEXES YIELD name RETURN name;

// == B12: list/UNWIND/range/collect/head/toLower/substring/toFloat (LIST_SESSIONS) ==
UNWIND range(0,3) AS i WITH collect(i) AS xs
RETURN head(xs) AS h, size(xs) AS sz, toLower('ABC') AS lo, substring('abcdef',0,3) AS sub, toFloat('1.5') AS f;
