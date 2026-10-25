// pluginApi.ts is the data layer over /api/governance/plugins (internal/agui
// governance_plugins_api.go). Go writes an empty slice as null and its field names in
// snake_case, so every response is decoded here into the shapes the board renders.
//
// The install and the removal carry the Idempotency-Key the shared fetch wrapper attaches
// (api/idempotency.ts); the review writes nothing and needs none.

import { getJSON } from '../api/json';
import { deleteJSON, postJSON, stringValue } from './governanceApi';

export const GOV_PLUGINS_PATH = '/api/governance/plugins';

export interface PluginSkill {
  readonly name: string;
  readonly hash: string;
}

/** A skill or connector the review left out, with the reason. */
export interface PluginSkip {
  readonly name: string;
  readonly reason: string;
}

export interface PluginConnector {
  readonly name: string;
  readonly transport: string;
  /** 'box' when it runs in each identity's box, 'remote' when it is reached by URL. */
  readonly placement: string;
  readonly trust: string;
  /** True when the server may ask for an authorization before it answers. */
  readonly oauth: boolean;
}

/** What an install would write, pinned to one commit and named by its digest. */
export interface PluginReview {
  readonly ref: string;
  readonly commit: string;
  readonly digest: string;
  readonly name: string;
  readonly version: string;
  readonly author: string;
  readonly format: string;
  readonly skills: readonly PluginSkill[];
  readonly skippedSkills: readonly PluginSkip[];
  readonly connectors: readonly PluginConnector[];
  readonly skippedConnectors: readonly PluginSkip[];
  readonly notes: readonly string[];
  /** Why the plugin's connectors are off as a whole; empty when they are not. */
  readonly mcpDisabled: string;
}

/** One skill or connector of an install or a removal: ok, skipped, failed or attention. */
export interface PluginStep {
  readonly name: string;
  readonly outcome: string;
  readonly reason: string;
}

export interface PluginReport {
  readonly review: PluginReview;
  readonly skills: readonly PluginStep[];
  readonly connectors: readonly PluginStep[];
}

export interface PluginRemoveReport {
  readonly name: string;
  readonly skills: readonly PluginStep[];
  readonly connectors: readonly PluginStep[];
  readonly dataDeleted: boolean;
}

/** One installed plugin. `phase` is 'installed' unless an install or a removal is under way,
 * or was interrupted. */
export interface PluginRecord {
  readonly name: string;
  readonly version: string;
  readonly commit: string;
  readonly skills: readonly PluginSkill[];
  readonly connectors: readonly string[];
  readonly phase: string;
}

export interface PluginInstallRequest {
  readonly ref: string;
  readonly commit: string;
  readonly digest: string;
}

/** The first seven characters of a commit, as git abbreviates it. */
export function shortCommit(commit: string): string {
  return commit.slice(0, 7);
}

type Wire = Record<string, unknown>;

function list<T>(value: unknown, decode: (raw: Wire) => T): readonly T[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry: unknown) =>
    entry !== null && typeof entry === 'object' ? [decode(entry as Wire)] : [],
  );
}

function strings(value: unknown): readonly string[] {
  return Array.isArray(value)
    ? value.filter((entry): entry is string => typeof entry === 'string')
    : [];
}

const skill = (raw: Wire): PluginSkill => ({
  name: stringValue(raw.name),
  hash: stringValue(raw.hash),
});

const skip = (raw: Wire): PluginSkip => ({
  name: stringValue(raw.name),
  reason: stringValue(raw.reason),
});

const step = (raw: Wire): PluginStep => ({
  name: stringValue(raw.name),
  outcome: stringValue(raw.outcome),
  reason: stringValue(raw.reason),
});

const connector = (raw: Wire): PluginConnector => ({
  name: stringValue(raw.name),
  transport: stringValue(raw.transport),
  placement: stringValue(raw.placement),
  trust: stringValue(raw.trust),
  oauth: raw.oauth === true,
});

function review(raw: Wire): PluginReview {
  return {
    ref: stringValue(raw.ref),
    commit: stringValue(raw.commit),
    digest: stringValue(raw.digest),
    name: stringValue(raw.name),
    version: stringValue(raw.version),
    author: stringValue(raw.author),
    format: stringValue(raw.format),
    skills: list(raw.skills, skill),
    skippedSkills: list(raw.skipped_skills, skip),
    connectors: list(raw.connectors, connector),
    skippedConnectors: list(raw.skipped_connectors, skip),
    notes: strings(raw.notes),
    mcpDisabled: stringValue(raw.mcp_disabled),
  };
}

const record = (raw: Wire): PluginRecord => ({
  name: stringValue(raw.name),
  version: stringValue(raw.version),
  commit: stringValue(raw.commit),
  skills: list(raw.skills, skill),
  connectors: strings(raw.connectors),
  phase: stringValue(raw.phase),
});

/** GET /api/governance/plugins — the installed plugins. */
export async function fetchPlugins(): Promise<readonly PluginRecord[]> {
  const body = await getJSON<Wire>(GOV_PLUGINS_PATH);
  return list(body.plugins, record);
}

/** POST /api/governance/plugins/review — fetch the plugin at its pinned commit and say what an
 * install would write. */
export async function reviewPlugin(ref: string): Promise<PluginReview> {
  return review(await postJSON<Wire>(`${GOV_PLUGINS_PATH}/review`, { ref }));
}

/** POST /api/governance/plugins — install what the review showed. A 409 means the review no
 * longer holds, or the plugin is already installed. */
export async function installPlugin(req: PluginInstallRequest): Promise<PluginReport> {
  const raw = await postJSON<Wire>(GOV_PLUGINS_PATH, req);
  const reviewed = raw.review;
  return {
    review: review(reviewed !== null && typeof reviewed === 'object' ? (reviewed as Wire) : {}),
    skills: list(raw.skills, step),
    connectors: list(raw.connectors, step),
  };
}

/** DELETE /api/governance/plugins/{name} — remove the plugin; its data goes too only when
 * asked. */
export async function removePlugin(name: string, deleteData: boolean): Promise<PluginRemoveReport> {
  const query = deleteData ? '?data=delete' : '';
  const raw = await deleteJSON<Wire>(`${GOV_PLUGINS_PATH}/${encodeURIComponent(name)}${query}`);
  return {
    name: stringValue(raw.name),
    skills: list(raw.skills, step),
    connectors: list(raw.connectors, step),
    dataDeleted: raw.data_deleted === true,
  };
}
