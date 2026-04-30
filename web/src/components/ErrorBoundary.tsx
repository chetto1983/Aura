import { Component, type ErrorInfo, type ReactNode } from 'react';
import { toast } from 'sonner';

interface Props { children: ReactNode }
interface State { error: Error | null }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught', error, info);
    // 10e: surface the failure as a toast too — the inline error card
    // is easy to miss on tall pages, while a toast pops above all
    // content. Title is short so the toast doesn't dominate.
    toast.error(error.message || 'Something went wrong', {
      description: 'Check the console for the full stack.',
      duration: 6000,
    });
  }

  render() {
    if (this.state.error) {
      return (
        <div className="p-6">
          <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
            <h2 className="text-base font-semibold">Something went wrong</h2>
            <p className="mt-2 text-sm text-muted-foreground">{this.state.error.message}</p>
            <button
              type="button"
              onClick={() => this.setState({ error: null })}
              className="mt-3 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground"
            >
              Try again
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
