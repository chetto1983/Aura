// resources.videoStudioAudio.ts — the audio lanes' strings, merged under `videoStudio.audio`.
// `refusal.*` paths are not a choice: commands_audio.ts raises them BY KEY and the workspace shows
// `t(reasonKey)` unchanged.

export const videoStudioAudioEn = {
  lane: 'Audio {{index}}',
  item: 'Sound {{index}}',
  add: 'Add audio',
  pick: 'Choose a sound',
  extract: 'Extract audio',
  fadeIn: 'Fade in',
  fadeOut: 'Fade out',
  startsAt: 'Starts at',
  in: 'In',
  out: 'Out',
  trimStart: 'Start of sound {{index}}',
  trimEnd: 'End of sound {{index}}',
  refusal: {
    noClip: 'A sound hangs on a clip: add a clip first.',
    pastEnd: 'There is no film at that point to put a sound on.',
    beforeStart: 'A sound cannot start before the film does.',
    overlap: 'Two sounds cannot cover the same instant of one audio lane.',
    notSound: 'That source has no sound to play.',
    fadesTooLong: 'The fades together are longer than the sound.',
    undecodable: 'This browser cannot decode that sound.',
  },
};

export const videoStudioAudioIt: typeof videoStudioAudioEn = {
  lane: 'Audio {{index}}',
  item: 'Suono {{index}}',
  add: 'Aggiungi audio',
  pick: 'Scegli un suono',
  extract: 'Estrai audio',
  fadeIn: 'Dissolvenza in entrata',
  fadeOut: 'Dissolvenza in uscita',
  startsAt: 'Inizia a',
  in: 'Inizio',
  out: 'Fine',
  trimStart: 'Inizio del suono {{index}}',
  trimEnd: 'Fine del suono {{index}}',
  refusal: {
    noClip: 'Un suono è agganciato a una clip: aggiungi prima una clip.',
    pastEnd: 'In quel punto non c’è filmato su cui mettere un suono.',
    beforeStart: 'Un suono non può iniziare prima del filmato.',
    overlap: 'Due suoni non possono coprire lo stesso istante di una traccia audio.',
    notSound: 'Quella sorgente non ha un suono da riprodurre.',
    fadesTooLong: 'Le dissolvenze insieme durano più del suono.',
    undecodable: 'Questo browser non riesce a decodificare quel suono.',
  },
};
