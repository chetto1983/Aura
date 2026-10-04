import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import { SaveFileLink } from './SaveFileLink';
import { sameLinks, useShareFiles, type SaveLink } from './useSaveFile';
import {
  emulateHomeScreen,
  leaveHomeScreen,
  sharedFiles,
  type HomeScreenSheet,
} from '@/test/iosHomeScreen';

// SaveFileLink in and out of iOS's home-screen app. Outside it the link must download exactly
// as it did; inside it the bytes go to the share sheet, with a second tap when the tap's
// activation lapsed during the fetch (webkit.org/blog/13862).

const HREF = '/api/assets/a1/download';
const fetchMock = vi.fn<(url: string, init: RequestInit) => Promise<unknown>>();
let sheet: HomeScreenSheet;

function homeScreen(): void {
  sheet = emulateHomeScreen();
  // The probe must describe the real file: iOS decides by its type whether the sheet takes it.
  sheet.accepts = ([probe]) =>
    probe instanceof File && probe.name === 'Appunti.pdf' && probe.type === 'application/pdf';
}

function pdfResponse(): Promise<unknown> {
  return Promise.resolve({
    ok: true,
    status: 200,
    blob: () => Promise.resolve(new Blob(['%PDF-1.4'], { type: 'application/octet-stream' })),
  });
}

function link(): HTMLElement {
  render(
    <SaveFileLink
      href={HREF}
      fileName="Appunti.pdf"
      mimeType="application/pdf"
      label="Download"
      aria-label="Download Appunti.pdf"
    />,
  );
  return screen.getByRole('link');
}

// Reads whether the link's own handler cancelled the click, then cancels it regardless: jsdom
// does not implement the navigation a real download would start.
function tap(target: HTMLElement): boolean {
  let prevented = false;
  const record = (event: Event) => {
    prevented = event.defaultPrevented;
    event.preventDefault();
  };
  document.addEventListener('click', record);
  fireEvent.click(target);
  document.removeEventListener('click', record);
  return prevented;
}

function sharedFile(call = 0): File {
  const file = sharedFiles(sheet, call)[0];
  if (file === undefined) throw new Error(`share call ${String(call)} carried no file`);
  return file;
}

beforeEach(async () => {
  await act(() => i18n.changeLanguage('en'));
  fetchMock.mockReset().mockImplementation(pdfResponse);
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  leaveHomeScreen();
});

