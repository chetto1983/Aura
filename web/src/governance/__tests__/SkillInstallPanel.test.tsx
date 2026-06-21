import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SkillsCatalogResult, SkillsInstallInfo } from '../governanceApi';

// SkillInstallPanel test (SKW-01, Claude-Code parity 2026-06-21). It mocks installSkill +
// searchSkillCatalog so it can assert the no-ceremony contract:
//  - catalog search is ON by default (no toggle, the search box is never disabled),
//  - a search hit fills the source field,
//  - the submit label is `Install` (never "Stage for approval"),
//  - NO RISKY badge and NO validation-checklist ceremony,
//  - a successful install reports "Installed and active." (direct activation),
//  - an empty source surfaces a safe inline error and fires NO request.

const installSkill = vi.fn();
const searchSkillCatalog = vi.fn();

vi.mock('../governanceApi', () => ({
  installSkill: (...a: unknown[]) => installSkill(...a) as Promise<SkillsInstallInfo>,
  searchSkillCatalog: (...a: unknown[]) => searchSkillCatalog(...a) as Promise<SkillsCatalogResult>,
}));

const { SkillInstallPanel } = await import('../SkillInstallPanel');

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function renderPanel(onClose: () => void = () => undefined) {
  const qc = client();
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(<SkillInstallPanel onClose={onClose} />, { wrapper: Wrapper });
}

const activeInstall: SkillsInstallInfo = {
  name: 'xlsx',
  source: 'anthropics/skills',
  content_hash: 'sha256:abc123',
  preview: 'SKILL preview body',
  destination: '/var/lib/aura/skills/xlsx',
  risk_tier: 'RISKY',
  status: 'active',
  approval_token: '',
  checklist: [{ label: 'parsed', passed: true }],
};

describe('SkillInstallPanel (SKW-01, no-ceremony)', () => {
  beforeEach(() => {
    installSkill.mockReset();
    searchSkillCatalog.mockReset();
    searchSkillCatalog.mockResolvedValue({ enabled: true, query: '', hits: [] });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('has NO RISKY badge and NO validation-checklist ceremony', () => {
    renderPanel();
    expect(screen.queryByText('RISKY')).toBeNull();
    expect(screen.queryByText(/supply-chain input/)).toBeNull();
    expect(screen.queryByText('Validation checklist')).toBeNull();
    expect(screen.queryByText('Injection-literal blocklist')).toBeNull();
  });

  it('submit is "Install" — never "Stage for approval"', () => {
    renderPanel();
    expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Stage for approval/ })).toBeNull();
  });

  it('search is enabled by default (no toggle) and renders catalog hits', async () => {
    searchSkillCatalog.mockResolvedValue({
      enabled: true,
      query: 'xlsx',
      hits: [{ source: 'anthropics/skills', skill: 'xlsx', installs: '12K' }],
    });
    renderPanel();
    const search = screen.getByPlaceholderText('Search the skills.sh catalog');
    expect(search.hasAttribute('disabled')).toBe(false);
    fireEvent.change(search, { target: { value: 'xlsx' } });
    await waitFor(() => {
      expect(searchSkillCatalog).toHaveBeenCalledWith('xlsx');
    });
    const hit = await screen.findByRole('button', { name: /anthropics\/skills@xlsx/ });
    // Clicking a hit fills the source with the full installable spec (owner/repo@skill).
    fireEvent.click(hit);
    expect(screen.getByDisplayValue('anthropics/skills@xlsx')).toBeTruthy();
  });

  it('shows a safe inline error + fires NO request on an empty source', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Install' }));
    const field = screen.getByLabelText('Source');
    expect(field.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByRole('alert').textContent).toContain('Enter a valid source');
    expect(installSkill).not.toHaveBeenCalled();
  });

  it('installs a valid source and reports it active (direct activation, no queue)', async () => {
    installSkill.mockResolvedValue(activeInstall);
    renderPanel();
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'anthropics/skills' } });
    fireEvent.click(screen.getByRole('button', { name: 'Install' }));

    await waitFor(() => {
      expect(installSkill).toHaveBeenCalledWith('anthropics/skills');
    });
    await waitFor(() => {
      expect(screen.getByText('/var/lib/aura/skills/xlsx')).toBeTruthy();
    });
    expect(screen.getByText('Installed and active.')).toBeTruthy();
    // No "staged/queued" copy anywhere.
    expect(screen.queryByText(/Staged for approval/)).toBeNull();
  });

  it('a failed install surfaces the generic governance error, not the source-format hint', async () => {
    installSkill.mockRejectedValue(new Error('HTTP 502'));
    renderPanel();
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'owner/repo' } });
    fireEvent.click(screen.getByRole('button', { name: 'Install' }));

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain("Couldn't load this board");
      expect(alert.textContent).not.toContain('Enter a valid source');
    });
  });

  it('marks the submit button aria-busy while the install is in flight', async () => {
    let resolveInstall: (info: SkillsInstallInfo) => void = () => undefined;
    installSkill.mockImplementation(
      () =>
        new Promise<SkillsInstallInfo>((resolve) => {
          resolveInstall = resolve;
        }),
    );
    renderPanel();
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'owner/repo' } });
    const submit = screen.getByRole('button', { name: 'Install' });
    fireEvent.click(submit);

    await waitFor(() => {
      expect(submit.getAttribute('aria-busy')).toBe('true');
      expect(submit.hasAttribute('disabled')).toBe(true);
    });

    resolveInstall(activeInstall);
    await waitFor(() => {
      expect(submit.getAttribute('aria-busy')).toBe('false');
    });
  });

  it('Cancel calls onClose without a request', () => {
    const onClose = vi.fn();
    renderPanel(onClose);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(installSkill).not.toHaveBeenCalled();
  });
});
