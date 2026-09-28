// Adapted from Tool UI Stats Display at revision
// 49a870286facdbf28160cd647f0d337ebdc9b275 (stats-display registry JSON
// SHA-256 c705f6d3e94f57be556b0651005276dbe9e579f36e82a9142d4063a723da2c51).
// MIT license: LICENSE.tool-ui. Aura supplies bounded read-only counts and copy.

export interface ToolStat {
  readonly key: string;
  readonly label: string;
  readonly value: number;
}

export function ToolStats({
  id,
  title,
  stats,
  locale,
}: {
  readonly id: string;
  readonly title: string;
  readonly stats: readonly ToolStat[];
  readonly locale: string;
}) {
  const formatter = new Intl.NumberFormat(locale);
  return (
    <article
      data-slot="stats-display"
      data-tool-ui-id={id}
      className="w-full max-w-xl overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface-2"
    >
      <h3 className="border-b border-border px-4 py-3 text-sm font-medium text-text">{title}</h3>
      <dl className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,180px),1fr))]">
        {stats.map((stat) => (
          <div key={stat.key} className="min-w-0 border-b border-border px-5 py-4 last:border-b-0">
            <dt className="text-xs tracking-wide text-text-muted uppercase">{stat.label}</dt>
            <dd className="mt-1 text-3xl font-light tabular-nums text-text">
              {formatter.format(stat.value)}
            </dd>
          </div>
        ))}
      </dl>
    </article>
  );
}
