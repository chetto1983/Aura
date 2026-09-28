export interface GalleryItem {
  readonly assetId: string;
  readonly src: string;
  readonly width: number;
  readonly height: number;
  readonly alt: string;
}

export type GalleryCandidate = GalleryItem & { readonly turnId: string };

export const MAX_GALLERY_IMAGES = 8;
const MAX_IMAGE_DIMENSION = 32768;

function validImage(item: GalleryItem): boolean {
  return (
    item.assetId !== '' &&
    item.src.startsWith('blob:') &&
    Number.isInteger(item.width) &&
    Number.isInteger(item.height) &&
    item.width > 0 &&
    item.height > 0 &&
    item.width <= MAX_IMAGE_DIMENSION &&
    item.height <= MAX_IMAGE_DIMENSION
  );
}

/** Only complete groups of at least two trusted, decoded images become galleries. */
export function groupTrustedImages(items: readonly GalleryCandidate[]): readonly GalleryItem[][] {
  const turns = new Map<string, GalleryCandidate[]>();
  for (const item of items) {
    const group = turns.get(item.turnId) ?? [];
    group.push(item);
    turns.set(item.turnId, group);
  }
  const result: GalleryItem[][] = [];
  for (const group of turns.values()) {
    if (group.length < 2 || group.length > MAX_GALLERY_IMAGES || !group.every(validImage)) continue;
    result.push(
      group.map(({ assetId, src, width, height, alt }) => ({ assetId, src, width, height, alt })),
    );
  }
  return result;
}
