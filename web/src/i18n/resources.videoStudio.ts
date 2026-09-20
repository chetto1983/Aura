// resources.videoStudio.ts — every string of the multi-track video editor: the lanes, the
// commands, the seven refusals the commands raise BY KEY, the project file and the export.
//
// `videoStudio.refusal.*` is the one group whose paths are not a choice: `CommandRefusal`
// carries `videoStudio.refusal.<name>` as its `reasonKey` and the workspace shows `t(reasonKey)`
// unchanged, so renaming one of these five here renames nothing in commands.ts — it makes the
// refusal render as its own key. The last two (`sourceUndecodable`, `sourceMissingAsset`) are
// the shell's own, raised at the door where a source is read rather than inside a command.

export const videoStudioEn = {
  videoStudio: {
    title: 'Video editor',
    untitled: 'Untitled project',
    close: 'Close the editor',
    empty: 'Add a clip to begin.',
    commands: 'Editing commands',
    newTitle: 'Your title',
    frameAdopted: 'The project now uses this clip’s frame: {{width}} × {{height}}.',
    unplayable_one:
      'One clip plays a source the library no longer holds. Remove it to preview and export again.',
    unplayable_other:
      '{{count}} clips play a source the library no longer holds. Remove them to preview and export again.',
    problem: 'That edit could not be made: {{reason}}',
    open: {
      fromStudio: 'Open in the video editor',
      fromClip: 'Open in the multi-track editor',
      resume: 'Reopen the last project you saved here',
      loading: 'Opening the project…',
      failed: 'The project could not be opened.',
    },
    command: {
      addSource: 'Add a clip',
      split: 'Split at the playhead',
      remove: 'Remove the selection',
      addText: 'Add a title',
      undo: 'Undo',
      redo: 'Redo',
    },
    source: {
      pick: 'Choose a clip or a still',
      reading: 'Reading the clip…',
      uploading: 'Uploading {{name}}…',
      failed: 'The clip could not be added: {{reason}}',
    },
    timeline: {
      label: 'Timeline',
      videoLane: 'Video',
      overlayLane: 'Overlay {{index}}',
      clip: 'Clip {{index}}',
      overlayText: 'Title {{index}}',
      overlayImage: 'Image {{index}}',
      trimStart: 'Start of clip {{index}}',
      trimEnd: 'End of clip {{index}}',
      playhead: 'Playhead',
      zoomIn: 'Zoom in',
      zoomOut: 'Zoom out',
    },
    stage: {
      picture: 'Select the clip on screen',
      selection: 'Move the selected overlay',
      failed: 'The preview could not be drawn.',
    },
    inspector: {
      label: 'Properties',
      empty: 'Select a clip or an overlay to edit it.',
      start: 'Start',
      end: 'End',
      mute: 'Mute',
      rangeFrom: 'Remove from',
      rangeTo: 'Remove to',
      removeRange: 'Remove that stretch',
      text: 'Text',
      size: 'Text size',
      color: 'Colour',
      asset: 'Image asset id',
      scale: 'Scale',
      animation: 'Animation',
      animations: {
        none: 'None',
        fadeIn: 'Fade in',
        fadeOut: 'Fade out',
      },
    },
    refusal: {
      sourceMissing: 'That clip’s source is no longer in the project.',
      trimPastSource: 'A clip cannot play more than its source holds.',
      splitOnBoundary: 'There is nothing to cut here: the playhead sits on a clip’s edge.',
      emptyRange: 'That stretch of the timeline is empty.',
      overlayOverlap:
        'Two overlays cannot cover the same instant of one lane. Put this one on a lane of its own.',
      sourceUndecodable:
        'This browser cannot decode that clip, so it would export as black frames. Convert it to MP4, or try Chrome or Edge.',
      sourceMissingAsset:
        'A clip this project uses is no longer in your library: it cannot be played or exported.',
    },
    confirm: {
      title_one: 'An overlay will be removed',
      title_other: '{{count}} overlays will be removed',
      body_one: 'It hangs on the material this edit takes away, and goes with it.',
      body_other: 'They hang on the material this edit takes away, and go with it.',
      proceed: 'Make the edit',
      cancel: 'Leave it as it is',
    },
    export: {
      action: 'Export',
      progress: 'Export progress',
      percent: 'Exporting… {{percent}}%',
      cancel: 'Cancel',
      failed: 'The export failed: {{reason}}',
      empty: 'There is nothing to export yet.',
    },
    save: {
      action: 'Save the project',
      saving: 'Saving…',
      saved: 'Project saved.',
      failed: 'The project could not be saved: {{reason}}',
    },
  },
};

