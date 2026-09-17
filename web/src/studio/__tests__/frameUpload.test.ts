import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { presignAsset } from '../../chat/attachments/api';
import { putWithProgress } from '../../chat/attachments/upload';
import { uploadStudioFrame } from '../frameUpload';

vi.mock('../../chat/attachments/api', () => ({ presignAsset: vi.fn() }));
vi.mock('../../chat/attachments/upload', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../chat/attachments/upload')>()),
  putWithProgress: vi.fn(),
}));

const presignMock = vi.mocked(presignAsset);
const putMock = vi.mocked(putWithProgress);

const presigned = {
  asset: { id: 'asset-1', file_name: 'cat.png' },
  upload: {
    upload_url: 'https://objects.example/put/cat.png?sig=1',
    method: 'PUT',
    required_headers: { 'Content-Type': 'image/png' },
    expires_at: '2026-09-17T12:00:00Z',
  },
} as unknown as Awaited<ReturnType<typeof presignAsset>>;

const finalized = { id: 'asset-1', file_name: 'cat.png', mime_type: 'image/png' };

function pngFile(): File {
  return new File([new Uint8Array([1, 2, 3])], 'cat.png', { type: 'image/png' });
}

describe('uploading a Studio frame', () => {
  beforeEach(() => {
    presignMock.mockResolvedValue(presigned);
    putMock.mockResolvedValue(undefined);
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify(finalized), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('presigns a threadless image, PUTs it, then finalizes it through the Studio route', async () => {
    const onProgress = vi.fn();

    const ref = await uploadStudioFrame(pngFile(), onProgress);

    // thread_id '' — a Studio frame belongs to the identity, not to a conversation.
    expect(presignMock).toHaveBeenCalledWith({
      thread_id: '',
      file_name: 'cat.png',
      mime_type: 'image/png',
      size_bytes: 3,
      modality_hint: 'image',
    });
    const [url, file, headers, progress] = putMock.mock.calls[0] ?? [];
    expect(url).toBe('https://objects.example/put/cat.png?sig=1');
    expect(file).toBeInstanceOf(File);
    expect(headers).toEqual({ 'Content-Type': 'image/png' });
    expect(progress).toBe(onProgress);
    // The Studio's own finalize, not /api/assets/{id}/finalize: it accepts an image and
    // refuses anything else.
    expect(fetch).toHaveBeenCalledWith(
      '/api/studio/uploads/asset-1/finalize',
      expect.objectContaining({ method: 'POST', credentials: 'same-origin' }),
    );
    expect(ref).toEqual(finalized);
  });

  it('does not finalize before the bytes are up', async () => {
    let release = (): void => undefined;
    putMock.mockReturnValue(
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    const pending = uploadStudioFrame(pngFile(), vi.fn());
    await Promise.resolve();

    // Finalizing an object the store has not received yet answers a refusal about a file the
    // operator did send.
    expect(fetch).not.toHaveBeenCalled();

    release();
    await pending;
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('never finalizes an upload that failed', async () => {
    putMock.mockRejectedValue(new Error('upload failed: HTTP 403'));

    await expect(uploadStudioFrame(pngFile(), vi.fn())).rejects.toThrow('upload failed: HTTP 403');
    expect(fetch).not.toHaveBeenCalled();
  });

  it('hints the modality from the file, so the server can refuse what is not an image', async () => {
    const pdf = new File([new Uint8Array([1])], 'manual.pdf', { type: 'application/pdf' });

    await uploadStudioFrame(pdf, vi.fn());

    expect(presignMock).toHaveBeenCalledWith(
      expect.objectContaining({ modality_hint: 'document', file_name: 'manual.pdf' }),
    );
  });
});
