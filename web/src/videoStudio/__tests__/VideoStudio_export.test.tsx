import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Asset } from '../../chat/attachments/types';
import i18n from '../../i18n/i18n';
import type { VideoProject } from '../project';
import { NoProjectAudioError } from '../videoflow_exportAudio';
import { ExportSourceError } from '../videoflow_media';
import { ExportPanel } from '../VideoStudio_export';

// The export bar's failures, read through the REAL bundle in both languages: a failure reaches
// the operator only as the sentence this panel picks, and a key that resolves to nothing would
// ship as an empty alert. The exports themselves are videoflow_export.test.ts's subject.

const flow = vi.hoisted(() => ({ exportProject: vi.fn(), exportProjectAudio: vi.fn() }));
vi.mock('../videoflow', async (original) => ({
  ...(await original<typeof import('../videoflow')>()),
  exportProject: flow.exportProject,
}));
vi.mock('../videoflow_exportAudio', async (original) => ({
  ...(await original<typeof import('../videoflow_exportAudio')>()),
  exportProjectAudio: flow.exportProjectAudio,
}));

const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../../mediaEdit/download', () => ({ downloadBlob }));

// The asset row the library lists a source by: what the failure is named with.
const api = vi.hoisted(() => ({ getAsset: vi.fn() }));
vi.mock('../../chat/attachments/api', () => api);

function row(id: string, fileName: string): Asset {
  return {
    id,
    status: 'complete',
    modality: 'video',
    file_name: fileName,
    mime_type: 'video/mp4',
    declared_size_bytes: 1,
    size_bytes: 1,
  };
}

const project: VideoProject = {
  id: 'p',
  name: 'demo',
  size: { width: 320, height: 180 },
  fps: 30,
  sources: [],
  video: [],
  overlays: [],
};

/** Render the panel and press `key`'s button; the export fails, and the panel settles, inside act. */
async function press(key: string): Promise<void> {
  render(
    <ExportPanel
      project={project}
      fileName="demo.mp4"
      audioFileName="demo.wav"
      urls={{ assetUrl: (id) => `/api/assets/${id}/download` }}
    />,
  );
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: i18n.t(key) }));
    await turn();
  });
}

/** One macrotask: every promise the panel chained — the failure, the name read, the reset — has
 *  settled by then, so its state updates all land inside the surrounding act. */
function turn(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
}

function alertText(): string | null | undefined {
  return screen.queryByRole('alert')?.textContent;
}

beforeEach(() => {
  flow.exportProject.mockReset();
  flow.exportProjectAudio.mockReset();
  downloadBlob.mockReset();
  api.getAsset.mockReset();
  api.getAsset.mockImplementation((id: string) =>
    Promise.resolve(row(id, id === 'asset-b' ? 'clip-b.mp4' : 'voce.m4a')),
  );
});

afterEach(async () => {
  // The panel is still mounted here, and a language change re-renders it.
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('ExportPanel failures', () => {
  it.each([
    ['en', 'The export stopped: clip-b.mp4 could not be fetched, so no file was written.'],
    [
      'it',
      'Esportazione interrotta: non è stato possibile recuperare clip-b.mp4, quindi non è stato scritto alcun file.',
    ],
  ])(
    'names the source it could not fetch by its file (%s), and downloads nothing',
    async (language, text) => {
      await i18n.changeLanguage(language);
      flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

      await press('videoStudio.export.action');

      expect(alertText()).toBe(text);
      expect(api.getAsset).toHaveBeenCalledWith('asset-b');
      expect(downloadBlob).not.toHaveBeenCalled();
    },
  );

  it('names a source the renderer could not read, in the sound export too', async () => {
    flow.exportProjectAudio.mockRejectedValue(new ExportSourceError('asset-c', 'undecodable'));

    await press('videoStudio.export.audioAction');

    expect(alertText()).toBe(
      'The export stopped: the renderer could not read voce.m4a, so no file was written.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it.each([
    ['whose row is gone too', () => Promise.reject(new Error('asset not found'))],
    ['whose row has no name', () => Promise.resolve(row('asset-b', ''))],
  ])('falls back to the asset id for a source %s', async (_why, answer) => {
    api.getAsset.mockImplementation(answer);
    flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

    await press('videoStudio.export.action');

    expect(alertText()).toBe(
      'The export stopped: asset-b could not be fetched, so no file was written.',
    );
  });

  it('says nothing when the operator cancels while the name is read', async () => {
    let answer: ((asset: Asset) => void) | undefined;
    api.getAsset.mockImplementation(
      () =>
        new Promise<Asset>((resolve) => {
          answer = resolve;
        }),
    );
    flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

    await press('videoStudio.export.action');
    expect(answer).toBeDefined();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.export.cancel') }));
      answer?.(row('asset-b', 'clip-b.mp4'));
      await turn();
    });

    expect(screen.getByRole('button', { name: i18n.t('videoStudio.export.action') })).toBeTruthy();
    expect(alertText()).toBeUndefined();
  });

  it('says a project with no sound has none to export', async () => {
    flow.exportProjectAudio.mockRejectedValue(new NoProjectAudioError());

    await press('videoStudio.export.audioAction');

    expect(alertText()).toBe(i18n.t('videoStudio.export.noAudio'));
  });

  it('reports any other failure with its own reason', async () => {
    flow.exportProject.mockRejectedValue(new Error('encoder gone'));

    await press('videoStudio.export.action');

    expect(alertText()).toBe('The export failed: encoder gone');
    expect(api.getAsset).not.toHaveBeenCalled();
  });
});
