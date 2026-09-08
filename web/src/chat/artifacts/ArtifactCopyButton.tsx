import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Copy } from 'lucide-react';
import { useAssetSource } from './renderers/assetSourceContext';
import { Button } from '@/components/ui/button';

export function ArtifactCopyButton({ assetId }: { readonly assetId: string }) {
  const { t } = useTranslation();
  const { assetUrl, credentials } = useAssetSource();
  const [state, setState] = useState<'idle' | 'pending' | 'copied' | 'error'>('idle');
  async function copy() {
    setState('pending');
    try {
      const response = await fetch(assetUrl(assetId), { credentials });
      if (!response.ok) throw new Error('Source unavailable');
      await navigator.clipboard.writeText(await response.text());
      setState('copied');
    } catch {
      setState('error');
    }
  }
  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        disabled={state === 'pending'}
        onClick={() => {
          void copy();
        }}
        aria-label={t(state === 'copied' ? 'display.code.copied' : 'display.code.copyAria')}
      >
        {state === 'copied' ? <Check /> : <Copy />}
      </Button>
      {state === 'error' && (
        <span role="alert" className="text-xs text-danger">
          {t('artifacts.workspace.copyError')}
        </span>
      )}
    </>
  );
}
