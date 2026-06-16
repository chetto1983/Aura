import { Component, type ReactNode } from 'react';
import i18n from './i18n/i18n';

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  // D-13: no componentDidCatch telemetry override — there is no backend error
  // sink yet. getDerivedStateFromError swaps in the themed fallback below,
  // replacing the white-screen-of-death.

  render(): ReactNode {
    if (this.state.hasError) {
      return (
        <div
          role="alert"
          className="grid h-dvh place-items-center bg-bg px-6 text-center text-text"
        >
          <div className="max-w-md">
            <p className="text-sm font-medium text-text">{i18n.t('errorBoundary.title')}</p>
            <p className="mt-2 text-xs text-text-muted">{i18n.t('errorBoundary.action')}</p>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
