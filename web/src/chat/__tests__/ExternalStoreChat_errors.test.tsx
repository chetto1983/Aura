import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ExternalStoreChat } from '../ExternalStoreChat';
import {
  isHistoryURL,
  jsonResponse,
  messagesSnapshotResponse,
  renderChat,
  sendPrompt,
  sseResponse,
} from './chatTestHarness';

// The chat lane's ERROR slot (MessagePrimitive.Error). Split out of ExternalStoreChat.test.tsx
// when CRED-05's refusal took that file past the 600-LOC cap. Both cases share one render
// target and must read DIFFERENTLY: a turn that failed upstream and a turn refused for want of
// credit are different facts, and the second one names who to ask.

describe('ExternalStoreChat — the error slot', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('surfaces an incomplete turn when the stream errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          isHistoryURL(url)
            ? messagesSnapshotResponse([])
            : sseResponse([
                { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
                { type: 'RUN_ERROR', message: 'upstream 5xx' },
              ]),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('boom');
    // The reducer routes RUN_ERROR into an error text part rendered as markdown.
    await waitFor(() => {
      expect(screen.getByText('upstream 5xx')).toBeTruthy();
    });
    // The error slot itself carries the GENERIC copy: this turn failed upstream, it did not
    // run out of credit, and the two must not read the same.
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toBe('Something went wrong with this turn. Try again.');
    expect(alert.className).toContain('text-danger');
  });

  // CRED-05: a zero-credit identity is refused BEFORE the model is called, and the sentinel
  // cmd/aura/llm_client.go marshals rides the RUN_ERROR message. The slot names the identity and
  // says who to ask — and carries NO figure, because the only numbers available at refusal time
  // are a drifting duplicate of the ledger and the provider's 30-40s-lagged counter (M-07).
  it('renders the zero-credit refusal in the error slot, naming the identity and no amount', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        if (typeof url === 'string' && url.startsWith('/api/me')) {
          return Promise.resolve(
            jsonResponse({
              identity_id: 'id-alice',
              name: 'alice@example.com',
              capabilities: ['agent.run'],
            }),
          );
        }
        return Promise.resolve(
          isHistoryURL(url)
            ? messagesSnapshotResponse([])
            : sseResponse([
                { type: 'RUN_STARTED', threadId: 'conv-1', runId: 'run-1' },
                {
                  type: 'RUN_ERROR',
                  message:
                    '{"error":"credit_exhausted","hint":"Ask an administrator to add credit under Settings → Identities"}',
                },
              ]),
        );
      }),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('spend');

    const alert = await screen.findByText(/has no remaining credit for this turn/);
    expect(alert.textContent).toBe(
      'alice@example.com has no remaining credit for this turn. Ask an administrator to add credit under Settings → Identities.',
    );
    expect(alert.getAttribute('role')).toBe('alert');
    expect(alert.className).toContain('text-danger');
    // No balance figure anywhere in the refusal.
    expect(alert.textContent).not.toMatch(/\$|\d/);
  });
});
