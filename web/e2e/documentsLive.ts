import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createInterface } from 'node:readline';
import type { Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { sameOriginFetch } from './live';
import { auraCommand } from './support/auraCli';

// What the documents live specs share: the Documenti view opened in Italian, the documents API
// called as the signed-in operator, and the agent's own document_search, reached through
// `aura docs mcp` in the running container.

/** openDocuments signs in and lands on the Documenti view, in Italian. */
export async function openDocuments(page: Page): Promise<void> {
  // Registered before gotoAuthenticated's own script, which only fills an empty language slot.
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.language', 'it');
    window.localStorage.setItem('aura.shell.surface', 'documents');
  });
  // The Documents surface opens on its overview; the list the specs drive is the library.
  await gotoAuthenticated(page, '/documents/library');
}

/** api calls the documents API as the operator and decodes the answer, failing on any error. */
export async function api<T>(page: Page, method: string, url: string, body?: unknown): Promise<T> {
  const response = await sameOriginFetch(page, url, {
    method,
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (response.status < 200 || response.status > 299) {
    throw new Error(`${method} ${url}: HTTP ${String(response.status)} ${response.text}`);
  }
  return JSON.parse(response.text) as T;
}

export interface LiveValue {
  readonly field: string;
  readonly value_key: string;
  readonly set_by: string;
  readonly accepted: boolean;
}

export interface LiveRow {
  readonly id: string;
  readonly source_key: string;
  readonly file_name: string;
  readonly values: readonly LiveValue[];
  readonly needs_review: boolean;
  readonly reviewed_at?: string;
}

interface LiveLabel {
  readonly id: string;
  readonly name: string;
}

/** ensureLabel returns the id of the label of that name on a list, creating it when missing. */
export async function ensureLabel(
  page: Page,
  kind: 'type' | 'counterparty' | 'tag',
  name: string,
  rule: { readonly match_algorithm: string; readonly match_pattern: string } = {
    match_algorithm: 'none',
    match_pattern: '',
  },
): Promise<string> {
  const { labels } = await api<{ labels: readonly LiveLabel[] }>(
    page,
    'GET',
    `/api/documents/labels?kind=${kind}`,
  );
  const held = labels.find((label) => label.name.toLowerCase() === name.toLowerCase());
  if (held !== undefined) return held.id;
  const saved = await api<{ label: LiveLabel }>(page, 'POST', '/api/documents/labels', {
    kind,
    name,
    case_insensitive: true,
    ...rule,
  });
  return saved.label.id;
}

/** documentsNamed are the indexed documents with exactly that file name. */
export async function documentsNamed(page: Page, fileName: string): Promise<readonly LiveRow[]> {
  const page1 = await api<{ rows: readonly LiveRow[] }>(
    page,
    'GET',
    `/api/documents?q=${encodeURIComponent(fileName)}&limit=50`,
  );
  return page1.rows.filter((row) => row.file_name === fileName);
}

/** The values one document must hold after review: a key per single field, and its tags. */
export interface Wanted {
  readonly single: Readonly<Record<string, string>>;
  readonly tags: readonly string[];
}

/** reviewAs makes a document hold exactly the wanted values, as the operator, and marks it
 * reviewed: the operator's review the test stands in for. */
export async function reviewAs(page: Page, id: string, wanted: Wanted): Promise<void> {
  const doc = await api<LiveRow>(page, 'GET', `/api/documents/${encodeURIComponent(id)}`);
  const reviewed = (v: LiveValue) => v.set_by === 'operator' || v.accepted;
  const set: { field: string; value_key: string }[] = [];
  const remove: { field: string; value_key: string }[] = [];
  for (const [field, key] of Object.entries(wanted.single)) {
    if (!doc.values.some((v) => v.field === field && v.value_key === key && reviewed(v))) {
      set.push({ field, value_key: key });
    }
  }
  for (const tag of wanted.tags) {
    if (!doc.values.some((v) => v.field === 'tag' && v.value_key === tag && reviewed(v))) {
      set.push({ field: 'tag', value_key: tag });
    }
  }
  for (const v of doc.values) {
    const kept = v.field === 'tag' ? wanted.tags.includes(v.value_key) : v.field in wanted.single;
    if (!kept) remove.push({ field: v.field, value_key: v.value_key });
  }
  if (set.length + remove.length > 0) {
    await api(page, 'PATCH', `/api/documents/${encodeURIComponent(id)}`, { set, remove });
  }
  await api(page, 'POST', `/api/documents/${encodeURIComponent(id)}/review`);
}

const readToolOutput = 'read_tool_output';

/** A page of read_tool_output stays under the 30,000-byte preview cap with its own footer. */
const READ_PAGE_BYTES = 25_000;

interface ToolAnswer {
  readonly content: readonly { readonly text: string }[];
  readonly structuredContent?: SearchAnswer;
  readonly isError?: boolean;
}

interface RPCMessage {
  readonly id?: number;
  readonly result?: unknown;
  readonly error?: { readonly message: string };
}

/** DocsMCP speaks MCP over stdio to `aura docs mcp`, which serves the agent's own document
 * tools from the production registry. */
export class DocsMCP {
  private readonly child: ChildProcessWithoutNullStreams;
  private readonly waiting = new Map<number, (message: RPCMessage) => void>();
  private nextID = 1;

  private constructor() {
    const [program, argv] = auraCommand(['docs', 'mcp'], true);
    this.child = spawn(program, argv);
    createInterface({ input: this.child.stdout }).on('line', (line) => {
      const message = JSON.parse(line) as RPCMessage;
      if (message.id === undefined) return;
      this.waiting.get(message.id)?.(message);
      this.waiting.delete(message.id);
    });
  }

  static async start(): Promise<DocsMCP> {
    const mcp = new DocsMCP();
    await mcp.request('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'documents-live-e2e', version: '1' },
    });
    mcp.child.stdin.write(
      `${JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' })}\n`,
    );
    return mcp;
  }

  private request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.waiting.set(id, (message) => {
        if (message.error !== undefined) reject(new Error(`${method}: ${message.error.message}`));
        else resolve(message.result);
      });
      this.child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`);
    });
  }

  private async call(name: string, args: Record<string, unknown>): Promise<ToolAnswer> {
    const result = (await this.request('tools/call', { name, arguments: args })) as ToolAnswer;
    if (result.isError === true) throw new Error(`${name}: ${result.content[0]?.text ?? ''}`);
    return result;
  }

  /** search calls document_search and returns its answer, as the agent reads it. An answer
   * over the preview cap comes back truncated with no structured content, and is paged back
   * with read_tool_output, as its footer tells the agent to. */
  async search(args: Record<string, unknown>): Promise<SearchAnswer> {
    const result = await this.call('document_search', args);
    if (result.structuredContent !== undefined) return result.structuredContent;
    const preview = result.content[0]?.text ?? '';
    const footer = /read_tool_output\(tool_call_id="([^"]+)"/.exec(preview);
    const total = /of (\d+)/.exec(preview.slice(preview.lastIndexOf('[output truncated')));
    if (footer?.[1] === undefined || total?.[1] === undefined) {
      throw new Error(`document_search: an answer with neither JSON nor a paging footer`);
    }
    let body = '';
    for (let offset = 0; offset < Number(total[1]);) {
      const page = await this.call(readToolOutput, {
        tool_call_id: footer[1],
        offset,
        limit: READ_PAGE_BYTES,
      });
      const text = page.content[0]?.text ?? '';
      const tail = /\n\n\[showing bytes \d+-(\d+) of \d+, next offset \d+[^\]]*\]$/.exec(text);
      if (tail?.[1] === undefined) throw new Error('read_tool_output: a page with no footer');
      body += text.slice(0, tail.index);
      offset = Number(tail[1]);
    }
    return JSON.parse(body) as SearchAnswer;
  }

  close(): void {
    this.child.stdin.end();
  }
}

export interface SearchAnswer {
  readonly status: string;
  readonly abstained: boolean;
  readonly abstention_reason?: string;
  readonly documents: readonly { readonly title: string }[];
  readonly filters?: readonly {
    readonly name: string;
    readonly admitted_documents: number;
    readonly rests_on_suggestions: boolean;
  }[];
  readonly indexing?: { readonly status: string; readonly in_progress?: number };
}
