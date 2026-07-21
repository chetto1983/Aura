import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n';
import { useConversation } from '../../conversations/useConversations';
import { createShare, revokeShare, updateShareSnapshot } from './shareApi';
import type { ShareLink } from './shareTypes';
import { ShareModal } from './ShareModal';

// ShareModal.test.tsx — covers every <behavior> row from 37F-15-PLAN.md Task 2, including the
// single most important assertion in this plan (public is never preselected, D-01) and every
// grep-gated acceptance criterion's functional counterpart. shareApi's three owner-mutation
// functions are mocked (no listShares call — this modal is a self-contained mint-and-manage
// session per its own decision log, never fetching a pre-existing share on open);
// useConversation is mocked to control the stale-detection TurnCount signal.
//
// This project does not register @testing-library/jest-dom matchers (see src/test/setup.ts) —
// every assertion below uses plain DOM properties/attributes (.checked, .getAttribute,
// .disabled, .textContent), matching web/src/shell/ShareShell.test.tsx's own convention.

vi.mock('./shareApi', () => ({
  createShare: vi.fn(),
  updateShareSnapshot: vi.fn(),
  revokeShare: vi.fn(),
}));
vi.mock('../../conversations/useConversations', () => ({
  useConversation: vi.fn(),
}));

const mockCreate = vi.mocked(createShare);
const mockUpdate = vi.mocked(updateShareSnapshot);
const mockRevoke = vi.mocked(revokeShare);
const mockUseConversation = vi.mocked(useConversation);

const internalLink: ShareLink = {
  id: 'share-1',
  tier: 'internal',
  url: '/shared/share-1',
  created_at: '2026-07-17T10:00:00Z',
  updated_at: '2026-07-17T10:00:00Z',
  snapshot_turn_count: 4,
};

const publicLink: ShareLink = {
  id: 'share-2',
  tier: 'public',
  url: '/s/tok-abc123',
  expires_at: '2026-07-24T10:00:00Z',
  created_at: '2026-07-17T10:00:00Z',
  updated_at: '2026-07-17T10:00:00Z',
  snapshot_turn_count: 4,
};

function renderModal(threadId = 'conv-1') {
  const onClose = vi.fn();
  const utils = render(<ShareModal threadId={threadId} onClose={onClose} />);
  return { ...utils, onClose };
}

// Narrowing helpers, not inline `as` casts: the parameter here is explicitly typed as
// HTMLElement (not inferred from screen.getByRole(...)), so the cast is unambiguous
// regardless of how narrowly (or not) testing-library's own overloads type getByRole's
// return under this project's tsconfig.
function asInput(el: HTMLElement): HTMLInputElement {
  return el as HTMLInputElement;
}
function asButton(el: HTMLElement): HTMLButtonElement {
  return el as HTMLButtonElement;
}

async function mintInternal(): Promise<void> {
  mockCreate.mockResolvedValueOnce(internalLink);
  fireEvent.click(screen.getByRole('button', { name: 'Create link' }));
  await waitFor(() => {
    expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
  });
}

let mockWriteText: ReturnType<typeof vi.fn>;

