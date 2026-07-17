// shareTypes.ts — the TypeScript mirror of the Go internal/share/snapshot.go wire contract
// (built in plan 37F-03). BuildSnapshot in that file is the ONLY function in the Go codebase
// that accepts an []llm.Message and returns share-bound data; the two files must not drift —
// scripts/check_share_wire_contract.cjs extracts every json tag from internal/share/snapshot.go
// and asserts each one appears here, failing the build on any rename or omission. Diff the two
// files directly when unsure which one is current.
//
// Neither this file nor internal/share/snapshot.go declares a field capable of carrying a tool
// call's parameters, its raw output, a host or container directory string, or the owner's
// identity. That is not an oversight to review for on either side — it is a structural
// property of both types (SC3): the TS shape below is the second statement of the SAME
// invariant, not an independent design choice.

/** Mirrors internal/share.Snapshot. The complete, recipient-safe projection of one shared
 *  conversation — the ONLY shape the Markdown export, the JSON export, and the public share
 *  page all render (three surfaces, one struct, per D-07). */
export interface Snapshot {
  readonly schema_version: number;
  readonly title: string;
  readonly model: string;
  readonly created_at: string;
  readonly snapshot_at: string;
  readonly turns: readonly SnapshotTurn[];
  readonly artifacts: readonly SnapshotArtifact[];
}

/** Mirrors internal/share.SnapshotTurn. `role` is deliberately the literal union below, never
 *  a wider `string`: the Go allowlist projection drops every role OTHER than "user"/
 *  "assistant" before this type is ever constructed, so a renderer branch for any other role
 *  is dead code that cannot execute — widening the type here would only invite writing it. */
export interface SnapshotTurn {
  readonly seq: number;
  readonly role: 'user' | 'assistant';
  readonly text: string;
  /** Tool NAMES the assistant called on this turn, in call order, duplicates preserved —
   *  NEVER the call's parameters or its returned data (the D-08 hard redaction rule).
   *  Absent (not merely empty) when no tool was called. */
  readonly tool_names?: readonly string[];
}

/** Mirrors internal/share.SnapshotArtifact — the 4-key allowlist descriptor for one delivered,
 *  agent-produced artifact (D-09). Carries no field shaped to hold a storage location. */
export interface SnapshotArtifact {
  readonly asset_id: string;
  readonly filename: string;
  readonly mime_type: string;
  readonly size_bytes: number;
}

// ShareLink — the API view of one aura.shared_links database row (migration 0040), served by
// plan 37F-10's endpoints and consumed by the share modal (37F-15) and the management surfaces
// (37F-17). It is NOT part of the Snapshot wire-contract mirror above and is not checked by
// scripts/check_share_wire_contract.cjs — that script asserts ONLY the Snapshot family's json
// tags.
//
// `url` holds one of two DIFFERENT shapes depending on `tier`, and the difference is
// load-bearing (PRD item 17 / RESEARCH OQ#4):
//   - tier: 'public' -> url is the one-time plaintext token form `/s/{token}`, returned ONLY
//     by createShare's response at mint time. The token is hashed at rest (D-13), so there is
//     nothing to re-derive it from afterward — listShares returns public rows with `url`
//     ABSENT.
//   - tier: 'internal' -> url is `/shared/{id}`, derived from the `id` this type already
//     carries. It holds no secret — migration 0040's shared_links_tier_shape CHECK forces
//     token_hash IS NULL whenever tier='internal', so there is no token to build a `/s/{token}`
//     form from even by mistake — and is therefore ALWAYS present, including from listShares.
//
// Consequence for every consumer: code that treats `url` as uniformly present breaks the
// moment it renders a listed public row (there is nothing to show); code that treats it as
// uniformly absent needlessly hides an internal link it could always render. An internal
// share's link is NEVER the `/s/{token}` form — there is no token behind it to build one from.
export interface ShareLink {
  readonly id: string;
  readonly tier: 'internal' | 'public';
  /** See the tier-shape note above. */
  readonly url?: string;
  readonly expires_at?: string;
  readonly revoked_at?: string;
  readonly created_at: string;
  readonly updated_at: string;
}
