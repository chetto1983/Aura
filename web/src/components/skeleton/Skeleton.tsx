import type { CSSProperties, ReactNode } from 'react';

type SkeletonRadius = 'none' | 'sm' | 'md' | 'lg' | 'full';
type SkeletonTextSize = 'xs' | 'sm' | 'md' | 'lg' | 'xl';
type SkeletonButtonSize = 'sm' | 'md' | 'lg';
type SkeletonAvatarSize = 'sm' | 'md' | 'lg' | 'xl' | number;

interface BaseSkeletonProps {
  className?: string | undefined;
  testId?: string | undefined;
}

export interface SkeletonBlockProps extends BaseSkeletonProps {
  height?: CSSProperties['height'] | undefined;
  minHeight?: CSSProperties['minHeight'] | undefined;
  radius?: SkeletonRadius;
  style?: CSSProperties | undefined;
  width?: CSSProperties['width'] | undefined;
}

const textHeights: Record<SkeletonTextSize, number> = {
  xs: 8,
  sm: 10,
  md: 12,
  lg: 16,
  xl: 20,
};

const buttonHeights: Record<SkeletonButtonSize, number> = {
  sm: 32,
  md: 44,
  lg: 48,
};

const avatarSizes: Record<Exclude<SkeletonAvatarSize, number>, number> = {
  sm: 24,
  md: 32,
  lg: 40,
  xl: 48,
};

function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}

function sizedStyle({
  height,
  minHeight,
  style,
  width,
}: Pick<SkeletonBlockProps, 'height' | 'minHeight' | 'style' | 'width'>): CSSProperties {
  return {
    ...style,
    ...(height === undefined ? {} : { height }),
    ...(minHeight === undefined ? {} : { minHeight }),
    ...(width === undefined ? {} : { width }),
  };
}

export function SkeletonBlock({
  className,
  height,
  minHeight,
  radius = 'sm',
  style,
  testId,
  width = '100%',
}: SkeletonBlockProps) {
  return (
    <span
      aria-hidden="true"
      className={cx('skeleton-block', className)}
      data-rounded={radius}
      data-testid={testId}
      style={sizedStyle({ height, minHeight, style, width })}
    />
  );
}

export interface SkeletonTextProps extends BaseSkeletonProps {
  lastLineWidth?: CSSProperties['width'];
  lines?: number;
  size?: SkeletonTextSize;
  widths?: CSSProperties['width'] | CSSProperties['width'][] | undefined;
}

function widthForLine(
  index: number,
  count: number,
  widths: SkeletonTextProps['widths'],
  lastLineWidth: CSSProperties['width'],
): CSSProperties['width'] {
  if (Array.isArray(widths)) {
    return widths[index] ?? widths[widths.length - 1] ?? '100%';
  }
  if (widths !== undefined) {
    return widths;
  }
  return index === count - 1 ? lastLineWidth : '100%';
}

export function SkeletonText({
  className,
  lastLineWidth = '68%',
  lines = 1,
  size = 'md',
  testId,
  widths,
}: SkeletonTextProps) {
  const count = Math.max(1, lines);
  return (
    <span className={cx('flex flex-col gap-1.5', className)} data-testid={testId}>
      {Array.from({ length: count }).map((_, index) => (
        <SkeletonBlock
          key={index}
          height={textHeights[size]}
          radius="full"
          width={widthForLine(index, count, widths, lastLineWidth)}
        />
      ))}
    </span>
  );
}

export interface SkeletonAvatarProps extends BaseSkeletonProps {
  size?: SkeletonAvatarSize;
}

function avatarSize(size: SkeletonAvatarSize): number {
  return typeof size === 'number' ? size : avatarSizes[size];
}

export function SkeletonAvatar({ className, size = 'md', testId }: SkeletonAvatarProps) {
  const px = avatarSize(size);
  return (
    <SkeletonBlock className={className} height={px} radius="full" testId={testId} width={px} />
  );
}

export interface SkeletonButtonProps extends BaseSkeletonProps {
  size?: SkeletonButtonSize;
  width?: CSSProperties['width'];
}

export function SkeletonButton({
  className,
  size = 'md',
  testId,
  width = '7rem',
}: SkeletonButtonProps) {
  return (
    <SkeletonBlock
      className={className}
      height={buttonHeights[size]}
      radius="md"
      testId={testId}
      width={width}
    />
  );
}

export interface SkeletonCardProps extends BaseSkeletonProps {
  children?: ReactNode;
  compact?: boolean;
}

export function SkeletonCard({ children, className, compact = false, testId }: SkeletonCardProps) {
  return (
    <div
      aria-hidden="true"
      className={cx('skeleton-card flex flex-col', compact ? 'gap-3 p-3' : 'gap-4 p-4', className)}
      data-testid={testId}
    >
      {children ?? (
        <>
          <div className="flex items-center gap-3">
            <SkeletonAvatar size="md" />
            <SkeletonText className="min-w-0 flex-1" lines={2} size="sm" widths={['60%', '38%']} />
          </div>
          <SkeletonText lines={3} size="sm" widths={['92%', '76%', '54%']} />
        </>
      )}
    </div>
  );
}

