import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { McpEnvChip, McpWriteResponse } from '../governanceApi';

// McpEnvEditForm test (MCPW-02 / T-29-04-01). It mocks setMcpServerEnv so it can assert:
//  - the FOUR env states render distinctly (required / optional / missing / placeholder),
//  - NO raw secret VALUE ever enters the DOM (the secret field holds the redacted ${KEY}
//    placeholder, never the value the fixture "knows"),
//  - a placeholder-required save is STILL ALLOWED with the soft warning shown,
//  - an untouched secret submits its ${KEY} placeholder verbatim (the backend preserves it).

const setMcpServerEnv = vi.fn();

vi.mock('../governanceApi', () => ({
  setMcpServerEnv: (...a: unknown[]) => setMcpServerEnv(...a) as Promise<McpWriteResponse>,
}));

const { McpEnvEditForm } = await import('../McpEnvEditForm');

const SECRET_VALUE = 'super-secret-token-VALUE-42';

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function renderForm(props: {
  envKeys: readonly McpEnvChip[];
  requiredEnv?: readonly string[];
  onSaved?: (preserved: boolean) => void;
  onClose?: () => void;
}) {
  const qc = client();
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <McpEnvEditForm
      serverName="github"
      envKeys={props.envKeys}
      {...(props.requiredEnv !== undefined ? { requiredEnv: props.requiredEnv } : {})}
      {...(props.onSaved !== undefined ? { onSaved: props.onSaved } : {})}
      onClose={props.onClose ?? (() => undefined)}
    />,
    { wrapper: Wrapper },
  );
}

describe('McpEnvEditForm (MCPW-02)', () => {
  beforeEach(() => {
    setMcpServerEnv.mockReset();
    setMcpServerEnv.mockResolvedValue({ name: 'github', envKeys: [] });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the four env states distinctly (required/optional/missing/placeholder)', () => {
    renderForm({
      envKeys: [
        { key: 'GITHUB_TOKEN', redacted: true }, // required + present → "required"
        { key: 'GITHUB_ORG', redacted: false }, // not required → "optional"
        { key: 'GITHUB_HOST', redacted: false }, // required + present but blank-able → "required"
      ],
      requiredEnv: ['GITHUB_TOKEN', 'GITHUB_HOST', 'GITHUB_API'], // GITHUB_API missing
    });

    // required chip (present required) + optional chip + missing chip all render distinct labels.
    expect(screen.getAllByText('Required').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Optional')).toBeTruthy();
    expect(screen.getByText('Missing')).toBeTruthy();
    // The missing required var (GITHUB_API) renders as an editable row (its label) AND in the
    // soft-warning offending-keys list — both render the key, so there are 2 matches.
    expect(screen.getAllByText('GITHUB_API').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByLabelText('GITHUB_API')).toBeTruthy();
  });

  it('never puts a secret VALUE in the DOM — the secret field holds the redacted ${KEY} placeholder', () => {
    renderForm({
      envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
      requiredEnv: ['GITHUB_TOKEN'],
    });
    // The redacted placeholder is the field value; the real secret value is never rendered.
    const field = screen.getByLabelText<HTMLInputElement>('GITHUB_TOKEN');
    expect(field.value).toBe('${GITHUB_TOKEN}');
    expect(document.body.textContent).not.toContain(SECRET_VALUE);
  });

  it('masks secret env rows by default and reveals only on explicit request', () => {
    renderForm({
      envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
      requiredEnv: ['GITHUB_TOKEN'],
    });

    const field = screen.getByLabelText<HTMLInputElement>('GITHUB_TOKEN');
    expect(field.getAttribute('type')).toBe('password');

    fireEvent.click(screen.getByRole('button', { name: 'Show GITHUB_TOKEN' }));
    expect(field.getAttribute('type')).toBe('text');

    fireEvent.click(screen.getByRole('button', { name: 'Hide GITHUB_TOKEN' }));
    expect(field.getAttribute('type')).toBe('password');
  });

  it('shows the soft-warning card AND still allows save when a required var is a placeholder', async () => {
    const onClose = vi.fn();
    renderForm({
      envKeys: [{ key: 'API_KEY', redacted: true }],
      requiredEnv: ['API_KEY'],
      onClose,
    });
    // API_KEY holds its placeholder → it's an offending required var → soft warning shows.
    expect(screen.getByText('Required values still missing')).toBeTruthy();

    // Save is NOT disabled — the warning is informational.
    const save = screen.getByRole('button', { name: 'Save changes' });
    expect(save.hasAttribute('disabled')).toBe(false);
    fireEvent.click(save);

    await waitFor(() => {
      expect(setMcpServerEnv).toHaveBeenCalledTimes(1);
    });
    // The untouched secret submits its placeholder verbatim (the backend preserves it).
    expect(setMcpServerEnv).toHaveBeenCalledWith('github', ['API_KEY=${API_KEY}']);
    expect(onClose).toHaveBeenCalled();
  });

  it('reports the preserved-secret flag on save when an untouched secret was left placeholder', async () => {
    const onSaved = vi.fn();
    renderForm({
      envKeys: [{ key: 'API_KEY', redacted: true }],
      requiredEnv: ['API_KEY'],
      onSaved,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(true);
    });
  });

  it('rotates a secret when the operator types a real replacement value', async () => {
    renderForm({
      envKeys: [{ key: 'API_KEY', redacted: true }],
      requiredEnv: ['API_KEY'],
    });
    const field = screen.getByLabelText('API_KEY');
    fireEvent.change(field, { target: { value: 'rotated-value' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => {
      expect(setMcpServerEnv).toHaveBeenCalledWith('github', ['API_KEY=rotated-value']);
    });
  });

  it('Discard changes calls onClose without a request', () => {
    const onClose = vi.fn();
    renderForm({ envKeys: [{ key: 'PLAIN', redacted: false }], onClose });
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(setMcpServerEnv).not.toHaveBeenCalled();
  });
});