export const videoStudioIt = {
  videoStudio: {
    title: 'Editor video',
    untitled: 'Progetto senza nome',
    close: "Chiudi l'editor",
    empty: 'Aggiungi una clip per cominciare.',
    commands: 'Comandi di modifica',
    newTitle: 'Il tuo titolo',
    frameAdopted: 'Il progetto usa ora il fotogramma di questa clip: {{width}} × {{height}}.',
    unplayable_one:
      'Una clip riproduce una sorgente che la libreria non ha più. Rimuovila per tornare a vedere in anteprima e a esportare.',
    unplayable_other:
      '{{count}} clip riproducono una sorgente che la libreria non ha più. Rimuovile per tornare a vedere in anteprima e a esportare.',
    problem: 'Non è stato possibile applicare la modifica: {{reason}}',
    open: {
      fromStudio: "Apri nell'editor video",
      fromClip: "Apri nell'editor multitraccia",
      resume: "Riapri l'ultimo progetto che hai salvato qui",
      loading: 'Apertura del progetto…',
      failed: 'Non è stato possibile aprire il progetto.',
    },
    command: {
      addSource: 'Aggiungi una clip',
      split: "Dividi all'indicatore",
      remove: 'Rimuovi la selezione',
      addText: 'Aggiungi un titolo',
      undo: 'Annulla',
      redo: 'Ripristina',
    },
    source: {
      pick: 'Scegli una clip o un’immagine',
      reading: 'Lettura della clip…',
      uploading: 'Caricamento di {{name}}…',
      failed: 'Non è stato possibile aggiungere la clip: {{reason}}',
    },
    timeline: {
      label: 'Linea del tempo',
      videoLane: 'Video',
      overlayLane: 'Sovrimpressione {{index}}',
      clip: 'Clip {{index}}',
      overlayText: 'Titolo {{index}}',
      overlayImage: 'Immagine {{index}}',
      trimStart: 'Inizio della clip {{index}}',
      trimEnd: 'Fine della clip {{index}}',
      playhead: 'Indicatore di riproduzione',
      zoomIn: 'Ingrandisci',
      zoomOut: 'Riduci',
    },
    stage: {
      picture: 'Seleziona la clip a schermo',
      selection: 'Sposta la sovrimpressione selezionata',
      failed: "Non è stato possibile disegnare l'anteprima.",
    },
    inspector: {
      label: 'Proprietà',
      empty: 'Seleziona una clip o una sovrimpressione per modificarla.',
      start: 'Inizio',
      end: 'Fine',
      mute: 'Muto',
      rangeFrom: 'Togli da',
      rangeTo: 'Togli fino a',
      removeRange: 'Togli quel tratto',
      text: 'Testo',
      size: 'Dimensione del testo',
      color: 'Colore',
      asset: "ID dell'immagine",
      scale: 'Scala',
      animation: 'Animazione',
      animations: {
        none: 'Nessuna',
        fadeIn: 'Dissolvenza in apertura',
        fadeOut: 'Dissolvenza in chiusura',
      },
    },
    refusal: {
      sourceMissing: 'La sorgente di quella clip non è più nel progetto.',
      trimPastSource: 'Una clip non può riprodurre più di quanto contenga la sua sorgente.',
      splitOnBoundary: "Qui non c'è niente da tagliare: l'indicatore è sul bordo di una clip.",
      emptyRange: 'Quel tratto della linea del tempo è vuoto.',
      overlayOverlap:
        'Due sovrimpressioni non possono coprire lo stesso istante della stessa traccia. Metti questa su una traccia tutta sua.',
      sourceUndecodable:
        'Questo browser non riesce a decodificare quella clip: verrebbe esportata come fotogrammi neri. Convertila in MP4, oppure prova con Chrome o Edge.',
      sourceMissingAsset:
        'Una clip usata da questo progetto non è più nella tua libreria: non può essere riprodotta né esportata.',
    },
    confirm: {
      title_one: 'Una sovrimpressione verrà rimossa',
      title_other: '{{count}} sovrimpressioni verranno rimosse',
      body_one: 'È appesa al materiale che questa modifica toglie, e se ne va con lui.',
      body_other: 'Sono appese al materiale che questa modifica toglie, e se ne vanno con lui.',
      proceed: 'Applica la modifica',
      cancel: 'Lascia com’è',
    },
    export: {
      action: 'Esporta',
      progress: "Avanzamento dell'esportazione",
      percent: 'Esportazione… {{percent}}%',
      cancel: 'Annulla',
      failed: 'Esportazione non riuscita: {{reason}}',
      empty: "Non c'è ancora niente da esportare.",
    },
    save: {
      action: 'Salva il progetto',
      saving: 'Salvataggio…',
      saved: 'Progetto salvato.',
      failed: 'Non è stato possibile salvare il progetto: {{reason}}',
    },
  },
};
