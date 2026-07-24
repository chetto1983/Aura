// -- migrate:up
// Private adaptive projection. :User is the identity anchor already owned by
// neo4j-agent-memory. Adaptive nodes use disjoint labels and are never exposed
// through memory_search / memory_get_context.

CREATE CONSTRAINT adaptive_episode_id IF NOT EXISTS
  FOR (e:AdaptiveEpisode) REQUIRE e.id IS UNIQUE;

CREATE CONSTRAINT adaptive_event_id IF NOT EXISTS
  FOR (e:AdaptiveEvent) REQUIRE e.id IS UNIQUE;

CREATE INDEX adaptive_episode_owner IF NOT EXISTS
  FOR (e:AdaptiveEpisode) ON (e.owner_id);

CREATE INDEX adaptive_event_owner_sequence IF NOT EXISTS
  FOR (e:AdaptiveEvent) ON (e.owner_id, e.aggregate_id, e.sequence);
