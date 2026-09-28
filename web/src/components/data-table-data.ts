export interface DataTableSort {
  key: string;
  direction: 'asc' | 'desc';
}

export function compareDataTableValues(a: string, b: string, locale = 'en-US'): number {
  const aNum = a.trim() !== '' && Number.isFinite(Number(a));
  const bNum = b.trim() !== '' && Number.isFinite(Number(b));
  if (aNum && bNum) return Number(a) - Number(b);
  return a.localeCompare(b, locale);
}

export function nextDataTableSort(
  current: DataTableSort | null,
  key: string,
): DataTableSort | null {
  if (current?.key !== key) return { key, direction: 'asc' };
  if (current.direction === 'asc') return { key, direction: 'desc' };
  return null;
}
