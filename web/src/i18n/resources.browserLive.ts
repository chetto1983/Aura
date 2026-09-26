// The `browserLive.*` bundle: the live view of an agent-browser session (prd.md §12).

export const browserLiveEn = {
  browserLive: {
    title: 'Live browser',
    done: 'Done',
    keyboard: 'Show keyboard',
    stageLabel: 'Live browser page. Click to interact, type to enter text.',
    frameAlt: 'The page open in your sandbox browser',
    notice:
      "You are using the browser in your own sandbox. What you type goes to the page, not to the model, but the session it saves stays in the sandbox, where the agent's tools can read it.",
    connecting: 'Connecting to your sandbox browser…',
    ended: {
      disconnected: 'The connection to your sandbox browser was lost.',
      taken_over: 'This browser is now open in another window.',
      no_such_session: 'The agent has no browser open under this name any more.',
      stream_closed: 'The agent closed this browser.',
      stream_unreachable: 'The sandbox browser is not running.',
    },
  },
};

export const browserLiveIt = {
  browserLive: {
    title: 'Browser dal vivo',
    done: 'Fatto',
    keyboard: 'Mostra tastiera',
    stageLabel: 'Pagina del browser dal vivo. Fai clic per interagire, digita per inserire testo.',
    frameAlt: 'La pagina aperta nel browser della tua sandbox',
    notice:
      "Stai usando il browser della tua sandbox. Ciò che digiti va alla pagina, non al modello, ma la sessione che salva resta nella sandbox, dove gli strumenti dell'agente possono leggerla.",
    connecting: 'Connessione al browser della sandbox…',
    ended: {
      disconnected: 'La connessione al browser della sandbox si è interrotta.',
      taken_over: 'Questo browser ora è aperto in un’altra finestra.',
      no_such_session: "L'agente non ha più un browser aperto con questo nome.",
      stream_closed: "L'agente ha chiuso questo browser.",
      stream_unreachable: 'Il browser della sandbox non è in esecuzione.',
    },
  },
};
