// @echogarden/fvad-wasm ships libfvad as an Emscripten module with no declarations. Declared is
// exactly the surface audioSpeech.ts calls — libfvad's C API (fvad.h) and the heap view Emscripten
// exports (`Module["HEAP16"]`, fvad.js `updateMemoryViews`) — and nothing more.
declare module '@echogarden/fvad-wasm' {
  interface FvadModule {
    _fvad_new(): number;
    _fvad_free(handle: number): void;
    _fvad_set_sample_rate(handle: number, rate: number): number;
    _fvad_set_mode(handle: number, mode: number): number;
    _fvad_process(handle: number, frame: number, length: number): number;
    _malloc(bytes: number): number;
    _free(pointer: number): void;
    readonly HEAP16: Int16Array;
  }
  export default function fvadInit(options: {
    readonly locateFile: (path: string) => string;
  }): Promise<FvadModule>;
}
