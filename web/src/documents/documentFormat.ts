export function formatBytes(size: number): string {
  if (size < 1024) return `${String(size)} B`;
  const kb = size / 1024;
  if (kb < 1024) {
    return `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(kb)} KB`;
  }
  return `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(kb / 1024)} MB`;
}
