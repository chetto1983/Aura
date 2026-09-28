import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { MessageDraftCard } from '../MessageDraftCard';
import type { MessageDraft, ResolveMessageDraft } from '../useThreadMessageDrafts';

const email: MessageDraft = {
  id: 'draft-one',
  conversation_id: 'thread-one',
  channel: 'email',
  status: 'pending',
  arguments: {
    action: 'send_email',
    accountId: 'account-one',
    to: ['first@example.test'],
    cc: ['copy@example.test'],
    subject: 'Original subject',
    body: 'Original body',
    attachments: [{ name: 'fixed.pdf' }],
  },
  expires_at: new Date(Date.now() + 60_000).toISOString(),
};

describe('MessageDraftCard', () => {
  it('sends exactly the edited email fields and shows the new destination', async () => {
    const resolve = vi.fn<(input: ResolveMessageDraft) => Promise<unknown>>().mockResolvedValue({});
    render(<MessageDraftCard draft={email} busy={false} onResolve={resolve} />);
    expect(screen.getByText(/fixed.pdf/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText('To'), { target: { value: 'edited@example.test' } });
    fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Edited body' } });
    expect(screen.getByText('Send to edited@example.test')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => {
      expect(resolve).toHaveBeenCalledWith({
        id: email.id,
        action: 'send',
        overrides: {
          to: ['edited@example.test'],
          cc: ['copy@example.test'],
          subject: 'Original subject',
          body: 'Edited body',
        },
      });
    });
    expect(screen.queryByText(/undo/i)).toBeNull();
  });

  it('declines a WhatsApp message without submitting edited content', async () => {
    const resolve = vi.fn<(input: ResolveMessageDraft) => Promise<unknown>>().mockResolvedValue({});
    render(
      <MessageDraftCard
        draft={{
          ...email,
          channel: 'whatsapp',
          arguments: { recipient: '12345', message: 'Original message', quoted_content: 'Prior' },
        }}
        busy={false}
        onResolve={resolve}
      />,
    );
    fireEvent.change(screen.getByLabelText('Recipient'), { target: { value: '67890' } });
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(resolve).toHaveBeenCalledWith({ id: email.id, action: 'decline' });
    });
  });

  it('does not offer another send when delivery is uncertain', () => {
    render(
      <MessageDraftCard
        draft={{ ...email, status: 'uncertain' }}
        busy={false}
        onResolve={vi.fn().mockResolvedValue({})}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Send message' })).toBeNull();
    expect(screen.getByText(/Delivery is uncertain/)).toBeTruthy();
  });

  it('closes an expired review without sending', async () => {
    const resolve = vi.fn<(input: ResolveMessageDraft) => Promise<unknown>>().mockResolvedValue({});
    render(
      <MessageDraftCard draft={{ ...email, status: 'expired' }} busy={false} onResolve={resolve} />,
    );
    expect(screen.queryByRole('button', { name: 'Send message' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Continue conversation' }));
    await waitFor(() => {
      expect(resolve).toHaveBeenCalledWith({ id: email.id, action: 'decline' });
    });
  });
});
