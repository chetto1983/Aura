import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// RatioTiles — the aspect-ratio control of both option popovers. A ratio is a shape, and a
// row of "16:9 / 9:16 / 1:1" text reads as three equal things; the tile draws the rectangle
// the clip will actually have, so the choice is visible before it is read.

interface RatioTilesProps {
  readonly label: string;
  readonly ratios: readonly string[];
  readonly value: string;
  readonly onChange: (ratio: string) => void;
}

/** True when the ratio is at least as wide as it is tall. A label neither side can parse
 *  (the catalog is free to declare one) is drawn as a landscape rectangle rather than
 *  collapsing to nothing. */
function isWide(ratio: string): boolean {
  const [width, height] = ratio.split(':').map(Number);
  if (width === undefined || height === undefined) return true;
  if (!Number.isFinite(width) || !Number.isFinite(height) || height <= 0) return true;
  return width >= height;
}

export function RatioTiles({ label, ratios, value, onChange }: RatioTilesProps) {
  return (
    <ToggleGroup
      type="single"
      spacing={2}
      variant="outline"
      value={value}
      aria-label={label}
      // Radix reports '' when the pressed item is toggled off. A generation always has a
      // ratio, so an empty choice is discarded rather than written back as "none".
      onValueChange={(next: string) => {
        if (next !== '') onChange(next);
      }}
      className="flex w-full flex-wrap gap-1.5"
    >
      {ratios.map((ratio) => (
        <ToggleGroupItem
          key={ratio}
          value={ratio}
          aria-label={ratio}
          className="h-auto flex-col gap-1.5 rounded-[var(--radius-sm)] px-2.5 py-2 text-text-muted data-[state=on]:text-accent-text"
        >
          <span className="grid size-7 place-items-center">
            {/* The box is fixed; the rectangle claims the one dimension that lets the other
                stay inside it, so every tile's drawing is on the same scale. */}
            <span
              aria-hidden="true"
              style={{ aspectRatio: ratio.replace(':', ' / ') }}
              className={`rounded-[3px] border border-current ${isWide(ratio) ? 'w-7' : 'h-7'}`}
            />
          </span>
          <span className="text-[11px] leading-none font-medium tabular-nums">{ratio}</span>
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}
