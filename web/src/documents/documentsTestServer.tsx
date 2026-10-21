import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { vi } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { DocumentDetail } from './DocumentDetail';
import DocumentsSurface from './DocumentsSurface';
import './DocumentsWorkspace';
import type {
  CustomField,
  DocumentDetail as Detail,
  DocumentRow,
  DocumentsPage,
  FieldValue,
  Label,
} from './documentsApi';

// The documents API as the view's tests see it: two invoices, the operator's lists, and a server
// that records what the view asks for.

export const LABELS: Readonly<Record<string, readonly Partial<Label>[]>> = {
  type: [
    {
      id: 't-fattura',
      kind: 'type',
      name: 'Fattura',
      match_algorithm: 'any',
      match_pattern: 'FATTURA',
      case_insensitive: true,
    },
    { id: 't-contratto', kind: 'type', name: 'Contratto', match_algorithm: 'none' },
  ],
  counterparty: [
    { id: 'c-gialli', kind: 'counterparty', name: 'Gialli Import' },
    { id: 'c-rossi', kind: 'counterparty', name: 'Rossi SRL' },
  ],
  tag: [{ id: 'g-ricambi', kind: 'tag', name: 'ricambi' }],
};

export const CUSTOM_FIELDS: readonly CustomField[] = [
  { id: 'cf-numero', name: 'Numero fattura', data_type: 'text' },
  { id: 'cf-stato', name: 'Stato', data_type: 'select', options: ['aperta', 'pagata'] },
];

function row(
  id: string,
  fileName: string,
  values: DocumentRow['values'],
  needsReview: boolean,
): DocumentRow {
  return {
    id,
    source_key: `chat/${fileName}`,
    file_name: fileName,
    size_bytes: 1024,
    indexed_at: '2026-10-18 10:00:00.000',
    record_id: values.length > 0 ? `r-${id}` : undefined,
    values,
    needs_review: needsReview,
    ...(needsReview || values.length === 0 ? {} : { reviewed_at: '2026-10-18T11:00:00Z' }),
  } as DocumentRow;
}

const value = (field: string, key: string, setBy: string, evidence?: string): FieldValue => ({
  field,
  value_key: key,
  set_by: setBy,
  accepted: false,
  set_at: '2026-10-18T10:00:00Z',
  ...(evidence === undefined ? {} : { evidence }),
});

export const PAGE: DocumentsPage = {
  rows: [
    row(
      'd1',
      'fattura_2026_031.pdf',
      [
        value('type', 't-fattura', 'operator'),
        value('counterparty', 'c-gialli', 'operator'),
        value('document_date', '2026-03-27', 'operator'),
        value('tag', 'g-ricambi', 'operator'),
      ],
      false,
    ),
    row(
      'd2',
      'fattura_2026_044.pdf',
      [
        value('type', 't-fattura', 'rule:t-fattura'),
        value('document_date', '2026-04-14', 'extractor'),
      ],
      true,
    ),
  ],
  total: 24,
  facets: {
    type: { 't-fattura': 20 },
    counterparty: { 'c-gialli': 9, 'c-rossi': 6 },
    tags: { 'g-ricambi': 4 },
    unassigned: { type: 4, counterparty: 3, tags: 20 },
  },
};

/** The documents as the detail reads them now: the second invoice has gained a counterparty the
 * extractor read since the list was loaded, and a delivery note waits next in the review list. */
export const DETAILS: Readonly<Record<string, Detail>> = {
  d2: {
    ...row(
      'd2',
      'fattura_2026_044.pdf',
      [
        value('type', 't-fattura', 'rule:t-fattura', 'any "FATTURA"'),
        value('counterparty', 'c-gialli', 'extractor', 'Cliente | Gialli Import'),
        value('document_date', '2026-04-14', 'extractor', 'Data emissione | 14/04/2026'),
        value('tag', 'g-ricambi', 'operator'),
      ],
      true,
    ),
    rejections: [],
    history: [
      {
        field: 'type',
        value_key: 't-fattura',
        action: 'set',
        actor: 'rule:t-fattura',
        at: '2026-10-18T10:00:00Z',
      },
      {
        field: 'tag',
        value_key: 'g-ricambi',
        action: 'set',
        actor: 'operator',
        at: '2026-10-18T10:05:00Z',
      },
    ],
  },
  d3: {
    ...row(
      'd3',
      'ddt_2026_118.pdf',
      [value('counterparty', 'new:Verdi SpA', 'extractor', 'Fornitore | Verdi SpA')],
      true,
    ),
    rejections: [],
    history: [],
  },
};

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/** A response the bulk route gives, in order; the last one repeats. */
export interface BulkAnswer {
  readonly status: number;
  readonly body: unknown;
}

