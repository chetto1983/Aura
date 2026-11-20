import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import SettingsWorkspace from '../SettingsWorkspace';

// The sub-panels have their own dedicated tests + own network calls; mock the ones this suite
// navigates to so it focuses on the SettingsWorkspace obligations: the capability gate, the
// rail, and the rule that only the SELECTED pane mounts.
vi.mock('../IdentityRoster', () => ({
  IdentityRoster: () => <div>Roster panel</div>,
}));
vi.mock('../TelegramSettingsPanel', () => ({
  TelegramSettingsPanel: () => <div>Telegram panel</div>,
}));
vi.mock('../SharedLinksSection', () => ({
  SharedLinksSection: () => <div>Shared links panel</div>,
}));
vi.mock('../remoteAccess/RemoteAccessPanel', () => ({
  RemoteAccessPanel: () => <div>Remote access panel</div>,
}));

vi.mock('../mail/MailAccountsSection', () => ({
  MailAccountsSection: () => <div>Mail accounts panel</div>,
}));

const SECTION_LABELS = [
  'Your profile',
  'Mail and calendar',
  'Model routing',
  'Token and turn budget',
  'Sidecar and cloud backends',
  'Identities & access',
  'Telegram',
  'Shared links',
  'Remote access',
];

const ADMIN = [
  'identity.create',
  'identity.delete',
  'agent.run',
  'governance.read',
  'governance.write',
  'share.public',
];
// Every identity holds governance.write; what a member lacks is identity.create.
const MEMBER = ['agent.run', 'governance.read', 'governance.write'];

function stubFetch(capabilities: readonly string[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/api/me')) {
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'local', capabilities }), { status: 200 }),
        );
      }
      if (url.includes('/api/profile')) {
        return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }));
      }
      return Promise.resolve(
        new Response(JSON.stringify({ restart_required: false, settings: [] }), { status: 200 }),
      );
    }),
  );
}

function renderWorkspace(onCreateIdentity = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SettingsWorkspace onCreateIdentity={onCreateIdentity} />
    </QueryClientProvider>,
  );
}

describe('SettingsWorkspace', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('gives an admin a rail of every section and opens the profile pane first', async () => {
    stubFetch(ADMIN);
    renderWorkspace();

    const rail = await screen.findByRole('navigation', { name: 'Settings sections' });
    for (const label of SECTION_LABELS) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy();
    }
    // The rail is grouped, and the captions survive at every width (sr-only on the strip).
    expect(rail.textContent).toContain('Runtime');
    expect(rail.textContent).toContain('Access & channels');

    expect(screen.getByRole('button', { name: 'Your profile' }).getAttribute('aria-current')).toBe(
      'page',
    );
  });

  it('mounts only the selected pane, not every panel on the surface', async () => {
    stubFetch(ADMIN);
    renderWorkspace();

    await screen.findByRole('navigation', { name: 'Settings sections' });
    // On the profile pane the admin panels must not be mounted — that unmounted state is the
    // whole point of the rail (the old single scroll fired all of their queries at once).
    expect(screen.queryByText('Roster panel')).toBeNull();
    expect(screen.queryByText('Telegram panel')).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Model routing' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Telegram' }));
    expect(screen.getByText('Telegram panel')).toBeTruthy();
  });

  it('scopes a runtime pane to its own group', async () => {
    stubFetch(ADMIN);
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: 'Token and turn budget' }));

    expect(await screen.findByRole('heading', { name: 'Token and turn budget' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Model routing' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Sidecar and cloud backends' })).toBeNull();
  });

  it('exposes identity creation and the roster on one pane', async () => {
    const onCreateIdentity = vi.fn();
    stubFetch(ADMIN);
    renderWorkspace(onCreateIdentity);

    fireEvent.click(await screen.findByRole('button', { name: 'Identities & access' }));
    expect(screen.getByText('Roster panel')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    expect(onCreateIdentity).toHaveBeenCalledOnce();
  });

  it('remembers the pane across mounts', async () => {
    stubFetch(ADMIN);
    const first = renderWorkspace();
    fireEvent.click(await screen.findByRole('button', { name: 'Shared links' }));
    expect(screen.getByText('Shared links panel')).toBeTruthy();
    first.unmount();

    renderWorkspace();
    expect(await screen.findByText('Shared links panel')).toBeTruthy();
  });

  it('gives a member a rail of the personal sections only', async () => {
    stubFetch(MEMBER);
    renderWorkspace();

    const rail = await screen.findByRole('navigation', { name: 'Settings sections' });
    expect(
      within(rail)
        .getAllByRole('button')
        .map((button) => button.textContent.trim()),
    ).toEqual(['Your profile', 'Mail and calendar']);
    expect(rail.textContent).not.toContain('Runtime');
    expect(screen.queryByRole('heading', { name: 'Model routing' })).toBeNull();
    expect(screen.queryByText('Roster panel')).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Admin access required' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Mail and calendar' }));
    expect(screen.getByText('Mail accounts panel')).toBeTruthy();
  });

  it('keeps the ChatGPT account on the profile pane for a member, not for an admin', async () => {
    stubFetch(MEMBER);
    const member = renderWorkspace();
    expect(await screen.findByRole('heading', { name: 'Your ChatGPT account' })).toBeTruthy();
    member.unmount();

    stubFetch(ADMIN);
    renderWorkspace();
    await screen.findByRole('button', { name: 'Model routing' });
    expect(screen.queryByRole('heading', { name: 'Your ChatGPT account' })).toBeNull();
  });

  it('falls back to a visible pane when the remembered one is admin-only', async () => {
    localStorage.setItem('aura.settings.section', 'backends');
    stubFetch(MEMBER);
    renderWorkspace();

    const profile = await screen.findByRole('button', { name: 'Your profile' });
    expect(profile.getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('heading', { name: 'Sidecar and cloud backends' })).toBeNull();
  });
});
