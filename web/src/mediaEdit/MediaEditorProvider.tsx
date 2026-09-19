import { lazy, Suspense, useState, type ReactNode } from 'react';
import { OpenEditorContext, type EditTarget } from './mediaEditorContext';

// The one open editor, owned at the shell. A surface inside a Radix modal (PreviewModal) closes
// the modal and hands the target here; an editor opened inside the modal would be caught by the
// modal's focus trap. The host is lazy, so Filerobot and Mediabunny load on the first click.
const MediaEditorHost = lazy(() => import('./MediaEditorHost'));

export function MediaEditorProvider({ children }: { readonly children: ReactNode }) {
  const [target, setTarget] = useState<EditTarget>();
  return (
    <OpenEditorContext.Provider value={setTarget}>
      {children}
      {target === undefined ? null : (
        <Suspense fallback={null}>
          <MediaEditorHost
            key={target.assetId}
            assetId={target.assetId}
            kind={target.kind}
            onClose={() => {
              setTarget(undefined);
            }}
          />
        </Suspense>
      )}
    </OpenEditorContext.Provider>
  );
}
