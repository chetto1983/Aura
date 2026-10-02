import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { catalogError } from './useModelCatalog';
import {
  cancelChatGPTPlanLogin,
  disconnectChatGPTPlan,
  fetchChatGPTPlanStatus,
  isChatGPTPlanLoginRoute,
  startChatGPTPlanLogin,
  type ChatGPTPlanStatus,
} from './chatgptPlanApi';

const LOGIN_TIMEOUT_MS = 600_000;
const POLL_INTERVAL_MS = 1500;

// Survives a Settings remount in this page. A cancelled startup must finish cleanup
// before another POST: the server may deduplicate concurrent starts to one route.
let startupBarrier: Promise<void> = Promise.resolve();

function reserveStartup() {
  const before = startupBarrier;
  let release: () => void = () => undefined;
  startupBarrier = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { before, release };
}

function trackCleanup(cleanup: Promise<void>) {
  startupBarrier = Promise.all([startupBarrier, cleanup]).then(
    () => undefined,
    () => undefined,
  );
}

interface LoginAttempt {
  readonly id: number;
  popup: Window | null;
  authURL?: string;
  cancelRequested: boolean;
  cancelStarted: boolean;
  completed: boolean;
}

function stopped(flow: LoginAttempt, signal: AbortSignal): boolean {
  return flow.cancelRequested || signal.aborted;
}

function closePopup(flow: LoginAttempt) {
  flow.popup?.close();
  flow.popup = null;
}

