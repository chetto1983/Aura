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
  /** What the operator trusts: the command the box starts with its args, or the URL dialled. */
  readonly command: string;
  readonly args: readonly string[];
  readonly url: string;
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
  /** Why the install stopped part way; empty when it finished. */
  readonly error: string;
}

export interface PluginRemoveReport {
  readonly name: string;
  readonly skills: readonly PluginStep[];
  readonly connectors: readonly PluginStep[];
  readonly dataDeleted: boolean;
  /** Why the removal stopped part way; empty when it finished. */
  readonly error: string;
}

/** The account of an upgrade: the version before and after, or that it was already up to date.
 * `applied` means the new version's connectors were written; past that it does not roll back. */
export interface PluginUpgradeReport {
  readonly name: string;
  readonly fromVersion: string;
  readonly fromCommit: string;
  readonly version: string;
  readonly commit: string;
  readonly upToDate: boolean;
  readonly applied: boolean;
  readonly skills: readonly PluginStep[];
  readonly connectors: readonly PluginStep[];
  /** Why the upgrade stopped part way; empty when it finished. */
  readonly error: string;
}

/** One installed plugin. `phase` is 'installed' unless an install, an upgrade or a removal is
 * under way, or was interrupted. */
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

/** The URL a remote connector dials, or the command line a box one runs, where an argument that
 * is empty or holds a space or a quote is shown quoted, so every argument boundary is visible. */
export function connectorTarget(c: PluginConnector): string {
  if (c.url !== '') return c.url;
  return [c.command, ...c.args]
    .map((arg) => (arg === '' || /[\s"'\\]/.test(arg) ? JSON.stringify(arg) : arg))
    .join(' ');
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
  command: stringValue(raw.command),
  args: strings(raw.args),
  url: stringValue(raw.url),
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

/** A write that an error stopped after it began answers 207 with `{error, report}`: what it did,
 * and why it stopped. Any other answer is the report itself. */
function outcome(raw: Wire): { readonly body: Wire; readonly error: string } {
  const report = raw.report;
  return report !== null && typeof report === 'object'
    ? { body: report as Wire, error: stringValue(raw.error) }
    : { body: raw, error: '' };
}

/** POST /api/governance/plugins — install what the review showed. A 409 means the review no
 * longer holds, or the plugin is already installed. */
export async function installPlugin(req: PluginInstallRequest): Promise<PluginReport> {
  const { body, error } = outcome(await postJSON<Wire>(GOV_PLUGINS_PATH, req));
  const reviewed = body.review;
  return {
    review: review(reviewed !== null && typeof reviewed === 'object' ? (reviewed as Wire) : {}),
    skills: list(body.skills, step),
    connectors: list(body.connectors, step),
    error,
  };
}

/** POST /api/governance/plugins/{name}/upgrade — move the plugin to the head of its repository's
 * default branch, as Codex's upgrade does, with no review. A refusal carries why: a new connector
 * that did not start leaves the old version in place. */
export async function upgradePlugin(name: string): Promise<PluginUpgradeReport> {
  const { body, error } = outcome(
    await postJSON<Wire>(`${GOV_PLUGINS_PATH}/${encodeURIComponent(name)}/upgrade`, {}),
  );
  return {
    name: stringValue(body.name),
    fromVersion: stringValue(body.from_version),
    fromCommit: stringValue(body.from_commit),
    version: stringValue(body.version),
    commit: stringValue(body.commit),
    upToDate: body.up_to_date === true,
    applied: body.applied === true,
    skills: list(body.skills, step),
    connectors: list(body.connectors, step),
    error,
  };
}

/** DELETE /api/governance/plugins/{name} — remove the plugin; its data goes too only when
 * asked. */
export async function removePlugin(name: string, deleteData: boolean): Promise<PluginRemoveReport> {
  const query = deleteData ? '?data=delete' : '';
  const { body, error } = outcome(
    await deleteJSON<Wire>(`${GOV_PLUGINS_PATH}/${encodeURIComponent(name)}${query}`),
  );
  return {
    name: stringValue(body.name),
    skills: list(body.skills, step),
    connectors: list(body.connectors, step),
    dataDeleted: body.data_deleted === true,
    error,
  };
}
