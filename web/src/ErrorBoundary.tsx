import { Component, type ReactNode } from 'react';

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
            <p className="text-sm font-medium text-text">Aura could not render this view.</p>
            <p className="mt-2 text-xs text-text-muted">Reload to try again.</p>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