export interface SkeletonTableProps extends BaseSkeletonProps {
  columns?: number;
  columnWidths?: CSSProperties['width'][] | undefined;
  mode?: 'cards' | 'scroll';
  rows?: number;
  showHeader?: boolean;
}

function columnWidth(index: number, widths: CSSProperties['width'][] | undefined) {
  return widths?.[index] ?? (index === 0 ? '72%' : '56%');
}

export function SkeletonTable({
  className,
  columns = 4,
  columnWidths,
  mode = 'cards',
  rows = 5,
  showHeader = true,
  testId,
}: SkeletonTableProps) {
  const columnCount = Math.max(1, columns);
  const rowCount = Math.max(1, rows);
  const templateColumns = `repeat(${String(columnCount)}, minmax(7rem, 1fr))`;
  const cells = Array.from({ length: columnCount });
  const tableRows = Array.from({ length: rowCount });

  return (
    <div
      aria-hidden="true"
      className={cx(mode === 'scroll' ? 'overflow-x-auto' : '', className)}
      data-testid={testId}
      role="presentation"
    >
      <div className={cx('skeleton-card min-w-[38rem]', mode === 'cards' ? 'hidden sm:block' : '')}>
        {showHeader ? (
          <div
            className="grid gap-4 border-b border-border px-3 py-3"
            style={{ gridTemplateColumns: templateColumns }}
          >
            {cells.map((_, index) => (
              <SkeletonText key={index} size="xs" widths={columnWidth(index, columnWidths)} />
            ))}
          </div>
        ) : null}
        <div className="divide-y divide-border">
          {tableRows.map((_, rowIndex) => (
            <div
              key={rowIndex}
              className="grid min-h-[var(--row-h)] items-center gap-4 px-3 py-2"
              style={{ gridTemplateColumns: templateColumns }}
            >
              {cells.map((__, cellIndex) => (
                <SkeletonText
                  key={cellIndex}
                  size="sm"
                  widths={columnWidth(cellIndex, columnWidths)}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
      {mode === 'cards' ? (
        <div className="flex flex-col gap-2 sm:hidden">
          {tableRows.map((_, rowIndex) => (
            <SkeletonCard key={rowIndex} compact>
              <SkeletonText lines={2} size="sm" widths={['68%', '42%']} />
              <div className="grid grid-cols-2 gap-3">
                <SkeletonText size="xs" widths="70%" />
                <SkeletonText size="xs" widths="58%" />
                <SkeletonText size="xs" widths="52%" />
                <SkeletonText size="xs" widths="64%" />
              </div>
            </SkeletonCard>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export interface SkeletonChartProps extends BaseSkeletonProps {
  height?: CSSProperties['height'];
  legendItems?: number;
}

export function SkeletonChart({
  className,
  height = '16rem',
  legendItems = 3,
  testId,
}: SkeletonChartProps) {
  return (
    <SkeletonCard className={className} testId={testId}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SkeletonText className="min-w-32" size="md" widths="42%" />
        <div className="flex gap-2">
          {Array.from({ length: Math.max(1, legendItems) }).map((_, index) => (
            <SkeletonBlock key={index} height={10} radius="full" width={index === 0 ? 56 : 42} />
          ))}
        </div>
      </div>
      <SkeletonBlock className="skeleton-chart-plot" height={height} radius="md" />
    </SkeletonCard>
  );
}

export interface SkeletonFormProps extends BaseSkeletonProps {
  fields?: number;
  showActions?: boolean;
}

export function SkeletonForm({
  className,
  fields = 3,
  showActions = true,
  testId,
}: SkeletonFormProps) {
  return (
    <SkeletonCard className={className} testId={testId}>
      {Array.from({ length: Math.max(1, fields) }).map((_, index) => (
        <div key={index} className="flex flex-col gap-2">
          <SkeletonText size="xs" widths={index === 0 ? '36%' : '28%'} />
          <SkeletonBlock height="var(--row-h)" radius="md" />
        </div>
      ))}
      {showActions ? (
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <SkeletonButton size="sm" width="5.5rem" />
          <SkeletonButton width="7rem" />
        </div>
      ) : null}
    </SkeletonCard>
  );
}

export interface SkeletonPageProps extends BaseSkeletonProps {
  announce?: boolean;
  children: ReactNode;
  label: string;
}

export function SkeletonPage({
  announce = true,
  children,
  className,
  label,
  testId,
}: SkeletonPageProps) {
  return (
    <div
      aria-busy="true"
      aria-live={announce ? 'polite' : undefined}
      className={className}
      data-testid={testId}
      role={announce ? 'status' : undefined}
    >
      {announce ? <span className="sr-only">{label}</span> : null}
      {children}
    </div>
  );
}
