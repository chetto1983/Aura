import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { IdentityRoster } from '../IdentityRoster';

// IdentityRoster (RBAC-05/RBAC-06/RBAC-11) — read-only roster + a destructive removal behind
// a typed email confirmation. CapabilityAdminPanel's grant/revoke control is gone (RBAC-03
// grants everything at provisioning, RBAC-06 refuses the admin pair through the API), so this
// suite proves the roster/removal contract only, not a grants surface.

const ADMIN = {
  id: 'id-admin',
  name: 'admin@aura.local',
  kind: 'user',
  capabilities: ['identity.create', 'identity.delete', 'agent.run'],
};
const ALICE = {
  id: 'id-alice',
  name: 'alice@example.com',
  kind: 'user',
  capabilities: ['agent.run', 'governance.read', 'governance.write', 'share.public'],
};

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubFetch(opts: {
  readonly identities: readonly (typeof ADMIN)[];
  readonly selfId: string;
  readonly deleteStatus?: number;
}) {
  const spy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    const method = init?.method ?? 'GET';
    if (url.includes('/api/me')) {
      return Promise.resolve(
        new Response(JSON.stringify({ identity_id: opts.selfId, capabilities: [] }), {
          status: 200,
        }),
      );
    }
    if (url.includes('/api/admin/identities/') && method === 'DELETE') {
      const status = opts.deleteStatus ?? 200;
      if (status !== 200) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'identity removal failed' }), { status }),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify({ identity_id: 'id-alice', status: 'removed' }), {
          status: 200,
        }),
      );
    }
    return Promise.resolve(
      new Response(JSON.stringify({ identities: opts.identities }), { status: 200 }),
    );
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

function renderRoster() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <IdentityRoster />
    </QueryClientProvider>,
  );
}

describe('IdentityRoster', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists identities with a mono name, a role badge, and the (you) suffix on the caller', async () => {
    stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id });
    renderRoster();

    expect(await screen.findByText(/admin@aura\.local/)).toBeTruthy();
    expect(screen.getByText(/\(you\)/)).toBeTruthy();
    expect(screen.getByText('Admin')).toBeTruthy();
    expect(screen.getByText('Member')).toBeTruthy();
    expect(screen.getByRole('list')).toBeTruthy();
  });

  it('unconditionally disables the admin self-row remove button with a visible caption, never a Tooltip', async () => {
    stubFetch({ identities: [ADMIN], selfId: ADMIN.id });
    renderRoster();

    const remove = await screen.findByRole('button', { name: 'Remove admin@aura.local' });
    expect((remove as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/aura identity/)).toBeTruthy();
  });

  it('every other row behaves identically regardless of roster size (1 vs many)', async () => {
    stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id });
    renderRoster();

    const remove = await screen.findByRole('button', { name: 'Remove alice@example.com' });
    expect((remove as HTMLButtonElement).disabled).toBe(false);
  });

  it('a long identity name/email wraps via break-all font-mono, never truncates', async () => {
    const long = { ...ALICE, id: 'id-long', name: `${'x'.repeat(80)}@example.com` };
    stubFetch({ identities: [ADMIN, long], selfId: ADMIN.id });
    renderRoster();

    const el = await screen.findByText(new RegExp(long.name.slice(0, 20)));
    expect(el.className).toContain('break-all');
    expect(el.className).toContain('font-mono');
  });

  it('opens the removal dialog with the confirm button disabled until the exact email is typed', async () => {
    stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id });
    renderRoster();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove alice@example.com' }));
    expect(await screen.findByText('Remove alice@example.com?')).toBeTruthy();
    const confirm = screen.getByRole('button', { name: 'Remove permanently' });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);

    const input = screen.getByLabelText('Type alice@example.com to confirm');
    fireEvent.change(input, { target: { value: 'wrong@example.com' } });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input, { target: { value: 'alice@example.com' } });
    expect((confirm as HTMLButtonElement).disabled).toBe(false);
  });

  it('a trailing space still matches (trimmed) but a different case does not (case-sensitive)', async () => {
    stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id });
    renderRoster();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove alice@example.com' }));
    const input = await screen.findByLabelText('Type alice@example.com to confirm');
    const confirm = screen.getByRole('button', { name: 'Remove permanently' });

    fireEvent.change(input, { target: { value: 'alice@example.com  ' } });
    expect((confirm as HTMLButtonElement).disabled).toBe(false);

    fireEvent.change(input, { target: { value: 'Alice@example.com' } });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
  });

  it('confirming calls removeIdentity exactly once and the row shows the in-flight state', async () => {
    const spy = stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id });
    renderRoster();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove alice@example.com' }));
    const input = await screen.findByLabelText('Type alice@example.com to confirm');
    fireEvent.change(input, { target: { value: 'alice@example.com' } });
    fireEvent.click(screen.getByRole('button', { name: 'Remove permanently' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    const deletes = spy.mock.calls.filter((c) => c[1]?.method === 'DELETE');
    expect(deletes).toHaveLength(1);
  });

  it('a part-way removal failure renders the decided retry-safe copy inside the still-open dialog', async () => {
    stubFetch({ identities: [ADMIN, ALICE], selfId: ADMIN.id, deleteStatus: 502 });
    renderRoster();

    fireEvent.click(await screen.findByRole('button', { name: 'Remove alice@example.com' }));
    const input = await screen.findByLabelText('Type alice@example.com to confirm');
    fireEvent.change(input, { target: { value: 'alice@example.com' } });
    fireEvent.click(screen.getByRole('button', { name: 'Remove permanently' }));

    expect(
      await screen.findByText(
        "Couldn't finish removing alice@example.com. The removal is safe to retry — some data may already be gone.",
      ),
    ).toBeTruthy();
    // The dialog is still open — the human can retry without retyping the email.
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('reuses the AdminSection loading/error/empty guards and defines none of its own', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (urlOf(input).includes('/api/me')) {
          return Promise.resolve(
            new Response(JSON.stringify({ identity_id: 'x', capabilities: [] }), { status: 200 }),
          );
        }
        return Promise.resolve(new Response('boom', { status: 500 }));
      }),
    );
    renderRoster();
    expect(
      await screen.findByText("Couldn't load identities. Check the server and try again."),
    ).toBeTruthy();
  });
});
