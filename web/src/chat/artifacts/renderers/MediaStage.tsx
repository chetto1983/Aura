import type { ReactNode } from 'react';

// The preview's frame for an image, a clip or a recording: the media sits centred in the space
// under the header at its own size, instead of hugging the left edge beside an empty band.
export function MediaStage({ children }: { readonly children: ReactNode }) {
  return (
    <div data-slot="media-stage" className="flex h-full items-center justify-center p-6">
      {children}
    </div>
  );
}
