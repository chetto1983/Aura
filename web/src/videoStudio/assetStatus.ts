// assetStatus.ts — what an answer from the asset route MEANS, in the one place both routes that
// ask can read it. The editor fetching a source's bytes and a load checking whether a saved
// project's sources are still there differ in what they DO with the answer and not at all in how
// they read it — and the rule was living in two files, each with its own copy of the reasoning,
// which is the shape drift hides in.

/** The two statuses the asset route uses for "this is not here": 404 is its own existence-hiding
 *  answer for gone-or-not-yours (internal/agui/assets_api.go, D-12). Nothing else means gone. */
const GONE_STATUSES = new Set([404, 410]);

/**
 * Whether this answer says the asset is gone. Only a 404/410 does. A 401, a 403 or a 500 is a
 * different sentence and leaves as the failure it is: telling an operator their clip was
 * permanently deleted because a session expired or a proxy hiccuped is the worse of the two wrong
 * answers, and it is the one they act on.
 */
export function assetIsGone(assetId: string, response: Response): boolean {
  if (GONE_STATUSES.has(response.status)) return true;
  if (!response.ok) {
    throw new Error(`videoStudio: source ${assetId} answered ${String(response.status)}`);
  }
  return false;
}
