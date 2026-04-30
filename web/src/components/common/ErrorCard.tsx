interface ErrorCardProps {
  error: Error;
  title?: string;
  onRetry?: () => void;
}

export function ErrorCard({ error, title = 'Something went wrong', onRetry }: ErrorCardProps) {
  return (
    <div className="p-6">
      <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
        <h2 className="text-base font-semibold">{title}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{error.message}</p>
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="mt-3 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90"
          >
            Try again
          </button>
        )}
      </div>
    </div>
  );
}