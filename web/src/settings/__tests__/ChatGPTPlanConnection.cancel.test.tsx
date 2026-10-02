import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { ChatGPTPlanConnection } from '../ChatGPTPlanConnection';

const firstURL = '/browser/chatgpt-0123456789abcdef01234567';
const secondURL = '/browser/chatgpt-abcdef0123456789abcdef01';

function json(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200 });
}

function fixture() {
  let status = 'disconnected';
  let planEnabled = true;
  const popup = { closed: false, location: { href: '' }, opener: window, close: vi.fn() };
  vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === 'POST') {
      status = 'authorization_required';
      return Promise.resolve(json({ auth_url: firstURL, status }));
    }
    if (init?.method === 'DELETE') {
      expect(input).toBe('/api/settings/chatgpt/login');
      status = 'disconnected';
      return Promise.resolve(json({ cancelled: true }));
    }
    return Promise.resolve(
      json({
        connected: status === 'approved',
        email: '',
        plan_enabled: planEnabled,
        status,
      }),
    );
  });
  vi.stubGlobal('fetch', fetchMock);
  return {
    popup,
    fetchMock,
    approve: (plan = true) => {
      status = 'approved';
      planEnabled = plan;
    },
  };
}

async function login() {
  const button = await screen.findByRole<HTMLButtonElement>('button', {
    name: 'Continue with ChatGPT',
  });
  await waitFor(() => {
    expect(button.disabled).toBe(false);
  });
  fireEvent.click(button);
}

