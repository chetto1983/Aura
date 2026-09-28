// Adapted from Tool UI's Image registry component at revision
// 49a870286facdbf28160cd647f0d337ebdc9b275 (registry image.json SHA-256
// 96b19883d6d9ee4ea6ba38129bce246bc853ab5fb66f9f2df92c1670b80f0fe2).
// MIT license and copyright notice: see LICENSE.tool-ui in this directory.
// Aura supplies a vetted blob URL and owns zoom, copy, download and attribution.
// This frame keeps Tool UI's ratio/fit presentation but gives auto its natural size.

import type { ImgHTMLAttributes } from 'react';
import { cn } from '@/lib/utils';

type Ratio = 'auto' | '1:1' | '4:3' | '16:9' | '9:16';

const RATIO_CLASS: Record<Exclude<Ratio, 'auto'>, string> = {
  '1:1': 'aspect-square',
  '4:3': 'aspect-[4/3]',
  '16:9': 'aspect-video',
  '9:16': 'aspect-[9/16]',
};

export interface ToolImageProps {
  readonly id: string;
  readonly src: string;
  readonly alt: string;
  readonly ratio?: Ratio;
  readonly fit?: 'cover' | 'contain';
  readonly className?: string;
  readonly onError?: ImgHTMLAttributes<HTMLImageElement>['onError'];
}

export function ToolImage({
  id,
  src,
  alt,
  ratio = 'auto',
  fit = 'contain',
  className,
  onError,
}: ToolImageProps) {
  const natural = ratio === 'auto';
  return (
    <article data-slot="image" data-tool-ui-id={id} className={cn('w-full max-w-full', className)}>
      <div
        className={cn(
          'relative w-full overflow-hidden rounded-[var(--radius-md)] bg-surface-2',
          !natural && RATIO_CLASS[ratio],
        )}
      >
        <img
          src={src}
          alt={alt}
          loading="lazy"
          decoding="async"
          onError={onError}
          className={cn(
            natural
              ? 'block h-auto max-h-[70vh] w-auto max-w-full'
              : 'absolute inset-0 h-full w-full',
            fit === 'cover' ? 'object-cover' : 'object-contain',
          )}
        />
      </div>
    </article>
  );
}
