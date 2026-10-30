import { conversationsEn, conversationsIt } from './resources.conversations';
import { adminEn, adminIt } from './resources.admin';
import { chatReasoningEn, chatReasoningIt, chatToolEn, chatToolIt } from './resources.chatactivity';
import { messageDraftEn, messageDraftIt } from './resources.messageDraft';
import {
  chatCompactionEn,
  chatCompactionIt,
  composerEffortEn,
  composerEffortIt,
  composerGaragePickerEn,
  composerGaragePickerIt,
  composerSkillPickerEn,
  composerSkillPickerIt,
} from './resources.composer';
import { displayEn, displayIt } from './resources.display';
import { browserLiveEn, browserLiveIt } from './resources.browserLive';
import { footerEn, footerIt } from './resources.footer';
import { approvalEn, approvalIt } from './resources.approval';
import { filesEn, filesIt } from './resources.files';
import { documentsEn, documentsIt } from './resources.documents';
import { boardEn, boardIt } from './resources.board';
import { chatEmptyEn, chatEmptyIt } from './resources.empty';
import { governanceEn, governanceIt } from './resources.governance';
import { graphEn, graphIt } from './resources.graph';
import { loginEn, loginIt } from './resources.login';
import { mediaEn, mediaIt } from './resources.media';
import { onboardingEn, onboardingIt } from './resources.onboarding';
import { profileEn, profileIt, settingsEn, settingsIt } from './resources.settings';
import { embeddingRouteEn, embeddingRouteIt } from './resources.embeddingRoute';
import { remoteAccessEn, remoteAccessIt } from './resources.remoteAccess';
import { shareEn, shareIt } from './resources.share';
import { studioEn, studioIt } from './resources.studio';
import { chatTurnNoticesEn, chatTurnNoticesIt } from './resources.turnnotices';
import { updateEn, updateIt } from './resources.update';
import { questionCardEn, questionCardIt } from './resources.questions';

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
      stepStrip: {
        progress: 'Step {{current}} of {{total}}',
        done: 'done',
        failed: 'needs attention',
      },
      ...loginEn,
      messageDraft: messageDraftEn,
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
        resizeWorker: 'Resize agent activity panel',
        resizeSections: 'Resize the sections rail',
        openRuntime: 'Open runtime status',
        closePanel: 'Close panel',
        logout: 'Sign out',
        modes: {
          chat: 'Chat',
          board: 'Board',
          studio: 'Studio',
          graph: 'Graph',
          governance: 'Governance',
          documents: 'Documents',
          settings: 'Settings',
        },
        modesCompact: {
          chat: 'Chat',
          board: 'Board',
          studio: 'Studio',
          graph: 'Graph',
          governance: 'Gov',
          documents: 'Docs',
          settings: 'Config',
        },
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
        garagePicker: composerGaragePickerEn,
        compaction: chatCompactionEn,
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
        },
        running: 'Running...',
        empty: chatEmptyEn,
        liveRun: {
          hint: 'A run is still in progress. Type to redirect it, or tap Stop.',
        },
        ...chatTurnNoticesEn,
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
        reasoning: chatReasoningEn,
        tool: chatToolEn,
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
        voice: {
          title: 'Voice mode',
          you: 'You:',
          send: 'Send now',
          retry: 'Try again',
          close: 'Close',
          hint: 'Speak, then pause — what you said is sent as text. Talk over the answer to interrupt it.',
          phase: {
            opening: 'Opening the microphone…',
            listening: 'Listening…',
            transcribing: 'Transcribing…',
            thinking: 'Thinking…',
            speaking: 'Answering…',
            error: 'Voice mode stopped.',
          },
          error: {
            mic: 'No microphone. Grant access and try again.',
            stt: "Couldn't transcribe that. Try again.",
            tts: "Couldn't play the answer. It is in the conversation.",
          },
        },
        dictation: {
          start: 'Dictate',
          stop: 'Stop dictation',
          listening: 'Listening…',
          transcribing: 'Transcribing…',
          error: 'No transcription. Try again.',
        },
      },
      ...displayEn,
      ...browserLiveEn,
      ...mediaEn,
      ...studioEn,
      ...shareEn,
      ...filesEn,
      ...documentsEn,
      ...boardEn,
      ...governanceEn,
      ...graphEn,
      ...settingsEn,
      ...embeddingRouteEn,
      ...remoteAccessEn,
      ...updateEn,
      ...questionCardEn,
      ...profileEn,
      ...adminEn,
      ...onboardingEn,
      ...conversationsEn,
      ...approvalEn,
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
          memory: 'Memory',
          bindAddress: 'Bind address',
          build: 'Build',
          commit: 'Commit',
          buildDate: 'Built',
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
      stepStrip: {
        progress: 'Passo {{current}} di {{total}}',
        done: 'fatto',
        failed: 'da sistemare',
      },
      ...loginIt,
      messageDraft: messageDraftIt,
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
        resizeWorker: 'Ridimensiona pannello agenti',
        resizeSections: 'Ridimensiona la barra sezioni',
        openRuntime: 'Apri stato runtime',
        closePanel: 'Chiudi pannello',
        logout: 'Disconnetti',
        modes: {
          chat: 'Chat',
          board: 'Bacheca',
          studio: 'Studio',
          graph: 'Grafo',
          governance: 'Governance',
          documents: 'Documenti',
          settings: 'Impostazioni',
        },
        modesCompact: {
          chat: 'Chat',
          board: 'Bach.',
          studio: 'Studio',
          graph: 'Grafo',
          governance: 'Gov',
          documents: 'Doc',
          settings: 'Imp.',
        },
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
        garagePicker: composerGaragePickerIt,
        compaction: chatCompactionIt,
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
        },
        running: 'In esecuzione...',
        empty: chatEmptyIt,
        liveRun: {
          hint: "Un'esecuzione è ancora in corso. Scrivi per reindirizzarla, oppure premi Ferma.",
        },
        ...chatTurnNoticesIt,
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
        reasoning: chatReasoningIt,
        tool: chatToolIt,
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
        voice: {
          title: 'Modalità vocale',
          you: 'Tu:',
          send: 'Invia ora',
          retry: 'Riprova',
          close: 'Chiudi',
          hint: 'Parla, poi fai una pausa: quello che dici viene inviato come testo. Parla sopra la risposta per interromperla.',
          phase: {
            opening: 'Apertura del microfono…',
            listening: 'In ascolto…',
            transcribing: 'Trascrizione…',
            thinking: 'Sto pensando…',
            speaking: 'Sto rispondendo…',
            error: 'Modalità vocale interrotta.',
          },
          error: {
            mic: 'Microfono non disponibile. Concedi l’accesso e riprova.',
            stt: 'Non sono riuscito a trascrivere. Riprova.',
            tts: 'Non sono riuscito a riprodurre la risposta. È nella conversazione.',
          },
        },
        dictation: {
          start: 'Detta',
          stop: 'Ferma dettatura',
          listening: 'In ascolto…',
          transcribing: 'Trascrizione…',
          error: 'Nessuna trascrizione. Riprova.',
        },
      },
      ...displayIt,
      ...browserLiveIt,
      ...mediaIt,
      ...studioIt,
      ...shareIt,
      ...filesIt,
      ...documentsIt,
      ...boardIt,
      ...governanceIt,
      ...graphIt,
      ...settingsIt,
      ...embeddingRouteIt,
      ...remoteAccessIt,
      ...updateIt,
      ...questionCardIt,
      ...profileIt,
      ...adminIt,
      ...onboardingIt,
      ...conversationsIt,
      ...approvalIt,
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
          memory: 'Memoria',
          bindAddress: 'Indirizzo bind',
          build: 'Build',
          commit: 'Commit',
          buildDate: 'Build date',
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
