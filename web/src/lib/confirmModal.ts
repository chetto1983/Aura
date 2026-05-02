// Imperative API + listener registry for the custom modal. Kept in a
// separate file from the host component so React fast-refresh works
// (a .tsx file can only export components when fast-refresh is on).

export type BaseRequest = {
  title: string;
  description?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
};

export type ConfirmOptions = BaseRequest;

export type PromptOptions = BaseRequest & {
  label?: string;
  placeholder?: string;
  defaultValue?: string;
  inputMode?: 'text' | 'numeric' | 'decimal';
  validate?: (value: string) => string | null;
};

export type ActiveRequest =
  | { kind: 'confirm'; opts: ConfirmOptions; resolve: (v: boolean) => void }
  | { kind: 'prompt'; opts: PromptOptions; resolve: (v: string | null) => void };

let activeListener: ((req: ActiveRequest) => void) | null = null;

export function _subscribe(listener: (req: ActiveRequest) => void): () => void {
  activeListener = listener;
  return () => {
    if (activeListener === listener) activeListener = null;
  };
}

function dispatch(req: ActiveRequest) {
  if (!activeListener) {
    // No host mounted — fall back to a denied confirm / null prompt rather
    // than throwing so callers don't crash if the provider is missing.
    if (req.kind === 'confirm') (req.resolve as (v: boolean) => void)(false);
    else (req.resolve as (v: string | null) => void)(null);
    return;
  }
  activeListener(req);
}

export function confirm(opts: ConfirmOptions): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    dispatch({ kind: 'confirm', opts, resolve });
  });
}

export function prompt(opts: PromptOptions): Promise<string | null> {
  return new Promise<string | null>((resolve) => {
    dispatch({ kind: 'prompt', opts, resolve });
  });
}

export function resolveCancelled(req: ActiveRequest) {
  if (req.kind === 'confirm') (req.resolve as (v: boolean) => void)(false);
  else (req.resolve as (v: string | null) => void)(null);
}