/** One write the detail sent: a PATCH of a document, its review, or a new label. */
export interface Write {
  readonly method: string;
  readonly path: string;
  readonly body: unknown;
}

const json = (body: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(body), { status }));

/** stubServer answers the documents API, records every list request's query, every bulk
 * request's body and every other write, answers the bulk route with the given responses in turn,
 * and every other write with refuse when one is given. */
export function stubServer(
  reviewCount = 7,
  bulk: readonly BulkAnswer[] = [{ status: 200, body: { applied: 1 } }],
  refuse?: BulkAnswer,
) {
  const lists: URLSearchParams[] = [];
  const posts: unknown[] = [];
  const writes: Write[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(urlOf(input), 'http://cockpit.test');
      const method = init?.method ?? 'GET';
      if (url.pathname === '/api/me')
        return json({ identity_id: 'documents-test', capabilities: [] });
      if (url.pathname === '/api/documents/ingestion') return json({ indexing: null });
      if (url.pathname === '/api/documents/views' && method === 'GET') return json({ views: [] });
      const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
      if (url.pathname === '/api/documents/bulk') {
        posts.push(body);
        const answer = bulk[Math.min(posts.length, bulk.length) - 1] ?? { status: 200, body: {} };
        return json(answer.body, answer.status);
      }
      if (method !== 'GET') {
        writes.push({ method, path: url.pathname, body });
        if (refuse !== undefined) return json(refuse.body, refuse.status);
        if (url.pathname.startsWith('/api/documents/labels') && method !== 'DELETE') {
          return json({ label: { id: 'c-verdi', ...(body as object) }, suggested: 3 });
        }
        if (url.pathname.startsWith('/api/documents/custom-fields') && method !== 'DELETE') {
          return json({ id: 'cf-new', ...(body as object) });
        }
        return json({ ok: true });
      }
      if (url.pathname === '/api/documents/labels') {
        return json({ labels: LABELS[url.searchParams.get('kind') ?? ''] ?? [] });
      }
      if (url.pathname === '/api/documents/custom-fields') return json({ fields: CUSTOM_FIELDS });
      if (url.pathname.startsWith('/api/documents/')) {
        const id = url.pathname.slice('/api/documents/'.length);
        return id in DETAILS ? json(DETAILS[id]) : json({ error: 'not_found' }, 404);
      }
      if (url.pathname !== '/api/documents') return json({ error: 'not_found' }, 404);
      if (url.searchParams.get('needs_review') === 'true') {
        const limit = url.searchParams.get('limit');
        if (limit === '1') return json({ rows: [], total: reviewCount, facets: {} });
        if (limit === '2') return json({ rows: [DETAILS.d3], total: reviewCount - 1, facets: {} });
      }
      lists.push(url.searchParams);
      return json(PAGE);
    }),
  );
  return { lists, posts, writes };
}

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

export function renderDocuments(path = '/documents/library') {
  const router = createMemoryRouter([{ path: '/documents/*', element: <DocumentsSurface /> }], {
    initialEntries: [path],
  });
  return render(
    <QueryClientProvider client={newClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

/** renderDetail opens one document's detail on its own, with the callbacks it reports to. */
export function renderDetail(id: string) {
  const onClose = vi.fn();
  const onOpen = vi.fn();
  render(
    <QueryClientProvider client={newClient()}>
      <DocumentDetail id={id} onClose={onClose} onOpen={onOpen} />
    </QueryClientProvider>,
  );
  return { onClose, onOpen };
}