describe('ShareModal', () => {
  beforeEach(() => {
    mockUseConversation.mockReturnValue({ data: undefined } as ReturnType<typeof useConversation>);
    // Captured in a local so assertions never read navigator.clipboard.writeText as a bound
    // method expression (@typescript-eslint/unbound-method) — the mock function IS the thing
    // under test, not the DOM Clipboard method it stands in for.
    mockWriteText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: mockWriteText },
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe('idle state — tier selection before minting (D-01)', () => {
    it('preselects the internal tier and NEVER the public tier — the single most important assertion in this plan', () => {
      renderModal();
      const internalRadio = asInput(screen.getByRole('radio', { name: 'Internal link' }));
      const publicRadio = asInput(screen.getByRole('radio', { name: 'Public link' }));
      expect(internalRadio.checked).toBe(true);
      expect(publicRadio.checked).toBe(false);
    });

    it('renders a real fieldset + legend + radio group for the tier selector', () => {
      // The Dialog is portal-rendered to document.body (Radix), outside RTL's `container` —
      // query the document, not the render container, matching how `screen` itself resolves.
      renderModal();
      const fieldset = document.querySelector('fieldset');
      expect(fieldset).toBeTruthy();
      expect(within(fieldset as HTMLElement).getByText('Who can open this link')).toBeTruthy();
      expect(document.querySelectorAll('input[type="radio"]').length).toBe(2);
    });

    it('does not render the public warning on open', () => {
      renderModal();
      expect(screen.queryByRole('note')).toBeNull();
    });

    it('reveals the public warning only after selecting the public tier, aria-describedby-linked to the radio', () => {
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));

      const warning = screen.getByRole('note');
      expect(warning.textContent).toContain('A public link is visible to anyone');
      const publicRadio = screen.getByRole('radio', { name: 'Public link' });
      expect(publicRadio.getAttribute('aria-describedby')).toContain(warning.id);
    });

    it('reveals the expiry chips only for the public tier, with 7 days preselected', () => {
      renderModal();
      expect(screen.queryByRole('button', { name: '7 days' })).toBeNull();

      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));

      const sevenDays = screen.getByRole('button', { name: '7 days' });
      expect(sevenDays.getAttribute('aria-pressed')).toBe('true');
      expect(screen.getByRole('button', { name: '1 day' }).getAttribute('aria-pressed')).toBe(
        'false',
      );
    });

    it('hides the warning and expiry chips again when switching back to internal', () => {
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      expect(screen.getByRole('note')).toBeTruthy();

      fireEvent.click(screen.getByRole('radio', { name: 'Internal link' }));

      expect(screen.queryByRole('note')).toBeNull();
      expect(screen.queryByRole('button', { name: '7 days' })).toBeNull();
    });

    it('states the snapshot-frozen note before minting', () => {
      renderModal();
      expect(screen.getByText(/snapshot is frozen now/i)).toBeTruthy();
    });

    it('closes via Cancel without minting anything', () => {
      const { onClose } = renderModal();
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
      expect(onClose).toHaveBeenCalledTimes(1);
      expect(mockCreate).not.toHaveBeenCalled();
    });
  });

  describe('custom expiry validation', () => {
    it('omits aria-invalid for a valid custom day count', () => {
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Custom' }));

      const customInput = screen.getByRole('spinbutton', { name: 'Custom expiry (days)' });
      fireEvent.change(customInput, { target: { value: '30' } });

      expect(customInput.hasAttribute('aria-invalid')).toBe(false);
    });

    it('sets aria-invalid for a non-positive custom day count', () => {
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Custom' }));

      const customInput = screen.getByRole('spinbutton', { name: 'Custom expiry (days)' });
      fireEvent.change(customInput, { target: { value: '0' } });

      expect(customInput.getAttribute('aria-invalid')).toBe('true');
    });

    it('sets aria-invalid for a custom day count above the assumed cap', () => {
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Custom' }));

      const customInput = screen.getByRole('spinbutton', { name: 'Custom expiry (days)' });
      fireEvent.change(customInput, { target: { value: '9999' } });

      expect(customInput.getAttribute('aria-invalid')).toBe('true');
    });
  });

  describe('minting', () => {
    it('disables Create and shows a status while creating', async () => {
      let resolveCreate: (link: ShareLink) => void = () => undefined;
      mockCreate.mockReturnValueOnce(
        new Promise<ShareLink>((resolve) => {
          resolveCreate = resolve;
        }),
      );

      renderModal();
      fireEvent.click(screen.getByRole('button', { name: 'Create link' }));

      expect(asButton(screen.getByRole('button', { name: 'Create link' })).disabled).toBe(true);
      expect(screen.getByRole('status')).toBeTruthy();

      resolveCreate(internalLink);
      await waitFor(() => {
        expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
      });
    });

    it('mints internal and renders the /shared/{id} URL, absolute and same-origin', async () => {
      renderModal();
      await mintInternal();

      const input = asInput(screen.getByRole('textbox', { name: 'Link' }));
      expect(input.value).toBe(`${window.location.origin}/shared/share-1`);
      expect(mockCreate).toHaveBeenCalledWith('conv-1', 'internal', undefined);
    });

    it('mints public and renders the /s/{token} URL, absolute and same-origin — never a /shared/{id} form', async () => {
      mockCreate.mockResolvedValueOnce(publicLink);
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Create link' }));

      await waitFor(() => {
        expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
      });
      const input = asInput(screen.getByRole('textbox', { name: 'Link' }));
      expect(input.value).toBe(`${window.location.origin}/s/tok-abc123`);
      expect(input.value).not.toContain('/shared/');
      expect(mockCreate).toHaveBeenCalledWith('conv-1', 'public', '7d');
    });

    it('renders the create error in role="alert" and returns to idle', async () => {
      mockCreate.mockRejectedValueOnce(new Error('public sharing is disabled'));
      renderModal();
      fireEvent.click(screen.getByRole('button', { name: 'Create link' }));

      await waitFor(() => {
        expect(screen.getByRole('alert').textContent).toContain('public sharing is disabled');
      });
      expect(asButton(screen.getByRole('button', { name: 'Create link' })).disabled).toBe(false);
    });
  });

  describe('shared state', () => {
    it('shows a metadata line with tier, relative expiry, and created date', async () => {
      mockCreate.mockResolvedValueOnce(publicLink);
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Create link' }));

      await waitFor(() => {
        expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
      });
      expect(screen.getByText(/Public/)).toBeTruthy();
    });

    it('copies the URL on a direct click with no preceding await, swaps the label, and announces via aria-live', async () => {
      renderModal();
      await mintInternal();

      const copyButton = screen.getByRole('button', { name: 'Copy' });
      expect(copyButton.getAttribute('aria-live')).toBe('polite');
      fireEvent.click(copyButton);

      expect(mockWriteText).toHaveBeenCalledWith(`${window.location.origin}/shared/share-1`);
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Copied ✓' })).toBeTruthy();
      });
    });

    it('shows the stale note with the count when the thread has newer turns than the snapshot', async () => {
      mockUseConversation.mockReturnValue({ data: { TurnCount: 7 } } as ReturnType<
        typeof useConversation
      >);
      renderModal();
      await mintInternal();

      expect(screen.getByText('3 new messages are not in this link')).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Update' })).toBeTruthy();
    });

    it('shows no stale note when there are no newer turns', async () => {
      mockUseConversation.mockReturnValue({ data: { TurnCount: 4 } } as ReturnType<
        typeof useConversation
      >);
      renderModal();
      await mintInternal();

      expect(screen.queryByText(/new messages are not in this link/)).toBeNull();
    });
  });

  describe('update', () => {
    it('re-snapshots and returns to shared with the SAME URL', async () => {
      mockUseConversation.mockReturnValue({ data: { TurnCount: 7 } } as ReturnType<
        typeof useConversation
      >);
      renderModal();
      await mintInternal();

      const refreshed = {
        ...internalLink,
        snapshot_turn_count: 7,
        updated_at: '2026-07-18T00:00:00Z',
      };
      mockUpdate.mockResolvedValueOnce(refreshed);
      fireEvent.click(screen.getByRole('button', { name: 'Update' }));

      expect(screen.getByRole('status')).toBeTruthy();
      await waitFor(() => {
        expect(screen.queryByText(/new messages are not in this link/)).toBeNull();
      });
      const input = asInput(screen.getByRole('textbox', { name: 'Link' }));
      expect(input.value).toBe(`${window.location.origin}/shared/share-1`);
      expect(mockUpdate).toHaveBeenCalledWith('share-1');
    });

    it('renders an update error in role="alert" without losing the current link', async () => {
      mockUseConversation.mockReturnValue({ data: { TurnCount: 7 } } as ReturnType<
        typeof useConversation
      >);
      renderModal();
      await mintInternal();

      mockUpdate.mockRejectedValueOnce(new Error('update failed'));
      fireEvent.click(screen.getByRole('button', { name: 'Update' }));

      await waitFor(() => {
        expect(screen.getByRole('alert').textContent).toContain('update failed');
      });
      // The link is still shown — a failed update returns to 'shared', never drops the link.
      const input = asInput(screen.getByRole('textbox', { name: 'Link' }));
      expect(input.value).toBe(`${window.location.origin}/shared/share-1`);
    });
  });

  describe('revoke', () => {
    it('does not call revokeShare until the confirm dialog is accepted', async () => {
      renderModal();
      await mintInternal();

      fireEvent.click(screen.getByRole('button', { name: 'Revoke link' }));
      expect(mockRevoke).not.toHaveBeenCalled();
      expect(screen.getByText('Revoke this link?')).toBeTruthy();
    });

    it('cancelling the confirm dialog returns to shared without revoking', async () => {
      renderModal();
      await mintInternal();

      fireEvent.click(screen.getByRole('button', { name: 'Revoke link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

      expect(mockRevoke).not.toHaveBeenCalled();
      expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
    });

    it('confirming revokes, then the modal returns to the idle create form with an announcement', async () => {
      mockRevoke.mockResolvedValueOnce(undefined);
      renderModal();
      await mintInternal();

      fireEvent.click(screen.getByRole('button', { name: 'Revoke link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));

      await waitFor(() => {
        expect(mockRevoke).toHaveBeenCalledWith('share-1');
      });
      await waitFor(() => {
        expect(screen.getByRole('radio', { name: 'Internal link' })).toBeTruthy();
      });
      expect(asInput(screen.getByRole('radio', { name: 'Internal link' })).checked).toBe(true);
    });

    it('a failed revoke renders role="alert" and returns to shared with the link intact', async () => {
      mockRevoke.mockRejectedValueOnce(new Error('revoke failed'));
      renderModal();
      await mintInternal();

      fireEvent.click(screen.getByRole('button', { name: 'Revoke link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));

      await waitFor(() => {
        expect(screen.getByRole('alert').textContent).toContain('revoke failed');
      });
      const input = asInput(screen.getByRole('textbox', { name: 'Link' }));
      expect(input.value).toBe(`${window.location.origin}/shared/share-1`);
    });
  });

  describe('expired-but-not-swept', () => {
    it('renders as expired and disables Copy', async () => {
      const expired: ShareLink = {
        ...publicLink,
        expires_at: '2020-01-01T00:00:00Z',
      };
      mockCreate.mockResolvedValueOnce(expired);
      renderModal();
      fireEvent.click(screen.getByRole('radio', { name: 'Public link' }));
      fireEvent.click(screen.getByRole('button', { name: 'Create link' }));

      await waitFor(() => {
        expect(screen.getByRole('textbox', { name: 'Link' })).toBeTruthy();
      });
      expect(screen.getByText(/Expired/)).toBeTruthy();
      expect(asButton(screen.getByRole('button', { name: 'Copy' })).disabled).toBe(true);
    });
  });
});

// The "no async onClick", "no ClipboardItem", and "no div-onClick tier selection" acceptance
// criteria are grep-gated against the source directly (see the plan's <acceptance_criteria>)
// and are verified as part of this task's shell-command verification step, not re-asserted
// here as a placeholder test.
