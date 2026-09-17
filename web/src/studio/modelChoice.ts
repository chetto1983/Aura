import type { StudioKind, StudioModel } from './studioApi';

// modelChoice.ts — which model the Studio opens on. The operator's last pick is remembered per
// kind, because choosing a video model says nothing about which image model they want.

function storageKey(kind: StudioKind): string {
  return `aura.studio.model.${kind}`;
}

/** The model this browser last picked for a kind, or undefined.
 *
 *  Every accessor is guarded: a private window throws on localStorage rather than returning
 *  null, and a picker that cannot open is worse than one that forgets. */
export function rememberedModel(kind: StudioKind): string | undefined {
  try {
    return localStorage.getItem(storageKey(kind)) ?? undefined;
  } catch {
    return undefined;
  }
}

export function rememberModel(kind: StudioKind, modelId: string): void {
  try {
    localStorage.setItem(storageKey(kind), modelId);
  } catch {
    // Storage is a convenience; the picker still shows the choice for this session.
  }
}

/** The model to open on: the remembered one while the deployment still lists it, then the
 *  deployment's own default, then whatever it does list. A remembered id the catalog has since
 *  dropped is not offered — the picker would show a selection nothing in the list matches. */
export function initialModel(
  kind: StudioKind,
  listed: readonly StudioModel[],
  deploymentDefault: string,
): string {
  const lists = (id: string): boolean => listed.some((model) => model.id === id);
  const remembered = rememberedModel(kind);
  if (remembered !== undefined && lists(remembered)) return remembered;
  if (lists(deploymentDefault)) return deploymentDefault;
  return listed[0]?.id ?? '';
}
