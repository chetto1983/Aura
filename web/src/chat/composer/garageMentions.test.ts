import type { IEntity } from '@svar-ui/react-filemanager';
import { describe, expect, it, vi } from 'vitest';
import {
  createGarageDirectiveFormatter,
  createGarageMentionFetcher,
  resolveGarageMentions,
  serializeGarageMention,
} from './garageMentions';

describe('Garage mention text and structural scope', () => {
  it('keeps a readable quoted token and resolves exact file/folder scope beside the text', () => {
    const file = {
      id: 'file:finance/report 2026.pdf',
      type: 'garage-file',
      label: 'report 2026.pdf',
      metadata: { kind: 'file', path: 'finance/report 2026.pdf' },
    } as const;
    expect(serializeGarageMention(file)).toBe('@file:"/finance/report 2026.pdf"');

    const text = `Review ${serializeGarageMention(file)} and @folder:"/policies/current".`;
    expect(resolveGarageMentions(text)).toEqual([
      { kind: 'file', path: 'finance/report 2026.pdf' },
      { kind: 'folder', path: 'policies/current' },
    ]);
    expect(createGarageDirectiveFormatter().parse(text)).toEqual([
      { kind: 'text', text: 'Review ' },
      {
        kind: 'mention',
        type: 'garage-file',
        label: '/finance/report 2026.pdf',
        id: 'file:finance/report 2026.pdf',
      },
      { kind: 'text', text: ' and ' },
      {
        kind: 'mention',
        type: 'garage-folder',
        label: '/policies/current',
        id: 'folder:policies/current',
      },
      { kind: 'text', text: '.' },
    ]);
  });

  it('deduplicates scope and ignores malformed, traversal-shaped and email-like text', () => {
    expect(
      resolveGarageMentions(
        '@file:"/a.pdf" @file:"/a.pdf" x@y.com @folder:"../private" @file:not-json',
      ),
    ).toEqual([{ kind: 'file', path: 'a.pdf' }]);
  });
});

describe('Garage assistant-ui completion source', () => {
  it('reads only the addressed folder page and filters the returned children', async () => {
    const list = vi.fn<(path: string) => Promise<readonly IEntity[]>>().mockResolvedValue([
      { id: '/finance/q1.pdf', type: 'file', name: 'Quarter one.pdf' },
      { id: '/finance/q2.pdf', type: 'file', name: 'Quarter two.pdf' },
      { id: '/finance/archive', type: 'folder' },
    ]);

    const items = await createGarageMentionFetcher(list)('file:finance/quarter t');

    expect(list).toHaveBeenCalledOnce();
    expect(list).toHaveBeenCalledWith('/finance');
    expect(items.map((item) => item.label)).toEqual(['Quarter two.pdf']);
    expect(items[0]?.metadata).toEqual({ kind: 'file', path: 'finance/q2.pdf' });
  });

  it('reuses one folder listing while the operator refines the name', async () => {
    const list = vi
      .fn<(path: string) => Promise<readonly IEntity[]>>()
      .mockResolvedValue([{ id: '/finance/q1.pdf', type: 'file', name: 'Quarter one.pdf' }]);
    const fetch = createGarageMentionFetcher(list);

    await fetch('finance/q');
    await fetch('finance/quar');
    await fetch('finance/quarter one');

    expect(list).toHaveBeenCalledOnce();
    expect(list).toHaveBeenCalledWith('/finance');
  });

  it('lists folders before files for a bare @ without walking descendants', async () => {
    const list = vi.fn<(path: string) => Promise<readonly IEntity[]>>().mockResolvedValue([
      { id: '/z.pdf', type: 'file' },
      { id: '/finance', type: 'folder' },
    ]);

    const items = await createGarageMentionFetcher(list)('');

    expect(list).toHaveBeenCalledWith('');
    expect(items.map((item) => item.label)).toEqual(['finance', 'z.pdf']);
    expect(items[0]).toMatchObject({
      type: 'garage-folder-nav',
      metadata: { kind: 'folder', path: 'finance', browse: true },
    });
    const rootFolder = items[0];
    if (rootFolder === undefined) throw new Error('missing root folder completion');
    expect(serializeGarageMention(rootFolder)).toBe('@finance/');
  });

  it('offers the addressed folder itself before its immediate children', async () => {
    const list = vi.fn<(path: string) => Promise<readonly IEntity[]>>().mockResolvedValue([
      { id: '/finance/q1.pdf', type: 'file' },
      { id: '/finance/archive', type: 'folder' },
    ]);

    const items = await createGarageMentionFetcher(list)('finance/');

    expect(list).toHaveBeenCalledWith('/finance');
    expect(items.map((item) => item.type)).toEqual([
      'garage-folder',
      'garage-folder-nav',
      'garage-file',
    ]);
    expect(items[0]?.metadata).toEqual({ kind: 'folder', path: 'finance' });
    const currentFolder = items[0];
    if (currentFolder === undefined) throw new Error('missing current folder completion');
    expect(serializeGarageMention(currentFolder)).toBe('@folder:"/finance"');
  });
});
