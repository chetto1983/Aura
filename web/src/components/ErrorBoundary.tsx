import { Component, type ErrorInfo, type ReactNode } from 'react';
import { toast } from 'sonner';
import { useLocale } from '@/hooks/useLocale';
import type { TFunction } from 'i18next';

interface InnerProps { children: ReactNode; t: TFunction }
interface State { error: Error | null }

// Thin wrapper so the class component can access translations
export function ErrorBoundary({ children }: { children: ReactNode }) {
  const { t } = useLocale();
  return <ErrorBoundaryInner t={t}>{children}</ErrorBoundaryInner>;
}

class ErrorBoundaryInner extends Component<InnerProps, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught', error, info);
    // 10e: surface the failure as a toast too — the inline error card
    // is easy to miss on tall pages, while a toast pops above all
    // content. Title is short so the toast doesn't dominate.
    toast.error(error.message || this.props.t('common.error'), {
      description: this.props.t('common.checkConsole'),
      duration: 6000,
    });
  }

  render() {
    if (this.state.error) {
      return (
        <div className="p-6">
          <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
            <h2 className="text-base font-semibold">{this.props.t('error.somethingWentWrong')}</h2>
            <p className="mt-2 text-sm text-muted-foreground">{this.state.error.message}</p>
            <button
              type="button"
              onClick={() => this.setState({ error: null })}
              className="mt-3 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground"
            >
              {this.props.t('common.tryAgain')}
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
