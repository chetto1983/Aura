# Usare il piano ChatGPT in Aura

In **Impostazioni → Instradamento modello**, scegli **ChatGPT**, premi **Continua
con ChatGPT**. Aura apre una finestra con la pagina ufficiale OpenAI nel browser
del tuo ambiente Aura, anche quando Aura gira su una VM. Completa lì l'accesso e
il consenso all'uso del piano. La finestra si chiude al completamento e Aura
carica i modelli disponibili per quell'account. Scegli il modello e salva: la nuova
rotta si applica alle richieste successive senza riavviare il daemon.

Il collegamento appartiene alla tua identità Aura. Gli altri utenti collegano il
proprio account dalla sezione personale delle Impostazioni; cambiare la rotta e il
modello del deployment richiede i permessi amministrativi già previsti da Aura.

Non serve una chiave API OpenAI. L'accesso all'identità OpenAI e il consenso a usare
il piano sono distinti: quando il piano non è abilitato, premi nuovamente il
pulsante di accesso per concedere il permesso. **Ricollega ChatGPT** recupera una
sessione scaduta. **Scollega ChatGPT** cancella i token locali e tenta la revoca
remota; se questa fallisce, controlla anche le connessioni nelle impostazioni ChatGPT.

Se il browser blocca la finestra, usa **Apri l’accesso a ChatGPT**. Chiuderla annulla
quel tentativo senza scollegare un account già attivo. Un accesso scade dopo dieci
minuti; puoi riprovare dallo stesso pulsante. Le credenziali digitate nel browser
non passano attraverso il modello.

## Installazione e credenziali

Il collegamento riusa il sandbox per utente e la visualizzazione browser già presenti
in Aura. Non richiede modifiche a Compose, porte pubbliche aggiuntive o tunnel SSH.
Il listener del callback gira accanto al browser nel sandbox, perciò il callback
`http://127.0.0.1:<porta>/auth/callback` richiesto dal [contratto OpenAI](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
raggiunge l'ambiente Aura corretto. Lo stato casuale lega la risposta al tentativo
e all'utente che lo ha avviato. Aura chiude il browser temporaneo e rimuove il suo
profilo al termine o all'annullamento.

Il servizio usa `AURA_AUTHULA_SECRET` per cifrare le credenziali nella directory
`chatgpt-plan`, accanto a `AURA_SKILLS_DIR`. Nella configurazione Compose questa
directory appartiene al volume persistente `aura-home`. Mantieni quel volume e il
segreto nei backup; non copiare i token nel frontend o in una variabile API key.

## Modelli, budget e utilizzo

Il selettore usa il catalogo dell'account e il suo ordine, senza una lista di modelli
fissata nel codice. La rotta è l'endpoint ufficiale `https://api.openai.com/v1` con
Responses streaming, contesto completo, `store=false` e strumenti Aura eseguiti
localmente. Una risposta interrotta o priva dell'evento di completamento è un errore.

Nel compositore, il selettore del ragionamento mostra i livelli dichiarati dal modello
scelto. **Auto** applica la politica di Aura; un livello esplicito viene inviato a
ChatGPT. **Off** compare soltanto quando il modello consente di disattivare il
ragionamento. Aura visualizza il riepilogo restituito da OpenAI nelle risposte che lo
forniscono, se `AURA_SHOW_REASONING` è abilitato.

Quando il catalogo non pubblica una finestra di contesto, Aura usa un budget di
lavoro prudente di 32.768 token: questo numero è un limite di Aura, non una capacità
dichiarata del modello. I budget espliciti dell'operatore restano configurabili.
Non vengono inventati prezzi per token o importi a consumo: si applicano i limiti
del piano ChatGPT. Le altre funzionalità che già usano OpenRouter, come la
generazione multimediale, mantengono la loro configurazione.

## Contratto e verifica

Il contratto ufficiale è documentato in [Sign in with ChatGPT per applicazioni
open source](https://developers.openai.com/siwc/token-sharing-open-source),
[modelli e inferenza](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
e [recupero degli errori](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery).

Le misure di questa implementazione, inclusi i limiti delle prove, sono registrate
nel [rapporto di validazione](../.planning/quick/261002-pfr-add-chatgpt-plan-sign-in-and-account-mod/261002-pfr-VALIDATION.md).
La disponibilità del piano e dei singoli modelli viene stabilita dall'account
OpenAI al momento del collegamento.
