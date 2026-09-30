// The Studio's own modules, bundled unchanged, on window for the harness to drive: the export
// (withLocalFonts + primeDecodedBuffers + exportVideo), the compile alone, the project parser, and
// the two browser-only analyses S4 measures.
import { cleanedFile, denoiseSamples, DENOISE_RATE } from '../../../web/src/videoStudio/audioClean';
import { decodeMono } from '../../../web/src/videoStudio/audioDecode';
import { detectSpeech } from '../../../web/src/videoStudio/audioSpeech';
import { loadProject } from '../../../web/src/videoStudio/projectStore';
import { exportProject, toVideoJSON } from '../../../web/src/videoStudio/videoflow';

Object.assign(window, {
  studio: { cleanedFile, decodeMono, denoiseSamples, DENOISE_RATE, detectSpeech, exportProject, loadProject, toVideoJSON },
});
