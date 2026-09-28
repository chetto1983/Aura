# Audio fixtures for the Video Studio

16 kHz, mono, 16-bit PCM WAV. Made by `spikes/video-studio-audio/` on 2026-09-27.

| File                | What it is                                                                   | How it was made                                                                                                                                            |
| ------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `music.wav`         | 8 s A-minor progression at a constant level: RMS −18.24 dBFS                 | `make-music.mjs`, three equal sines per chord, amplitude 0.1                                                                                               |
| `speech.wav`        | Three English phrases: 1.0 s lead silence, 1.5 s gaps, 1.0 s tail            | The lab VM's own `POST /api/tts` (`tts-phrases.sh`; it answered 24 kHz mono MP3), trimmed at −50 dBFS, peak-normalised to −3 dBFS (`make-fixtures.mjs`)    |
| `speech-noisy.wav`  | `speech.wav` plus seeded pink noise at 10 dB SNR                             | Paul Kellet's pink filter, seed 20260927, SNR measured over the speech windows                                                                             |
| `speech.truth.json` | The speech windows in seconds, exact to the sample                           | Written by the same run; `windows` is the ground truth a VAD is scored on                                                                                  |
| `speech-phrase.mp3` | "The river runs past the old mill every morning.", as `/api/tts` answered it | The lab VM's own `POST /api/tts` (`tts-phrases.sh`, phrase 1), saved byte for byte (24 kHz mono MP3): the voice E2E serves it where no voice is configured |

`verify-fixtures.mjs` prints the levels from the committed bytes. On 2026-09-27:

```
music RMS -18.24 dBFS (want -18.24)
speech.wav: speech windows -18.02, -18.18, -18.35 dBFS; gaps -200.00, -200.00, -200.00 dBFS
speech-noisy.wav: speech windows -17.63, -17.74, -17.92 dBFS; gaps -27.63, -27.95, -29.00 dBFS
```
