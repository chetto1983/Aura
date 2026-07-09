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
