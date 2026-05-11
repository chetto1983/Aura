import { useCallback } from 'react';
import { Link, useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api, ApiError } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';

export function WikiPageView() {
  const { t } = useLocale();
  const { slug = '' } = useParams<{ slug: string }>();
  const fetcher = useCallback(() => api.wikiPage(slug), [slug]);
  const { data, error, loading } = useApi(fetcher);

  if (loading) return <div className="p-6 text-sm text-muted-foreground">{t('wikiPage.loading')}</div>;

  if (error) {
    const is404 = error instanceof ApiError && error.status === 404;
    return (
      <div className="p-6">
        <Link to="/wiki" className="text-sm text-muted-foreground underline">{t('wikiPage.back')}</Link>
        <h1 className="mt-4 text-2xl font-semibold">{is404 ? t('wikiPage.notFound') : t('wikiPage.failed')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {is404 ? t('wikiPage.noSlug', { slug }) : error.message}
        </p>
      </div>
    );
  }
  if (!data) return null;

  const fm = data.frontmatter;
  const category = typeof fm.category === 'string' ? fm.category : undefined;
  const tags = Array.isArray(fm.tags) ? (fm.tags as string[]) : [];
  const unversioned = fm.unversioned === true || data.unversioned === true;

  return (
    <article className="p-6 max-w-3xl">
      <Link to="/wiki" className="text-sm text-muted-foreground underline">{t('wikiPage.back')}</Link>
      <header className="mt-4 mb-6">
        <div className="flex flex-wrap items-start gap-2">
          <h1 className="text-3xl font-semibold">{data.title}</h1>
          {unversioned && (
            <span
              className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800 border border-yellow-300 mt-1"
              title={t('wikiPage.unversionedTitle')}
              aria-label={t('wikiPage.unversionedBadge')}
            >
              {t('wikiPage.unversionedBadge')}
            </span>
          )}
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {category && <span className="rounded-full border px-2 py-0.5">{category}</span>}
          {tags.map((tag) => (
            <span key={tag} className="rounded-full bg-muted px-2 py-0.5">#{tag}</span>
          ))}
        </div>
      </header>
      <div className="prose prose-sm dark:prose-invert max-w-none">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{data.body_md}</ReactMarkdown>
      </div>
    </article>
  );
}
