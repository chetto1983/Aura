// api.ts — the composer skills client (WEBSKILL-01) and the DEGRADE-TO-NO-OP source
// (D-09). It mirrors governanceApi.ts's same-origin getJSON pattern, but is deliberately
// fail-soft: any failure (a throw — including a 401 from an expired session or the 503 the
// backend returns when the skills provider is nil — or a body without a `skills` field)
// resolves to [] so the '/' picker simply never opens instead of surfacing an error state.
// The list is the GLOBAL active-skills snapshot (D-04: no per-identity scoping exists),
// served behind plain RequireAuth by 37D-02's handleComposerSkills.
import { getJSON } from '../../api/json';

export const COMPOSER_SKILLS_PATH = '/api/composer/skills';

/** One active skill row for the picker: the {name, description, type} projection the
 * backend (37D-02 `activeSkillRows(loader.List())`) returns. It intentionally omits the
 * body — only non-sensitive metadata reaches the browser. */
export interface ComposerSkillRow {
  readonly name: string;
  readonly description: string;
  readonly type: string;
}

/** GET /api/composer/skills → the global active-skills snapshot. Any failure (a throw,
 * incl. 401/500/503, or a body missing `skills`) degrades to [] (D-09) so the picker never
 * opens on an empty/unreachable list and never leaks an error surface — the fetch client
 * returns [] on ANY throw. */
export async function fetchComposerSkills(): Promise<readonly ComposerSkillRow[]> {
  try {
    const body = await getJSON<{ skills?: readonly ComposerSkillRow[] }>(COMPOSER_SKILLS_PATH);
    return body.skills ?? [];
  } catch {
    return [];
  }
}

export const REASONING_CAPABILITIES_PATH = '/api/composer/reasoning-capabilities';

/** The composer reasoning-effort capability surface (WEBMODEL-01 / 37E-06 reasoningCapabilitiesDTO):
 * the auto-first UI symbols the active model advertises, its default, and whether detection
 * succeeded. `backend` is non-secret but unused UI-side (the response NEVER carries the model id /
 * base URL / key — T-37E-06-LEAK), so it is intentionally not projected here. */
export interface ReasoningCapabilities {
  readonly levels: readonly string[];
  readonly default: string;
  readonly detected: boolean;
}

/** The safe floor (37D D-09 / D-13): a nil source, a failed detection, or an unreachable probe
 * degrades to {auto,off} so the selector always renders a truthful minimal set and never breaks
 * the Composer. Exported so the hook seeds its initial state with the identical value. */
export const REASONING_CAPABILITIES_FLOOR: ReasoningCapabilities = {
  levels: ['auto', 'off'],
  default: 'auto',
  detected: false,
};

/** GET /api/composer/reasoning-capabilities → the active model's advertised effort levels. Any
 * failure (a throw — incl. 401/500/503 — or a body missing/emptying `levels`) degrades to the safe
 * floor {auto,off} (D-09): the selector must degrade, never break, and Stage-2 server governance
 * rejects any graduated level while undetected so the floor is honest. */
export async function fetchReasoningCapabilities(): Promise<ReasoningCapabilities> {
  try {
    const body = await getJSON<Partial<ReasoningCapabilities>>(REASONING_CAPABILITIES_PATH);
    if (body.levels === undefined || body.levels.length === 0) return REASONING_CAPABILITIES_FLOOR;
    return { levels: body.levels, default: body.default ?? 'auto', detected: body.detected ?? false };
  } catch {
    return REASONING_CAPABILITIES_FLOOR;
  }
}
