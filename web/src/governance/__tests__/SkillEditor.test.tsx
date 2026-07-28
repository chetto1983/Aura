import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SkillDraft } from '../governanceApi';

// SkillEditor test. The contract worth pinning is that the editor does not decide anything
// about validity on its own: it asks the server to dry-run the real write boundary and
// renders that verdict. A local copy of the rules would pass this test while diverging from
// the save — which is why the assertions are about WHAT WAS SENT and WHAT WAS BLOCKED, not
// about specific messages the browser invented.

const validateSkill = vi.fn();
const createSkill = vi.fn();
const updateSkill = vi.fn();

vi.mock('../governanceApi', () => ({
  validateSkill: (...a: unknown[]) => validateSkill(...a) as Promise<unknown>,
  createSkill: (...a: unknown[]) => createSkill(...a) as Promise<unknown>,
  updateSkill: (...a: unknown[]) => updateSkill(...a) as Promise<unknown>,
}));

// Tiptap needs a real ProseMirror/DOM stack that jsdom only half-supports, and this test is
// about the validation contract, not about rich text. The body surface is stubbed with a
// plain textarea that speaks the same props.
vi.mock('../SkillBodyEditor', () => ({
  SkillBodyEditor: ({
    value,
    onChange,
    label,
  }: {
    value: string;
    onChange: (md: string) => void;
    label: string;
  }) => (
    <textarea
      aria-label={label}
      defaultValue={value}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    />
  ),
}));

const { SkillEditor } = await import('../SkillEditor');

const EXISTING: SkillDraft = {
  name: 'preventivo-pdf',
  description: 'Builds a quote PDF.',
  body: '## When to use\nAlways.',
  always: false,
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client()}>{children}</QueryClientProvider>;
}

function renderEditor(props: Partial<Parameters<typeof SkillEditor>[0]> = {}) {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  render(<SkillEditor onClose={onClose} onSaved={onSaved} {...props} />, { wrapper: Wrapper });
  return { onClose, onSaved };
}

describe('SkillEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    validateSkill.mockReset();
    createSkill.mockReset();
    updateSkill.mockReset();
    validateSkill.mockResolvedValue({ ok: true });
    createSkill.mockResolvedValue({ status: 'active' });
    updateSkill.mockResolvedValue({ status: 'active' });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('does not validate an untouched blank draft', async () => {
    renderEditor();
    await vi.advanceTimersByTimeAsync(1000);
    expect(validateSkill).not.toHaveBeenCalled();
  });

  it('sends the whole draft to the server validator, debounced', async () => {
    renderEditor();

    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'quote-pdf' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Description' }), {
      target: { value: 'Builds a quote.' },
    });
    // Nothing fires mid-typing.
    expect(validateSkill).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(500);
    await waitFor(() => {
      expect(validateSkill).toHaveBeenCalled();
    });
    expect(validateSkill).toHaveBeenLastCalledWith({
      name: 'quote-pdf',
      description: 'Builds a quote.',
      body: '',
      always: false,
    });
  });

  it('renders the server verdict and blocks the save while it stands', async () => {
    validateSkill.mockResolvedValue({
      ok: false,
      field: 'name',
      message: 'invalid skill name: "Bad Name" must match ^[a-z0-9-]{1,64}$',
    });
    renderEditor();

    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'Bad Name' },
    });
    await vi.advanceTimersByTimeAsync(500);

    // The message is the SERVER's, rendered verbatim next to the field it names.
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('must match');
    });
    const save = screen.getByRole('button', { name: 'Save and activate' });
    expect(save.hasAttribute('disabled')).toBe(true);

    fireEvent.click(save);
    expect(createSkill).not.toHaveBeenCalled();
  });

  it('shows an unattributable rejection at form level, not on a guessed field', async () => {
    validateSkill.mockResolvedValue({
      ok: false,
      message: 'invalid skill structure: description is required',
    });
    renderEditor();

    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'good-name' },
    });
    await vi.advanceTimersByTimeAsync(500);

    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('description is required');
    });
  });

  it('creates a new skill and reports it live', async () => {
    const { onSaved } = renderEditor();

    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'quote-pdf' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Skill body' }), {
      target: { value: '## Do it\nRun pandoc.' },
    });
    await vi.advanceTimersByTimeAsync(500);
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Save and activate' }).hasAttribute('disabled'),
      ).toBe(false);
    });

    fireEvent.click(screen.getByRole('button', { name: 'Save and activate' }));
    await waitFor(() => {
      expect(createSkill).toHaveBeenCalledWith({
        name: 'quote-pdf',
        description: '',
        body: '## Do it\nRun pandoc.',
        always: false,
      });
    });
    expect(updateSkill).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith('quote-pdf');
    });
  });

  it('updates an existing skill and never lets its name be retyped', async () => {
    renderEditor({ initial: EXISTING });

    // The name IS the directory on disk; renaming here would create a second skill and
    // orphan the first, so the field states the fact rather than pretending to be editable.
    const name = screen.getByRole('textbox', { name: 'Name' });
    expect(name.getAttribute('readonly')).not.toBeNull();
    expect(
      screen.getByText('The name is the folder on disk and cannot be changed here.'),
    ).toBeTruthy();

    fireEvent.change(screen.getByRole('textbox', { name: 'Description' }), {
      target: { value: 'Builds a quote PDF, with the house template.' },
    });
    await vi.advanceTimersByTimeAsync(500);

    fireEvent.click(screen.getByRole('button', { name: 'Save and activate' }));
    await waitFor(() => {
      expect(updateSkill).toHaveBeenCalledWith({
        name: 'preventivo-pdf',
        description: 'Builds a quote PDF, with the house template.',
        body: '## When to use\nAlways.',
        always: false,
      });
    });
    expect(createSkill).not.toHaveBeenCalled();
  });

  it('surfaces a failed save instead of reporting success', async () => {
    createSkill.mockRejectedValue(new Error('HTTP 502'));
    const { onSaved } = renderEditor();

    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'quote-pdf' },
    });
    await vi.advanceTimersByTimeAsync(500);
    fireEvent.click(screen.getByRole('button', { name: 'Save and activate' }));

    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toBe("Couldn't save the skill. Try again.");
    });
    expect(onSaved).not.toHaveBeenCalled();
  });

  it('says that saving activates — there is no pending state to explain', () => {
    renderEditor();
    expect(screen.getByText(/no pending state, no approval/)).toBeTruthy();
    expect(screen.queryByText(/approval/i)?.textContent).toContain('no approval');
  });

  it('closes without saving', () => {
    const { onClose } = renderEditor();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(createSkill).not.toHaveBeenCalled();
    expect(updateSkill).not.toHaveBeenCalled();
  });
});
