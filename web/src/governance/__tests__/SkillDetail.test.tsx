import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SkillRow } from '../governanceApi';

// SkillDetail test — renders the detail directly so every field label/value, the dash
// fallback, the delete affordance, and each body branch is asserted. The body is the
// reason this pane exists (amendment #97 / the approved redesign), so its four states —
// loading, rendered markdown, archived, error — are the contract, not decoration.

const fetchSkillBody = vi.fn();

vi.mock('../governanceApi', () => ({
  fetchSkillBody: (...a: unknown[]) => fetchSkillBody(...a) as Promise<string>,
}));

const { SkillDetail } = await import('../SkillDetail');

const FULL: SkillRow = {
  name: 'rich-skill',
  description: 'fully described',
  type: 'executable',
  language: 'go',
  contentHash: 'deadbeef',
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client()}>{children}</QueryClientProvider>;
}

function renderDetail(props: Partial<Parameters<typeof SkillDetail>[0]> = {}) {
  return render(
    <SkillDetail
      skill={FULL}
      stage="active"
      onClose={() => undefined}
      onDelete={() => undefined}
      onEdit={() => undefined}
      {...props}
    />,
    { wrapper: Wrapper },
  );
}

describe('SkillDetail', () => {
  beforeEach(() => {
    fetchSkillBody.mockReset();
    fetchSkillBody.mockResolvedValue('');
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders every field label and value for a fully-populated skill', async () => {
    renderDetail();

    expect(screen.getByText('rich-skill')).toBeTruthy();
    expect(screen.getByText('Type')).toBeTruthy();
    expect(screen.getByText('executable')).toBeTruthy();
    expect(screen.getByText('State')).toBeTruthy();
    expect(screen.getByText('Language')).toBeTruthy();
    expect(screen.getByText('go')).toBeTruthy();
    expect(screen.getByText('Content hash')).toBeTruthy();
    expect(screen.getByText('deadbeef')).toBeTruthy();
    expect(screen.getByText('fully described')).toBeTruthy();
    await waitFor(() => {
      expect(screen.queryByText('—')).toBeNull();
    });
  });

  it('renders the skill body as sanitized markdown — the reason the pane exists', async () => {
    fetchSkillBody.mockResolvedValue('## Procedure\n\nRun `pandoc` and hand back the file.');
    const { container } = renderDetail();

    await waitFor(() => {
      expect(screen.getByText('Procedure')).toBeTruthy();
    });
    expect(container.querySelector('h2')?.textContent).toBe('Procedure');
    expect(container.querySelector('code')?.textContent).toBe('pandoc');
    expect(fetchSkillBody).toHaveBeenCalledWith('rich-skill');
  });

  it('never renders raw HTML smuggled through a skill body', async () => {
    // A skill body can arrive from a third-party repo via npx install, so the markdown
    // goes through the SAME sanitize chokepoint as the chat lane. A script tag must not
    // survive into the DOM.
    fetchSkillBody.mockResolvedValue('Hello <script>window.__pwned = 1;</script> there');
    const { container } = renderDetail();

    await waitFor(() => {
      expect(screen.getByText(/Hello/)).toBeTruthy();
    });
    expect(container.querySelector('script')).toBeNull();
  });

  it('does not fetch a body for an archived skill; it says why instead', async () => {
    renderDetail({ stage: 'archived' });

    await waitFor(() => {
      expect(
        screen.getByText('An archived skill is not loaded, so its body is not readable here.'),
      ).toBeTruthy();
    });
    expect(fetchSkillBody).not.toHaveBeenCalled();
    // The stage is stated in the metadata strip, not only implied by the note.
    expect(screen.getByText('Archived')).toBeTruthy();
  });

  it('surfaces a failed body fetch as a visible error, never as an empty body', async () => {
    fetchSkillBody.mockRejectedValue(new Error('HTTP 404'));
    renderDetail();

    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toBe(
        "Couldn't load the skill body. Try again.",
      );
    });
  });

  it('reports an empty body honestly', async () => {
    fetchSkillBody.mockResolvedValue('   ');
    renderDetail();
    await waitFor(() => {
      expect(screen.getByText('This skill has no body.')).toBeTruthy();
    });
  });

  it('marks an always-on skill and explains what it costs', async () => {
    renderDetail({ skill: { ...FULL, always: true } });
    await waitFor(() => {
      expect(
        screen.getByText('Always on — this skill enters the prompt every turn and costs context.'),
      ).toBeTruthy();
    });
  });

  it('renders dashes for missing type / language / content-hash', async () => {
    renderDetail({ skill: { name: 'sparse', description: '', type: '' } });
    // Type, language and content hash are all missing; State always resolves.
    await waitFor(() => {
      expect(screen.getAllByText('—')).toHaveLength(3);
    });
  });

  it('asks the host to delete — it never deletes on its own', async () => {
    const onDelete = vi.fn();
    renderDetail({ onDelete });

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(onDelete).toHaveBeenCalledWith('rich-skill');
    });
  });

  it('fires onClose when the close button is clicked', () => {
    const onClose = vi.fn();
    renderDetail({ onClose });
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('uses a stable vertical scroll pane for mobile sheet content', () => {
    const { container } = renderDetail();

    const pane = container.firstElementChild;
    expect(pane?.className).toContain('overflow-y-scroll');
    expect(pane?.className).toContain('overscroll-contain');
    expect(pane?.className).toContain('[scrollbar-gutter:stable]');
  });
});
