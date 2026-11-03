import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { postJSON } from './governanceApi';
import { getJSON } from '@/api/json';
import { Button } from '@/components/ui/button';

interface ImageState {
  readonly present: boolean;
  readonly pulling: boolean;
  readonly done?: number;
  readonly total?: number;
  readonly reason?: string;
  readonly error?: string;
}

const imagePath = '/api/governance/sign-in/image';

export function SignInImageGate({
  onReady,
  onPaste,
}: {
  readonly onReady: () => void;
  readonly onPaste: () => void;
}) {
  const { t } = useTranslation();
  const [image, setImage] = useState<ImageState>({ present: false, pulling: false });
  const [pending, setPending] = useState(false);
  const sequence = useRef(0);
  const notified = useRef(false);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const check = async () => {
      const request = ++sequence.current;
      try {
        const state = await getJSON<ImageState>(imagePath, controller.signal);
        if (!active || request !== sequence.current) return;
        setImage(state);
        if (state.present && !notified.current) {
          notified.current = true;
          onReady();
        }
      } catch {
        if (active && request === sequence.current) {
          setImage({ present: false, pulling: false, error: t('signIn.imageFailed') });
        }
      }
    };
    void check();
    const timer = window.setInterval(() => {
      if (!notified.current) void check();
    }, 1500);
    return () => {
      active = false;
      controller.abort();
      window.clearInterval(timer);
    };
  }, [onReady, t]);

  const download = async () => {
    const request = ++sequence.current;
    setPending(true);
    try {
      const state = await postJSON<ImageState>(imagePath, {});
      if (request === sequence.current) setImage(state);
    } catch {
      if (request === sequence.current)
        setImage({ present: false, pulling: false, error: t('signIn.imageFailed') });
    } finally {
      setPending(false);
    }
  };
  const progress = image.total
    ? Math.min(100, Math.floor((100 * (image.done ?? 0)) / image.total))
    : 0;
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <p className="text-[14px] text-text">{t('signIn.imageSize')}</p>
      {image.pulling ? (
        <progress
          aria-label={t('signIn.imageProgress')}
          aria-valuenow={progress}
          value={progress}
          max={100}
          className="w-full"
        />
      ) : (
        <div>
          <Button
            type="button"
            size="sm"
            disabled={pending || image.present}
            onClick={() => {
              void download();
            }}
          >
            {t('signIn.imageDownload')}
          </Button>
        </div>
      )}
      {image.error ? (
        <p role="alert" className="text-[13px] text-danger">
          {image.error}
        </p>
      ) : null}
      <div>
        <Button type="button" variant="ghost" size="sm" onClick={onPaste}>
          {t('governance.mcp.detail.authorization.ownBrowser')}
        </Button>
      </div>
    </div>
  );
}
