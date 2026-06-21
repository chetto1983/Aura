import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SkillsCatalogResult, SkillsInstallInfo } from '../governanceApi';

// SkillInstallPanel test (SKW-01 / prohibitions #5/#8). It mocks installSkill +
// searchSkillCatalog so it can assert:
//  - the panel ALWAYS renders the RISKY badge (install is never "safe"),
//  - EXACTLY five checklist items (no --ignore-scripts item — D-09),
//  - the submit label is `Stage for approval` (never Install/Activate),
//  - the search box is DISABLED with a note when the External-discovery toggle is off,
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

describe('SkillInstallPanel (SKW-01)', () => {
  beforeEach(() => {
    installSkill.mockReset();
    searchSkillCatalog.mockReset();
    searchSkillCatalog.mockResolvedValue({ enabled: false, query: '', hits: [] });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('always renders the RISKY badge (install is never "safe")', () => {
    renderPanel();
    expect(screen.getByText('RISKY')).toBeTruthy();
    expect(screen.getByText(/supply-chain input/)).toBeTruthy();
  });

  it('renders EXACTLY five checklist items (no --ignore-scripts item, D-09)', () => {
    renderPanel();
    expect(screen.getByText('Sanitized env')).toBeTruthy();
    expect(screen.getByText('SKILL.md parse')).toBeTruthy();
    expect(screen.getByText('Body cap')).toBeTruthy();
    expect(screen.getByText('Injection-literal blocklist')).toBeTruthy();
    expect(screen.getByText('Sanitized name/path')).toBeTruthy();
    // No --ignore-scripts chip anywhere in the panel (the honest-risk framing, D-09).
    expect(screen.queryByText(/ignore-scripts/i)).toBeNull();
    // The container-isolation note IS present.
    expect(screen.getByText(/Runs in Aura's container/)).toBeTruthy();
  });

  it('submit is "Stage for approval" — never Install/Activate', () => {
    renderPanel();
    expect(screen.getByRole('button', { name: 'Stage for approval' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Install$/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Activate/ })).toBeNull();
  });

  it('disables the search box with a note when External discovery is OFF (default)', () => {
    renderPanel();
    const search = screen.getByPlaceholderText('Search the skills.sh catalog');
    expect(search.hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Off — enable to search the public skills.sh catalog.')).toBeTruthy();
    // No catalog search fires while the toggle is off.
    expect(searchSkillCatalog).not.toHaveBeenCalled();
  });

  it('enables the search box when the External-discovery toggle is turned on', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'External discovery (skills.sh)' }));
    const search = screen.getByPlaceholderText('Search the skills.sh catalog');
    expect(search.hasAttribute('disabled')).toBe(false);
  });

  it('shows a safe inline error + fires NO request on an empty source', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Stage for approval' }));
    const field = screen.getByLabelText('Source');
    expect(field.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByRole('alert').textContent).toContain('Enter a valid source');
    expect(installSkill).not.toHaveBeenCalled();
  });

  it('stages a valid source and renders the resolved checklist + queued confirmation', async () => {
    installSkill.mockResolvedValue({
      name: 'fancy-skill',
      source: 'owner/fancy-skill',
      content_hash: 'sha256:abc123',
      preview: 'SKILL preview body',
      destination: '~/.aura/skills/pending/fancy-skill',
      risk_tier: 'RISKY',
      status: 'pending_approval',
      approval_token: 'tok-1',
      checklist: [
        { label: 'Sanitized env', passed: true },
        { label: 'SKILL.md parse', passed: true },
        { label: 'Body cap', passed: true },
        { label: 'Injection-literal blocklist', passed: true },
        { label: 'Sanitized name/path', passed: true },
      ],
    } satisfies SkillsInstallInfo);

    renderPanel();
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'owner/fancy-skill' } });
    fireEvent.click(screen.getByRole('button', { name: 'Stage for approval' }));

    await waitFor(() => {
      expect(installSkill).toHaveBeenCalledWith('owner/fancy-skill');
    });
    // The staged surface shows the hash + the queued confirmation.
    await waitFor(() => {
      expect(screen.getByText('sha256:abc123')).toBeTruthy();
    });
    expect(screen.getByText(/Staged for approval/)).toBeTruthy();
  });

  it('a failed install surfaces the generic governance error, not the source-format hint', async () => {
    installSkill.mockRejectedValue(new Error('HTTP 502'));
    renderPanel();
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'owner/repo' } });
    fireEvent.click(screen.getByRole('button', { name: 'Stage for approval' }));

    await waitFor(() => {
      // The service-failure alert must NOT blame the operator's input.
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
    const submit = screen.getByRole('button', { name: 'Stage for approval' });
    fireEvent.click(submit);

    await waitFor(() => {
      expect(submit.getAttribute('aria-busy')).toBe('true');
      expect(submit.hasAttribute('disabled')).toBe(true);
    });

    resolveInstall({
      name: 'demo',
      source: 'owner/repo',
      content_hash: 'sha256:abc',
      preview: '# demo',
      destination: '~/.aura/skills/pending/demo',
      risk_tier: 'RISKY',
      status: 'pending_approval',
      approval_token: 'tok-1',
      checklist: [{ label: 'parsed', passed: true }],
    });
    await waitFor(() => {
      expect(submit.getAttribute('aria-busy')).toBe('false');
    });
  });

  it('Discard install calls onClose without a request', () => {
    const onClose = vi.fn();
    renderPanel(onClose);
    fireEvent.click(screen.getByRole('button', { name: 'Discard install' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(installSkill).not.toHaveBeenCalled();
  });
});