/** Account status belongs to this view; cleanup requests belong to one exact server flow. */
export function useChatGPTPlanConnection(onConnectionChange?: (ready: boolean) => void) {
  const { t } = useTranslation();
  const [connection, setConnection] = useState<ChatGPTPlanStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [activeLogin, setActiveLogin] = useState<LoginAttempt>();
  const [authURL, setAuthURL] = useState<string>();
  const [ended, setEnded] = useState<'cancelled' | 'timeout'>();
  const lifetime = useRef<AbortController | null>(null);
  const currentLogin = useRef<LoginAttempt | null>(null);
  const attemptCounter = useRef(0);
  const sequence = useRef(0);
  const actionInFlight = useRef(false);
  const pending =
    ended === undefined &&
    (activeLogin !== undefined ||
      connection?.status === 'starting' ||
      connection?.status === 'authorization_required');
  const ready =
    connection?.connected === true &&
    connection.plan_enabled &&
    connection.status === 'approved' &&
    !pending;

  const completeLogin = useCallback(() => {
    const flow = currentLogin.current;
    if (flow !== null) {
      flow.completed = true;
      currentLogin.current = null;
      closePopup(flow);
    }
    setActiveLogin(undefined);
    setAuthURL(undefined);
  }, []);

  const refresh = useCallback(
    async (signal: AbortSignal) => {
      if (actionInFlight.current) return;
      const ticket = ++sequence.current;
      try {
        const next = await fetchChatGPTPlanStatus(signal);
        if (signal.aborted || ticket !== sequence.current) return;
        setConnection(next);
        setError(next.error);
        // Identity authorization is complete even when its plan lacks inference access.
        if (next.connected && next.status === 'approved') completeLogin();
        return next;
      } catch (err) {
        if (!signal.aborted && ticket === sequence.current) {
          setError(catalogError(err));
          setConnection(undefined);
        }
      }
      return undefined;
    },
    [completeLogin],
  );

  const cancelAttempt = useCallback(
    async (flow: LoginAttempt, reason?: 'cancelled' | 'timeout') => {
      if (flow.completed) return;
      flow.cancelRequested = true;
      closePopup(flow);
      if (currentLogin.current === flow) {
        sequence.current += 1;
        actionInFlight.current = true;
        if (lifetime.current?.signal.aborted === false) {
          setActiveLogin(undefined);
          setAuthURL(undefined);
          setBusy(true);
          if (reason !== undefined) setEnded(reason);
        }
      }
      // Retain an in-flight POST; its eventual response re-enters here with the selector.
      if (flow.authURL === undefined || flow.cancelStarted) return;
      flow.cancelStarted = true;
      try {
        const cleanup = cancelChatGPTPlanLogin(flow.authURL);
        trackCleanup(cleanup);
        await cleanup;
        const signal = lifetime.current?.signal;
        if (signal?.aborted === false && attemptCounter.current === flow.id) {
          actionInFlight.current = false;
          await refresh(signal);
        }
      } catch (err) {
        if (lifetime.current?.signal.aborted === false && attemptCounter.current === flow.id)
          setError(catalogError(err));
      } finally {
        if (currentLogin.current === flow) currentLogin.current = null;
        if (lifetime.current?.signal.aborted === false && attemptCounter.current === flow.id) {
          actionInFlight.current = false;
          setBusy(false);
        }
      }
    },
    [refresh],
  );

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    const timer = setTimeout(() => {
      void refresh(controller.signal);
    }, 0);
    const onFocus = () => {
      void refresh(controller.signal);
    };
    window.addEventListener('focus', onFocus);
    return () => {
      controller.abort();
      clearTimeout(timer);
      sequence.current += 1;
      window.removeEventListener('focus', onFocus);
      const flow = currentLogin.current;
      if (flow !== null) void cancelAttempt(flow);
    };
  }, [cancelAttempt, refresh]);

  useEffect(() => {
    onConnectionChange?.(ready);
  }, [onConnectionChange, ready]);

  useEffect(() => {
    if (!pending) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      await refresh(controller.signal);
      if (!controller.signal.aborted)
        timer = setTimeout(() => {
          void poll();
        }, POLL_INTERVAL_MS);
    };
    timer = setTimeout(() => {
      void poll();
    }, POLL_INTERVAL_MS);
    // A server-side flow from an earlier view may have no local popup or selector.
    const deadline =
      activeLogin === undefined
        ? setTimeout(() => {
            controller.abort();
            clearTimeout(timer);
            setEnded('timeout');
          }, LOGIN_TIMEOUT_MS)
        : undefined;
    return () => {
      controller.abort();
      clearTimeout(timer);
      clearTimeout(deadline);
    };
  }, [activeLogin, pending, refresh]);

  useEffect(() => {
    if (activeLogin === undefined) return;
    let checking = false;
    const monitor = setInterval(() => {
      if (activeLogin.popup?.closed !== true || checking) return;
      checking = true;
      const observeClose = async () => {
        // Read approval first: the browser can close immediately after consent completes.
        const signal = lifetime.current?.signal;
        if (signal?.aborted === false) await refresh(signal);
        if (currentLogin.current === activeLogin) await cancelAttempt(activeLogin, 'cancelled');
      };
      void observeClose();
    }, 500);
    const deadline = setTimeout(() => {
      void cancelAttempt(activeLogin, 'timeout');
    }, LOGIN_TIMEOUT_MS);
    return () => {
      clearInterval(monitor);
      clearTimeout(deadline);
    };
  }, [activeLogin, cancelAttempt, refresh]);

  const login = async () => {
    const signal = lifetime.current?.signal;
    if (signal === undefined || actionInFlight.current) return;
    const previous = currentLogin.current;
    if (previous !== null) void cancelAttempt(previous);
    const flow: LoginAttempt = {
      id: ++attemptCounter.current,
      // Preserve user activation while the VM prepares its browser asynchronously.
      popup: window.open('about:blank', 'aura-chatgpt-login', 'popup,width=1000,height=800'),
      cancelRequested: false,
      cancelStarted: false,
      completed: false,
    };
    if (flow.popup !== null) flow.popup.opener = null;
    currentLogin.current = flow;
    actionInFlight.current = true;
    sequence.current += 1;
    setActiveLogin(flow);
    setBusy(true);
    setError(undefined);
    setAuthURL(undefined);
    setEnded(undefined);
    const startup = reserveStartup();
    try {
      await startup.before;
      if (stopped(flow, signal)) return;
      const result = await startChatGPTPlanLogin();
      if (result.status === 'approved' && result.auth_url === '') {
        // The VM may authorize a returning account before the POST response arrives.
        flow.completed = true;
        closePopup(flow);
        if (currentLogin.current === flow) {
          currentLogin.current = null;
          if (!signal.aborted) {
            setActiveLogin(undefined);
            setAuthURL(undefined);
            setEnded(undefined);
            actionInFlight.current = false;
            await refresh(signal);
          }
        }
        return;
      }
      if (!isChatGPTPlanLoginRoute(result.auth_url))
        throw new Error(t('settings.chatgpt.invalidURL'));
      flow.authURL = result.auth_url;
      if (stopped(flow, signal) || currentLogin.current !== flow) {
        await cancelAttempt(flow);
        return;
      }
      if (flow.popup?.closed === true) {
        await cancelAttempt(flow, 'cancelled');
        return;
      }
      setAuthURL(result.auth_url);
      setConnection({ connected: false, plan_enabled: false, email: '', status: result.status });
      if (flow.popup !== null)
        flow.popup.location.href = `${window.location.origin}${result.auth_url}`;
    } catch (err) {
      if (!signal.aborted && currentLogin.current === flow) {
        const loginError = catalogError(err);
        setError(loginError);
        if (flow.authURL !== undefined) {
          await cancelAttempt(flow, 'cancelled');
        } else {
          closePopup(flow);
          currentLogin.current = null;
          setActiveLogin(undefined);
          actionInFlight.current = false;
          const recovered = await refresh(signal);
          if (
            !stopped(flow, signal) &&
            recovered !== undefined &&
            !(recovered.connected && recovered.status === 'approved')
          ) {
            setError(recovered.error ?? loginError);
          }
        }
      }
    } finally {
      startup.release();
      if (flow.cancelRequested && currentLogin.current === flow) currentLogin.current = null;
      if (
        currentLogin.current === flow ||
        (currentLogin.current === null && attemptCounter.current === flow.id)
      ) {
        actionInFlight.current = false;
        if (!signal.aborted) setBusy(false);
      }
    }
  };

  const disconnect = async () => {
    const signal = lifetime.current?.signal;
    if (signal === undefined) return;
    const flow = currentLogin.current;
    if (flow !== null) void cancelAttempt(flow);
    const ticket = ++sequence.current;
    actionInFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await disconnectChatGPTPlan(signal);
      if (signal.aborted || ticket !== sequence.current) return;
      setConnection({ connected: false, plan_enabled: false, email: '', status: 'disconnected' });
      setAuthURL(undefined);
    } catch (err) {
      if (!signal.aborted && ticket === sequence.current) {
        actionInFlight.current = false;
        await refresh(signal);
        if (lifetime.current?.signal.aborted === false) setError(catalogError(err));
      }
    } finally {
      actionInFlight.current = false;
      if (!signal.aborted) setBusy(false);
    }
  };

  const retry = () => {
    setEnded(undefined);
    const signal = lifetime.current?.signal;
    if (signal !== undefined) void refresh(signal);
  };
  const cancel = () => {
    const flow = currentLogin.current;
    if (flow !== null) void cancelAttempt(flow, 'cancelled');
  };
  return {
    connection,
    busy,
    error,
    authURL,
    ended,
    pending,
    ready,
    activeLogin: activeLogin !== undefined,
    login,
    disconnect,
    retry,
    cancel,
  };
}
