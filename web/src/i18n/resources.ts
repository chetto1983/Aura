import { adminEn, adminIt } from './resources.admin';
import {
  composerEffortEn,
  composerEffortIt,
  composerSkillPickerEn,
  composerSkillPickerIt,
} from './resources.composer';
import { displayEn, displayIt } from './resources.display';
import { footerEn, footerIt } from './resources.footer';
import { documentsEn, documentsIt } from './resources.documents';
import { chatEmptyEn, chatEmptyIt } from './resources.empty';
import { governanceEn, governanceIt } from './resources.governance';
import { graphEn, graphIt } from './resources.graph';
import { loginEn, loginIt } from './resources.login';
import { onboardingEn, onboardingIt } from './resources.onboarding';
import { settingsEn, settingsIt } from './resources.settings';
import { shareEn, shareIt } from './resources.share';

export const resources = {
  en: {
    translation: {
      language: {
        switcherLabel: 'Language',
        english: 'English',
        italian: 'Italiano',
      },
      theme: {
        switcherLabel: 'Theme',
        light: 'Light theme',
        dark: 'Dark theme',
      },
      secret: {
        show: 'Show {{label}}',
        hide: 'Hide {{label}}',
      },
      ...loginEn,
      shell: {
        primaryNav: 'Primary',
        mobileModes: 'Modes',
        navigation: 'Navigation',
        displayWorkspace: 'Display workspace',
        chatRegion: 'Chat',
        workspace: 'Operator cockpit',
        openNavigation: 'Open navigation',
        resizeNavigation: 'Resize conversation sidebar',
        resizeArtifacts: 'Resize artifacts sidebar',
        openRuntime: 'Open runtime status',
        closePanel: 'Close panel',
        logout: 'Sign out',
        modes: {
          chat: 'Chat',
          tree: 'Tree',
          graph: 'Graph',
          governance: 'Governance',
          documents: 'Documents',
          displays: 'Displays',
          settings: 'Settings',
        },
        modesCompact: {
          chat: 'Chat',
          graph: 'Graph',
          governance: 'Gov',
          documents: 'Docs',
          settings: 'Settings',
        },
        modeUnavailable: 'Coming soon',
      },
      chat: {
        scrollToBottom: 'Scroll to bottom',
        streaming: 'Streaming',
        loading: 'Loading',
        composer: {
          placeholder: 'Ask Aura',
          send: 'Send',
          sendAria: 'Send message',
          stop: 'Stop',
          stopAria: 'Stop the current response',
          effort: composerEffortEn,
        },
        skillPicker: composerSkillPickerEn,
        attachments: {
          add: 'Add files',
          remove: 'Remove {{name}}',
          uploading: 'Uploading {{progress}}%',
          processing: 'Processing',
          ready: 'Ready',
          failed: 'Failed',
          refused: 'Refused',
          retry: 'Retry',
          promote: 'Promote',
          mic: 'Record audio',
          micStop: 'Stop recording',
        },
        running: 'Running...',
        empty: chatEmptyEn,
        liveRun: {
          hint: 'A run is still in progress in this conversation. Sending re-enables when it finishes.',
        },
        error: {
          stream:
            'The response stopped unexpectedly. Retry the last message or check the runtime status.',
          connectionLost:
            'Connection lost mid-response. Showing the saved state of this conversation.',
          createConversation:
            "Couldn't start a new conversation. Check the runtime status and try again.",
          loadHistory:
            "Couldn't load this conversation's saved messages. Refresh the conversation and try again.",
          retry: 'Retry',
        },
        markdown: {
          copyCode: 'Copy code',
          codeCopied: 'Code copied',
        },
        reasoning: {
          show: 'Show reasoning',
          hide: 'Hide reasoning',
          pending: 'Thinking...',
        },
        tool: {
          showRaw: 'Show raw result',
          hideRaw: 'Hide raw result',
          duration: {
            seconds: '{{value}} s',
            minutes: '{{minutes}} min {{seconds}} s',
          },
          status: {
            running: 'Running',
            done: 'Done',
            error: 'Error',
          },
        },
        branch: {
          label: 'Branch {{current}} of {{count}}',
          previous: 'Previous branch',
          next: 'Next branch',
        },
        action: {
          copy: 'Copy',
          copied: 'Copied',
          edit: 'Edit',
          reload: 'Regenerate',
          speak: 'Read aloud',
          stopSpeaking: 'Stop reading',
          tooLong: 'Message too long to read fully',
        },
        edit: {
          save: 'Save and re-run',
          cancel: 'Cancel edit',
          label: 'Edit message',
        },
        voiceMode: {
          on: 'Voice mode on',
          off: 'Voice mode off',
        },
        dictation: {
          start: 'Dictate',
          stop: 'Stop dictation',
          listening: 'Listening…',
          transcribing: 'Transcribing…',
          error: 'No transcription. Try again or attach an audio file.',
        },
      },
      ...displayEn,
      ...shareEn,
      ...documentsEn,
      ...governanceEn,
      ...graphEn,
      ...settingsEn,
      ...adminEn,
      ...onboardingEn,
      conversations: {
        new: 'New chat',
        newPending: 'Creating chat...',
        heading: 'Conversations',
        loading: 'Loading conversations...',
        loadError: "Couldn't load conversations. Refresh the list.",
        untitled: 'Untitled',
        includeArchived: 'Show archived',
        archivedTag: 'Archived',
        renameLabel: 'Conversation title',
        empty: {
          heading: 'Start a run',
          body: 'Ask Aura a question to begin. Your conversations show up here.',
        },
        recency: {
          today: 'Today',
          yesterday: 'Yesterday',
          last7: 'Last 7 days',
          older: 'Older',
        },
        actions: {
          more: 'Conversation actions',
          rename: 'Rename',
          archive: 'Archive',
          unarchive: 'Unarchive',
          delete: 'Delete permanently',
        },
        delete: {
          title: 'Delete conversation?',
          body: 'This permanently deletes "{{title}}" and its messages. This can\'t be undone.',
          confirm: 'Delete permanently',
          cancel: 'Keep conversation',
        },
        search: {
          label: 'Search conversations',
          placeholder: 'Search conversations',
          searching: 'Searching...',
          empty: {
            heading: 'No matches',
            body: 'No conversations contain "{{query}}". Try a different term.',
          },
        },
      },
      approval: {
        badge: {
          aria_one: '{{count}} approval waiting',
          aria_other: '{{count}} approvals waiting',
          cleared: 'No approvals waiting.',
        },
        list: {
          label: 'Pending approvals',
          open: 'Open',
          empty: 'Nothing waiting on you.',
          untitled: 'Untitled',
        },
        kind: {
          clarification: 'Clarification',
          choice: 'Choice',
          approval: 'Approval',
        },
        frame: {
          approval: 'Approval required',
          input: 'Input required',
          review: 'Review the scope and consequence before continuing.',
        },
        lock: 'Answer the request above to continue.',
        card: {
          freeText: 'Your answer',
          freeTextPlaceholder: 'Type your answer',
          answer: 'Answer',
          decline: 'Decline',
          cancel: 'Cancel run',
          declined: 'Declined.',
          answered: 'Answered.',
          cancelled: 'Run cancelled.',
          confirmCancel: 'Stop this run?',
          confirmCancelYes: 'Stop run',
          confirmCancelNo: 'Keep running',
          error: "Couldn't resume this run. It may have already been answered or cancelled.",
        },
        terminal: {
          expired: 'Expired — auto-resolved.',
        },
      },
      ...footerEn,
      skeleton: {
        shell: 'Loading cockpit...',
        login: 'Loading sign-in...',
        page: 'Loading page...',
      },
      health: {
        title: 'Runtime',
        loading: 'Checking runtime...',
        unreachable:
          "Can't read runtime status. The health endpoints aren't responding - the server may be starting or down.",
        labels: {
          liveness: 'Liveness',
          readiness: 'Readiness',
          postgres: 'Postgres',
          neo4j: 'Neo4j',
          bindAddress: 'Bind address',
          build: 'Build',
        },
        status: {
          unavailable: 'Unavailable',
          live: 'Live',
          ready: 'Ready',
          degraded: 'Degraded',
          unknown: 'Unknown',
        },
        lastChecked: 'Last checked {{time}}',
        relative: {
          never: 'never',
          justNow: 'just now',
          secondsAgo: '{{count}}s ago',
          minutesAgo: '{{count}}m ago',
        },
      },
      notFound: {
        title: 'Page not found',
        beforeLink: "That cockpit page doesn't exist.",
        link: 'Return to dashboard',
      },
      errorBoundary: {
        title: 'Aura could not render this view.',
        action: 'Reload to try again.',
      },
    },
  },
  it: {
    translation: {
      language: {
        switcherLabel: 'Lingua',
        english: 'English',
        italian: 'Italiano',
      },
      theme: {
        switcherLabel: 'Tema',
        light: 'Tema chiaro',
        dark: 'Tema scuro',
      },
      secret: {
        show: 'Mostra {{label}}',
        hide: 'Nascondi {{label}}',
      },
      ...loginIt,
      shell: {
        primaryNav: 'Principale',
        mobileModes: 'Modalità',
        navigation: 'Navigazione',
        displayWorkspace: 'Area display',
        chatRegion: 'Area chat',
        workspace: 'Cockpit operatore',
        openNavigation: 'Apri navigazione',
        resizeNavigation: 'Ridimensiona barra conversazioni',
        resizeArtifacts: 'Ridimensiona barra artefatti',
        openRuntime: 'Apri stato runtime',
        closePanel: 'Chiudi pannello',
        logout: 'Disconnetti',
        modes: {
          chat: 'Chat',
          tree: 'Albero',
          graph: 'Grafo',
          governance: 'Governance',
          documents: 'Documenti',
          displays: 'Display',
          settings: 'Impostazioni',
        },
        modesCompact: {
          chat: 'Chat',
          graph: 'Grafo',
          governance: 'Gov',
          documents: 'Doc',
          settings: 'Imp.',
        },
        modeUnavailable: 'In arrivo',
      },
      chat: {
        scrollToBottom: 'Vai in fondo',
        streaming: 'Streaming',
        loading: 'Caricamento',
        composer: {
          placeholder: 'Chiedi ad Aura',
          send: 'Invia',
          sendAria: 'Invia messaggio',
          stop: 'Ferma',
          stopAria: 'Ferma la risposta in corso',
          effort: composerEffortIt,
        },
        skillPicker: composerSkillPickerIt,
        attachments: {
          add: 'Aggiungi file',
          remove: 'Rimuovi {{name}}',
          uploading: 'Caricamento {{progress}}%',
          processing: 'Elaborazione',
          ready: 'Pronto',
          failed: 'Errore',
          refused: 'Rifiutato',
          retry: 'Riprova',
          promote: 'Promuovi',
          mic: 'Registra audio',
          micStop: 'Ferma registrazione',
        },
        running: 'In esecuzione...',
        empty: chatEmptyIt,
        liveRun: {
          hint: "Un'esecuzione è ancora in corso in questa conversazione. L'invio si riattiva al termine.",
        },
        error: {
          stream:
            "La risposta si è interrotta inaspettatamente. Riprova l'ultimo messaggio o controlla lo stato del runtime.",
          connectionLost:
            'Connessione persa durante la risposta. Viene mostrato lo stato salvato della conversazione.',
          createConversation:
            'Impossibile avviare una nuova conversazione. Controlla lo stato runtime e riprova.',
          loadHistory:
            'Impossibile caricare i messaggi salvati di questa conversazione. Aggiorna la conversazione e riprova.',
          retry: 'Riprova',
        },
        markdown: {
          copyCode: 'Copia codice',
          codeCopied: 'Codice copiato',
        },
        reasoning: {
          show: 'Mostra ragionamento',
          hide: 'Nascondi ragionamento',
          pending: 'Ragionamento in corso...',
        },
        tool: {
          showRaw: 'Mostra risultato grezzo',
          hideRaw: 'Nascondi risultato grezzo',
          duration: {
            seconds: '{{value}} s',
            minutes: '{{minutes}} min {{seconds}} s',
          },
          status: {
            running: 'In corso',
            done: 'Completato',
            error: 'Errore',
          },
        },
        branch: {
          label: 'Ramo {{current}} di {{count}}',
          previous: 'Ramo precedente',
          next: 'Ramo successivo',
        },
        action: {
          copy: 'Copia',
          copied: 'Copiato',
          edit: 'Modifica',
          reload: 'Rigenera',
          speak: 'Leggi ad alta voce',
          stopSpeaking: 'Ferma lettura',
          tooLong: 'Messaggio troppo lungo per leggerlo tutto',
        },
        edit: {
          save: 'Salva e riesegui',
          cancel: 'Annulla modifica',
          label: 'Modifica messaggio',
        },
        voiceMode: {
          on: 'Modalità vocale attiva',
          off: 'Modalità vocale disattivata',
        },
        dictation: {
          start: 'Detta',
          stop: 'Ferma dettatura',
          listening: 'In ascolto…',
          transcribing: 'Trascrizione…',
          error: 'Nessuna trascrizione. Riprova o allega un file audio.',
        },
      },
      ...displayIt,
      ...shareIt,
      ...documentsIt,
      ...governanceIt,
      ...graphIt,
      ...settingsIt,
      ...adminIt,
      ...onboardingIt,
      conversations: {
        new: 'Nuova chat',
        newPending: 'Creazione chat...',
        heading: 'Conversazioni',
        loading: 'Caricamento conversazioni...',
        loadError: 'Impossibile caricare le conversazioni. Aggiorna la lista.',
        untitled: 'Senza titolo',
        includeArchived: 'Mostra archiviate',
        archivedTag: 'Archiviata',
        renameLabel: 'Titolo conversazione',
        empty: {
          heading: 'Avvia una esecuzione',
          body: 'Fai una domanda ad Aura per iniziare. Le tue conversazioni appariranno qui.',
        },
        recency: {
          today: 'Oggi',
          yesterday: 'Ieri',
          last7: 'Ultimi 7 giorni',
          older: 'Meno recenti',
        },
        actions: {
          more: 'Azioni conversazione',
          rename: 'Rinomina',
          archive: 'Archivia',
          unarchive: 'Ripristina',
          delete: 'Elimina definitivamente',
        },
        delete: {
          title: 'Eliminare la conversazione?',
          body: 'Questo elimina definitivamente "{{title}}" e i suoi messaggi. Non è reversibile.',
          confirm: 'Elimina definitivamente',
          cancel: 'Conserva la conversazione',
        },
        search: {
          label: 'Cerca nelle conversazioni',
          placeholder: 'Cerca nelle conversazioni',
          searching: 'Ricerca in corso...',
          empty: {
            heading: 'Nessun risultato',
            body: 'Nessuna conversazione contiene "{{query}}". Prova un altro termine.',
          },
        },
      },
      approval: {
        badge: {
          aria_one: '{{count}} approvazione in attesa',
          aria_other: '{{count}} approvazioni in attesa',
          cleared: 'Nessuna approvazione in attesa.',
        },
        list: {
          label: 'Approvazioni in attesa',
          open: 'Apri',
          empty: 'Niente che richieda la tua attenzione.',
          untitled: 'Senza titolo',
        },
        kind: {
          clarification: 'Chiarimento',
          choice: 'Scelta',
          approval: 'Approvazione',
        },
        frame: {
          approval: 'Approvazione richiesta',
          input: 'Input richiesto',
          review: 'Controlla ambito e conseguenze prima di continuare.',
        },
        lock: 'Rispondi alla richiesta qui sopra per continuare.',
        card: {
          freeText: 'La tua risposta',
          freeTextPlaceholder: 'Scrivi la tua risposta',
          answer: 'Rispondi',
          decline: 'Rifiuta',
          cancel: 'Annulla esecuzione',
          declined: 'Rifiutata.',
          answered: 'Risposta inviata.',
          cancelled: 'Esecuzione annullata.',
          confirmCancel: 'Fermare questa esecuzione?',
          confirmCancelYes: 'Ferma esecuzione',
          confirmCancelNo: 'Continua',
          error:
            'Impossibile riprendere questa esecuzione. Potrebbe essere già stata gestita o annullata.',
        },
        terminal: {
          expired: 'Scaduta — risolta automaticamente.',
        },
      },
      ...footerIt,
      skeleton: {
        shell: 'Caricamento cockpit...',
        login: 'Caricamento accesso...',
        page: 'Caricamento pagina...',
      },
      health: {
        title: 'Stato runtime',
        loading: 'Controllo runtime...',
        unreachable:
          'Impossibile leggere lo stato runtime. Gli endpoint di health non rispondono: il server potrebbe essere in avvio o non attivo.',
        labels: {
          liveness: 'Vitalità',
          readiness: 'Prontezza',
          postgres: 'Postgres',
          neo4j: 'Neo4j',
          bindAddress: 'Indirizzo bind',
          build: 'Build',
        },
        status: {
          unavailable: 'Non disponibile',
          live: 'Attivo',
          ready: 'Pronto',
          degraded: 'Degradato',
          unknown: 'Sconosciuto',
        },
        lastChecked: 'Ultimo controllo {{time}}',
        relative: {
          never: 'mai',
          justNow: 'ora',
          secondsAgo: '{{count}}s fa',
          minutesAgo: '{{count}}m fa',
        },
      },
      notFound: {
        title: 'Pagina non trovata',
        beforeLink: 'Questa pagina cockpit non esiste.',
        link: 'Torna alla dashboard',
      },
      errorBoundary: {
        title: 'Aura non può mostrare questa vista.',
        action: 'Ricarica per riprovare.',
      },
    },
  },
} as const;

export type AppLanguage = keyof typeof resources;