describe('SaveFileLink', () => {
  it('downloads as it always did outside the home-screen app', () => {
    const anchor = link();
    expect(anchor.getAttribute('href')).toBe(HREF);
    expect(anchor.getAttribute('download')).toBe('Appunti.pdf');
    expect(tap(anchor)).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('hands the file to the share sheet while the tap is still live', async () => {
    homeScreen();
    const anchor = link();
    expect(tap(anchor)).toBe(true);

    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(1);
    });
    expect(fetchMock).toHaveBeenCalledWith(HREF, { credentials: 'same-origin' });
    // The card's media type, not the download route's octet-stream: the sheet offers what
    // fits the file.
    expect(sharedFile().name).toBe('Appunti.pdf');
    expect(sharedFile().type).toBe('application/pdf');
    expect(sharedFile().size).toBe('%PDF-1.4'.length);
    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Download Appunti.pdf');
    });
  });

  it('shows that it is fetching while the bytes are on their way', async () => {
    homeScreen();
    let answer: (value: unknown) => void = () => undefined;
    fetchMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.textContent).toContain('Preparing…');
    });
    expect(anchor.getAttribute('aria-busy')).toBe('true');
    // A second tap while fetching does not start a second fetch.
    expect(tap(anchor)).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    answer(await pdfResponse());
    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(1);
    });
  });

  it('asks for a second tap when the activation lapsed during the fetch', async () => {
    homeScreen();
    sheet.activation.isActive = false;
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Save');
    });
    expect(sheet.share).not.toHaveBeenCalled();

    expect(tap(anchor)).toBe(true);
    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(1);
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('keeps the bytes for the next tap when the sheet refuses a lapsed activation', async () => {
    homeScreen();
    sheet.share.mockRejectedValueOnce(new DOMException('no activation', 'NotAllowedError'));
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Save');
    });
    tap(anchor);
    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(2);
    });
    expect(sharedFile(1).name).toBe('Appunti.pdf');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('treats closing the sheet as done, not as a failure', async () => {
    homeScreen();
    sheet.share.mockRejectedValueOnce(new DOMException('closed', 'AbortError'));
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Download Appunti.pdf');
    });
    // The bytes are dropped with the sheet: the next tap fetches the file afresh.
    tap(anchor);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });
  });

  it('says so when the file cannot be fetched, and retries on the next tap', async () => {
    homeScreen();
    // A refused fetch still has a body -- the sign-in page -- and it must not reach the sheet.
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 401,
      blob: () => Promise.resolve(new Blob(['<html>sign in</html>'])),
    });
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.textContent).toContain("Couldn't save — tap to retry");
    });
    tap(anchor);
    await waitFor(() => {
      expect(sheet.share).toHaveBeenCalledTimes(1);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('reports a share error that is neither a close nor a lapsed tap', async () => {
    homeScreen();
    sheet.share.mockRejectedValueOnce(new TypeError('unsupported'));
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.textContent).toContain("Couldn't save — tap to retry");
    });
  });

  it('says so when the browser can probe the sheet but not open it', async () => {
    homeScreen();
    Reflect.deleteProperty(navigator, 'share');
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.textContent).toContain("Couldn't save — tap to retry");
    });
  });

  it('downloads as before on a home-screen app whose browser has no share sheet', () => {
    Object.defineProperty(navigator, 'standalone', { value: true, configurable: true });
    expect(tap(link())).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('asks for a second tap when the browser cannot tell whether the tap is live', async () => {
    homeScreen();
    Reflect.deleteProperty(navigator, 'userActivation');
    const anchor = link();
    tap(anchor);

    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Save');
    });
    expect(sheet.share).not.toHaveBeenCalled();
  });

  // The preview modal keeps one link and changes its file: a "Save" left by the previous file
  // must not share that file's bytes under the new one.
  it('forgets a state that belonged to another file', async () => {
    homeScreen();
    sheet.activation.isActive = false;
    const { rerender } = render(
      <SaveFileLink
        href={HREF}
        fileName="Appunti.pdf"
        mimeType="application/pdf"
        label="Download"
      />,
    );
    const anchor = screen.getByRole('link');
    tap(anchor);
    await waitFor(() => {
      expect(anchor.getAttribute('aria-label')).toBe('Save');
    });

    const other = '/api/assets/a2/download';
    rerender(
      <SaveFileLink
        href={other}
        fileName="Appunti.pdf"
        mimeType="application/pdf"
        label="Download"
      />,
    );
    expect(anchor.textContent).toContain('Download');
    tap(anchor);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith(other, { credentials: 'same-origin' });
    });
    expect(sheet.share).not.toHaveBeenCalled();
  });

  it('downloads as before when the sheet cannot take this file', () => {
    homeScreen();
    sheet.accepts = () => false;
    expect(tap(link())).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

function linkTo(href: string): SaveLink {
  return { href, fileName: href.slice(href.lastIndexOf('/') + 1), mimeType: '' };
}

describe('sameLinks', () => {
  const a = linkTo('/a'),
    b = linkTo('/b'),
    c = linkTo('/c');

  it('is true only for the same files in the same order', () => {
    expect(sameLinks([a, b], [linkTo('/a'), linkTo('/b')])).toBe(true);
    expect(sameLinks([a, b], [b, a])).toBe(false);
    expect(sameLinks([a, b], [a, c])).toBe(false);
  });

  it('is false when one list is a prefix of the other', () => {
    expect(sameLinks([a], [a, b])).toBe(false);
    expect(sameLinks([a, b], [a])).toBe(false);
    expect(sameLinks([], [a])).toBe(false);
  });
});

describe('useShareFiles', () => {
  it('takes nothing when there is nothing to save, even in the home-screen app', () => {
    homeScreen();
    const { result } = renderHook(() => useShareFiles());
    expect(result.current.save([])).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
