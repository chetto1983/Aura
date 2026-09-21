import { createContext, useContext } from 'react';
import type { EditKind } from './editRules';

export interface AssetEditTarget {
  readonly assetId: string;
  readonly kind: EditKind;
}

export interface GarageEditTarget {
  readonly garageObjectId: string;
  readonly fileName: string;
  readonly sizeBytes: number;
  readonly kind: EditKind;
}

export type EditTarget = AssetEditTarget | GarageEditTarget;

/** How a surface opens the editor. Undefined outside AppShell (the share pages), where the
 *  button therefore renders nothing. */
export const OpenEditorContext = createContext<((target: EditTarget) => void) | undefined>(
  undefined,
);

export function useOpenEditor(): ((target: EditTarget) => void) | undefined {
  return useContext(OpenEditorContext);
}
