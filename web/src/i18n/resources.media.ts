// resources.media.ts — cockpit strings for generated images and videos (spec §5): the
// generation frame and the inline image actions. The download label and the preview failure
// reuse artifacts.preview.download and artifacts.preview.error.
export const mediaEn = {
  media: {
    generation: {
      generatingImage: 'Generating image',
      generatingVideo: 'Generating video',
      arriving: 'Arriving in this chat',
      elapsed: 'Elapsed time {{time}}',
    },
    image: {
      zoom: 'View {{name}} full screen',
      close: 'Close full screen view',
      copy: 'Copy image',
      copied: 'Copied',
      copyFailed: "Couldn't copy the image. Try again or download it.",
    },
  },
} as const;

export const mediaIt = {
  media: {
    generation: {
      generatingImage: 'Generazione immagine',
      generatingVideo: 'Generazione video',
      arriving: 'In arrivo in questa chat',
      elapsed: 'Tempo trascorso {{time}}',
    },
    image: {
      zoom: 'Visualizza {{name}} a schermo intero',
      close: 'Chiudi la vista a schermo intero',
      copy: 'Copia immagine',
      copied: 'Copiata',
      copyFailed: "Impossibile copiare l'immagine. Riprova o scaricala.",
    },
  },
} as const;