function deletes(fetchMock: ReturnType<typeof fixture>['fetchMock']) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === 'DELETE');
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('ChatGPT VM login cancellation', () => {
  it('cancels the exact flow when the user closes the popup and permits another login', async () => {
    const { popup, fetchMock } = fixture();
    render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    popup.closed = true;
    await screen.findByText('Sign-in cancelled. Continue with ChatGPT to try again.');
    await waitFor(() => {
      expect(deletes(fetchMock)).toHaveLength(1);
    });
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
    expect(screen.queryByRole('link', { name: 'Open ChatGPT sign-in' })).toBeNull();
  });

  it('cancels a popup closed just before the POST response, before the close monitor runs', async () => {
    const { popup, fetchMock } = fixture();
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(
        json({ connected: false, plan_enabled: false, email: '', status: 'disconnected' }),
      ),
    );
    fetchMock.mockImplementationOnce(() => {
      popup.closed = true;
      return Promise.resolve(json({ auth_url: firstURL, status: 'authorization_required' }));
    });
    render(<ChatGPTPlanConnection />);
    await login();
    await screen.findByText('Sign-in cancelled. Continue with ChatGPT to try again.');
    expect(popup.location.href).toBe('');
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
  });

  it('cleans up a known server flow if assigning the popup destination fails', async () => {
    const { popup, fetchMock } = fixture();
    Object.defineProperty(popup.location, 'href', {
      set: () => {
        throw new Error('Popup navigation failed.');
      },
    });
    render(<ChatGPTPlanConnection />);
    await login();
    await screen.findByText('Sign-in cancelled. Continue with ChatGPT to try again.');
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
    expect(screen.getByRole('button', { name: 'Continue with ChatGPT' })).toBeDefined();
  });

  it('offers an explicit cancel action when the popup is blocked', async () => {
    const { fetchMock } = fixture();
    vi.spyOn(window, 'open').mockReturnValue(null);
    render(<ChatGPTPlanConnection />);
    await login();
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel sign-in' }));
    await waitFor(() => {
      expect(deletes(fetchMock)).toHaveLength(1);
    });
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
  });

  it('cleans up the exact flow on unmount using a fresh request', async () => {
    const { popup, fetchMock } = fixture();
    const view = render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    view.unmount();
    await waitFor(() => {
      expect(deletes(fetchMock)).toHaveLength(1);
    });
    expect(deletes(fetchMock)[0]?.[1]?.signal?.aborted).toBe(false);
    expect(popup.close).toHaveBeenCalled();
  });

  it('waits for a late POST result after unmount, then cancels its returned flow', async () => {
    const { fetchMock } = fixture();
    let resolveLogin: (response: Response) => void = () => {
      throw new Error('Missing deferred login');
    };
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(
        json({ connected: false, plan_enabled: false, email: '', status: 'disconnected' }),
      ),
    );
    fetchMock.mockImplementationOnce((_input, init) => {
      expect(init?.signal?.aborted).toBe(false);
      return new Promise<Response>((resolve) => {
        resolveLogin = resolve;
      });
    });
    const view = render(<ChatGPTPlanConnection />);
    await login();
    view.unmount();
    expect(fetchMock.mock.calls[1]?.[1]?.signal?.aborted).toBe(false);
    await act(async () => {
      resolveLogin(json({ auth_url: firstURL, status: 'authorization_required' }));
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(deletes(fetchMock)).toHaveLength(1);
    });
  });

  it('blocks retry until cancelled startup and cleanup finish, even when POSTs reuse the same route', async () => {
    const { popup, fetchMock } = fixture();
    let resolveLogin: (response: Response) => void = () => {
      throw new Error('Missing deferred login');
    };
    let resolveCancel: (response: Response) => void = () => {
      throw new Error('Missing deferred cancellation');
    };
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(
        json({ connected: false, plan_enabled: false, email: '', status: 'disconnected' }),
      ),
    );
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          resolveLogin = resolve;
        }),
    );
    render(<ChatGPTPlanConnection />);
    await login();
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel sign-in' }));
    const retry = screen.getByRole<HTMLButtonElement>('button', { name: 'Continue with ChatGPT' });
    expect(retry.disabled).toBe(true);
    fireEvent.click(retry);
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1);
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          resolveCancel = resolve;
        }),
    );
    await act(async () => {
      resolveLogin(json({ auth_url: firstURL, status: 'authorization_required' }));
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(deletes(fetchMock)).toHaveLength(1);
    });
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
    expect(retry.disabled).toBe(true);
    fireEvent.click(retry);
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1);
    await act(async () => {
      resolveCancel(json({ cancelled: true }));
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(retry.disabled).toBe(false);
    });
    // A deduplicating backend would return this same route for overlapping POSTs.
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(json({ auth_url: firstURL, status: 'authorization_required' })),
    );
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(2);
    expect(deletes(fetchMock)).toHaveLength(1);
    expect(screen.getByRole('link', { name: 'Open ChatGPT sign-in' }).getAttribute('href')).toBe(
      firstURL,
    );
  });

  it('waits for cleanup from an unmounted startup before posting a login from the next view', async () => {
    const { popup, fetchMock } = fixture();
    let resolveLogin: (response: Response) => void = () => {
      throw new Error('Missing deferred login');
    };
    let cancelled = false;
    let posts = 0;
    fetchMock.mockImplementation((_input, init) => {
      if (init?.method === 'POST') {
        posts += 1;
        if (posts === 1)
          return new Promise<Response>((resolve) => {
            resolveLogin = resolve;
          });
        expect(cancelled).toBe(true);
        return Promise.resolve(json({ auth_url: secondURL, status: 'authorization_required' }));
      }
      if (init?.method === 'DELETE') {
        cancelled = true;
        return Promise.resolve(json({ cancelled: true }));
      }
      return Promise.resolve(
        json({ connected: false, plan_enabled: false, email: '', status: 'disconnected' }),
      );
    });
    const firstView = render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(posts).toBe(1);
    });
    firstView.unmount();
    render(<ChatGPTPlanConnection />);
    await login();
    expect(posts).toBe(1);
    await act(async () => {
      resolveLogin(json({ auth_url: firstURL, status: 'authorization_required' }));
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(popup.location.href).toContain(secondURL);
    });
    expect(posts).toBe(2);
    expect(deletes(fetchMock)[0]?.[1]?.body).toBe(JSON.stringify({ auth_url: firstURL }));
  });

  it('closes a completed missing-plan popup without cancelling the approved identity flow', async () => {
    const { popup, fetchMock, approve } = fixture();
    const view = render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    approve(false);
    fireEvent(window, new Event('focus'));
    await screen.findByText(
      'This connection does not grant ChatGPT plan access. Sign in again with an eligible account.',
    );
    expect(popup.close).toHaveBeenCalled();
    view.unmount();
    expect(deletes(fetchMock)).toHaveLength(0);
  });

  it('observes approval before treating a newly closed popup as a cancellation', async () => {
    const { popup, fetchMock, approve } = fixture();
    render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    approve();
    popup.closed = true;
    await screen.findByText('ChatGPT connected');
    expect(deletes(fetchMock)).toHaveLength(0);
  });

  it.each([true, false])(
    'verifies account status after an immediate approved response without a URL (plan: %s)',
    async (planEnabled) => {
      const { fetchMock, popup, approve } = fixture();
      const onConnectionChange = vi.fn();
      const view = render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
      const button = await screen.findByRole<HTMLButtonElement>('button', {
        name: 'Continue with ChatGPT',
      });
      await waitFor(() => {
        expect(button.disabled).toBe(false);
      });
      fetchMock.mockImplementationOnce(() => {
        approve(planEnabled);
        return Promise.resolve(json({ status: 'approved', auth_url: '' }));
      });
      fireEvent.click(button);
      await screen.findByText(
        planEnabled
          ? 'ChatGPT connected'
          : 'This connection does not grant ChatGPT plan access. Sign in again with an eligible account.',
      );
      await waitFor(() => {
        expect(onConnectionChange).toHaveBeenLastCalledWith(planEnabled);
      });
      expect(popup.location.href).toBe('');
      expect(popup.close).toHaveBeenCalled();
      expect(screen.queryByRole('alert')).toBeNull();
      view.unmount();
      expect(deletes(fetchMock)).toHaveLength(0);
    },
  );

  it.each(['cancel', 'unmount'] as const)(
    'preserves a late approved grant after %s while startup was pending',
    async (action) => {
      const { fetchMock, approve } = fixture();
      let resolveLogin: (response: Response) => void = () => {
        throw new Error('Missing deferred login');
      };
      const view = render(<ChatGPTPlanConnection />);
      const button = await screen.findByRole<HTMLButtonElement>('button', {
        name: 'Continue with ChatGPT',
      });
      await waitFor(() => {
        expect(button.disabled).toBe(false);
      });
      fetchMock.mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            resolveLogin = resolve;
          }),
      );
      fireEvent.click(button);
      await waitFor(() => {
        expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(true);
      });
      if (action === 'cancel')
        fireEvent.click(screen.getByRole('button', { name: 'Cancel sign-in' }));
      else view.unmount();
      approve();
      await act(async () => {
        resolveLogin(json({ status: 'approved', auth_url: '' }));
        await Promise.resolve();
      });
      if (action === 'cancel') {
        await screen.findByText('ChatGPT connected');
        expect(
          screen.queryByText('Sign-in cancelled. Continue with ChatGPT to try again.'),
        ).toBeNull();
      }
      expect(deletes(fetchMock)).toHaveLength(0);
    },
  );

  it('keeps the account disconnected if approved POST is not confirmed by the status endpoint', async () => {
    const { fetchMock, popup } = fixture();
    render(<ChatGPTPlanConnection />);
    const button = await screen.findByRole<HTMLButtonElement>('button', {
      name: 'Continue with ChatGPT',
    });
    await waitFor(() => {
      expect(button.disabled).toBe(false);
    });
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(json({ status: 'approved', auth_url: '' })),
    );
    fireEvent.click(button);
    await waitFor(() => {
      expect(popup.close).toHaveBeenCalled();
      expect(button.disabled).toBe(false);
    });
    expect(screen.queryByText('ChatGPT connected')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(deletes(fetchMock)).toHaveLength(0);
  });

  it('recovers an account approved on the server just before the login response failed', async () => {
    const { fetchMock, popup, approve } = fixture();
    render(<ChatGPTPlanConnection />);
    const button = await screen.findByRole<HTMLButtonElement>('button', {
      name: 'Continue with ChatGPT',
    });
    await waitFor(() => {
      expect(button.disabled).toBe(false);
    });
    fetchMock.mockImplementationOnce(() => {
      approve();
      return Promise.resolve(
        new Response(JSON.stringify({ error: 'Login response failed.' }), { status: 502 }),
      );
    });
    fireEvent.click(button);
    await screen.findByText('ChatGPT connected');
    expect(popup.close).toHaveBeenCalled();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(deletes(fetchMock)).toHaveLength(0);
  });

  it('reports cleanup failures and enables a new sign-in after the request finishes', async () => {
    const { fetchMock, popup } = fixture();
    render(<ChatGPTPlanConnection />);
    await login();
    await waitFor(() => {
      expect(popup.location.href).toContain(firstURL);
    });
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: 'Browser cleanup failed.' }), { status: 502 }),
      ),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel sign-in' }));
    expect(await screen.findByRole('alert')).toHaveProperty(
      'textContent',
      'Browser cleanup failed.',
    );
    await waitFor(() => {
      expect(
        screen.getByRole<HTMLButtonElement>('button', { name: 'Continue with ChatGPT' }).disabled,
      ).toBe(false);
    });
  });

  it('cancels at the ten-minute deadline while preserving a bounded polling loop', async () => {
    vi.useFakeTimers();
    const { fetchMock } = fixture();
    render(<ChatGPTPlanConnection />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue with ChatGPT' }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_001);
    });
    expect(screen.getByRole('alert').textContent).toContain('Sign-in timed out');
    expect(deletes(fetchMock)).toHaveLength(1);
    const count = fetchMock.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(count);
  });

  it.each([
    'https://auth.openai.com/oauth/authorize',
    'https://evil.example/browser/chatgpt-0123456789abcdef01234567',
    '//evil.example/browser/chatgpt-0123456789abcdef01234567',
    `${firstURL}?redirect=evil`,
    `${firstURL}#fragment`,
    '/browser/chatgpt-0123456789ABCDEF01234567',
    '/browser/chatgpt-0123456789abcdef01234567/../other',
    '/browser/%63hatgpt-0123456789abcdef01234567',
  ])('rejects a route outside the exact VM login boundary: %s', async (authURL) => {
    const { fetchMock, popup } = fixture();
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(
        json({ connected: false, plan_enabled: false, email: '', status: 'disconnected' }),
      ),
    );
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(json({ auth_url: authURL, status: 'authorization_required' })),
    );
    render(<ChatGPTPlanConnection />);
    await login();
    await screen.findByRole('alert');
    expect(popup.location.href).toBe('');
    expect(deletes(fetchMock)).toHaveLength(0);
    expect(screen.queryByRole('link', { name: 'Open ChatGPT sign-in' })).toBeNull();
  });
});
