// -- migrate:up
// Backfill (:Fact)-[:ABOUT_SUBJECT]->(:Entity) for facts written before the sidecar
// started drawing the edge.
//
// A fact's subject used to be a string that merely happened to spell an entity's name, so
// Fact{subject:"Davide"} and Entity{name:"Davide"} sat side by side, unconnected: nothing
// could reach what is known about a person by traversing from the person. New facts are
// linked at write time (long_term._link_fact_to_subject_entity); this catches the ones
// already stored.
//
// Same rules as the write path, deliberately, so the backfill cannot produce a graph the
// sidecar would not have produced itself:
//
//   * scoped per owner — a fact is only ever linked to an entity the SAME user owns
//     through HAS_ENTITY, never merely mentions. Matching globally by name would attach
//     one tenant's fact to another tenant's node.
//   * exact name, case-insensitive. Similarity matching here would link a fact about
//     "Davide" to the entity "David", which is the duplicate this deployment is trying to
//     get rid of, not a link worth having.
//   * creates nothing. Fact subjects are not always names — the onboarding sentinel writes
//     facts whose subject is an identity UUID — so a subject with no owned entity behind
//     it stays a plain string, exactly as at write time.
//
// MERGE, so re-running is a no-op.

MATCH (u:User)-[:HAS_FACT]->(f:Fact)
MATCH (u)-[:HAS_ENTITY]->(e:Entity)
WHERE toLower(e.name) = toLower(f.subject)
MERGE (f)-[:ABOUT_SUBJECT]->(e);
