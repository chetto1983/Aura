# PRD — Aura, assistente di lavoro per mail, agenda e CRM

**Versione 0.4 · 23 novembre 2026 · Integrazione Contatti Spark · Da valutare**

La specifica assume Gmail e Google Calendar collegati ad Aura per **andrea.piermarino.sacchi@gmail.com**, come indicato da Andrea. Capacità e permessi effettivi dei connettori, disponibilità dello schedulatore e copertura dei dati vanno verificati prima del pilota. Il progetto riguarda Aura e un proprio archivio operativo.

## 1. Obiettivo

Aura legge le informazioni di lavoro, prepara risposte, individua il prossimo passo e tiene sotto controllo le scadenze. Andrea interviene per approvare, correggere o decidere. Continua a inviare personalmente email, incarichi, solleciti e richieste a clienti, colleghi, responsabili e fornitori.

L'Assistente deve rispondere a tre domande con dati verificabili.

- **Cosa devo decidere oggi?** Bozze pronte, appuntamenti da approvare e scadenze che richiedono attenzione.
- **Chi mi deve ancora rispondere?** Persona, richiesta, termine, riscontri parziali e azione proposta.
- **Quali rapporti abbiamo avuto con questo cliente?** Scambi effettivi, persone, argomenti, esiti, visite e riunioni, impegni futuri e fonti consultabili.

## 2. Skill e attività ricorrenti

La **skill** stabilisce come leggere le fonti e quale proposta preparare. Aura ha bisogno di eseguire le attività in background, anche a chat chiusa. Un controllo ricorrente della posta serve se il connettore non fornisce eventi utilizzabili. In entrambi i casi, promemoria e riconciliazione richiedono una gestione persistente.

| Funzione | Scelta proposta | Recupero |
| --- | --- | --- |
| Acquisizione Gmail | Usare gli eventi del connettore Aura se disponibili e affidabili. Altrimenti, un solo controllo incrementale ogni **5 minuti** per casella nel pilota. | Salvare il cursore, deduplicare per casella e ID e recuperare gli aggiornamenti entro il perimetro approvato. |
| Chat Aura e Telegram | Acquisire al ricevimento nei canali privati abilitati. | Riconoscere messaggi ripetuti e restituire l'esito già registrato. |
| Appuntamenti e promemoria | Una coda persistente registra l'ora dovuta. Il servizio di consegna esegue e verifica l'azione. | Recuperare lavori saltati dopo un riavvio. Rendere visibile un esito incerto prima di ripetere un invio. |
| Riconciliazione | Controllo periodico di permessi, cursori e integrazioni, comprese le modifiche manuali su Calendar. | Esporre l'ultima sincronizzazione riuscita e l'impatto sui risultati. |

La lettura della posta e l'orario dei promemoria sono indipendenti. Gmail push resta un'evoluzione se serve più tempestività. Richiede Pub/Sub, rinnovo di `watch` entro sette giorni e recupero tramite `history.list`, anche quando una notifica si perde. Usare un solo percorso principale per casella e un controllo di riconciliazione a frequenza inferiore. [Sincronizzazione Gmail](https://developers.google.com/workspace/gmail/api/guides/sync), [Notifiche Gmail](https://developers.google.com/workspace/gmail/api/guides/push)

~~~mermaid
flowchart LR
  A[Gmail, Calendar, chat Aura e Telegram] --> B[Acquisizione e collegamento alla pratica]
  B --> C[Valutazione delle fonti]
  C --> D[Bozza Gmail e avviso ad Andrea]
  C --> E[Proposta di attesa o appuntamento]
  E --> F[Conferma o rettifica di Andrea]
  F --> G[Promemoria e Calendar]
  B --> H[Storico cliente con fonti]
  G --> H
  H --> I[Risposte e report in Aura]
~~~

## 3. Specifica della skill — Metodo di Andrea

### Percorso ammesso delle email

1. **Casella di lavoro.** Le comunicazioni dei clienti, dei colleghi e dei fornitori arrivano ad **andrea.piermarino@sacchi.it**, direttamente o in copia. Andrea sceglie quali sottoporre ad Aura.
2. **Inoltro scelto da Andrea.** Le mail sottoposte ad Aura arrivano a **andrea.piermarino.sacchi@gmail.com** con **andrea.piermarino@sacchi.it** come mittente esterno dell'inoltro. Il controllo usa l'indirizzo nei metadati Gmail e i segnali di autenticazione disponibili. In caso di autenticità incerta, l'inoltro resta da verificare prima dell'analisi operativa.
3. **Risposta dell'assistente.** La bozza rimane in **Bozze Gmail**. Le proposte e i promemoria sono rivolti ad Andrea nella chat Aura o nel bot Telegram privato. L'eventuale email, compresi inoltro e sollecito, parte solo su invio manuale di Andrea.

### Regole di lettura e valutazione

1. **Ricostruire la catena.** Separare l'inoltro di Andrea dalla comunicazione arrivata alla casella di lavoro. Identificare chi ha scritto ad Andrea, chi aveva scritto prima a quella persona e, quando documentato, chi ha avviato la richiesta. Per ciascun passaggio conservare autore, data, oggetto, richiesta, ruoli dei destinatari e fonte. Se la catena è incompleta, riportare i passaggi disponibili e il punto da verificare. Collegare cliente e codice con un'evidenza, conservando società collegate come entità distinte.
2. **Attribuire responsabilità e motivo.** Riconoscere richieste, ordini, offerte, preventivi, disponibilità, alternative prodotto, problemi, prospect, proposte commerciali, corsi e appuntamenti. Capire chi gestisce già la pratica, chi deve decidere e a chi chiedere il prossimo passo. Considerare sia il campo To/CC sia il testo. Se Andrea è in copia e un collega sta già gestendo la richiesta, registrare la pratica per consultazione. Una richiesta esplicita rivolta ad Andrea viene valutata anche quando è in copia. Un destinatario diretto può comunque ricevere una semplice informazione.
3. **Leggere le fonti.** Aprire PDF, Word ed Excel autorizzati e confrontare versioni e condizioni. Controllare codici, quantità, unità, valuta, IVA, validità e disponibilità. Citare pagina, tabella, scheda o riga. Segnalare documenti protetti, scansioni ambigue, incongruenze e allegati mancanti prima di usare i valori nella bozza.
4. **Consegnare un risultato utilizzabile.** Restituire sintesi, evidenze, ruoli, azione proposta, bozza e scadenza verificabile. Proporre il testo di eventuali incarichi o richieste a colleghi, tecnici, responsabili o fornitori. Quando mancano dati, preparare una richiesta di chiarimento o indicare cosa verificare.
5. **Adattarsi al metodo di Andrea.** Usare correzioni, istruzioni, scelte esplicite e, quando documentate, differenze tra la bozza proposta e il testo effettivamente inviato. Imparare come Andrea scrive, decide a chi chiedere, stabilisce una priorità e sceglie il prossimo passo. Le correzioni migliorano la pratica corrente. Per generalizzare, Aura propone una regola con ambito, fonte ed esempio. Andrea, titolare del prodotto, la approva, modifica o ritira. Le regole approvate sono versionate e consultabili.

La skill rende strutturati cliente, pratica, responsabilità, ragionamento sintetico, fonti, azioni, scadenze e incertezze. Il sistema verifica separatamente permessi e campi prima di ogni scrittura. Email, allegati e messaggi di terzi forniscono evidenze. Le autorizzazioni provengono dai comandi verificati di Andrea e dalle sue regole approvate.

| Autonomia prevista | Azioni |
| --- | --- |
| Automatica entro il perimetro approvato | Leggere, associare con evidenza certa, confrontare, creare una bozza iniziale, avvisare Andrea, aggiornare le attese con evidenza sufficiente e produrre riepiloghi. Aggiornare Contatti Spark per integrazioni univoche quando la relativa regola è stata autorizzata. |
| Su decisione di Andrea | Risolvere collegamenti o modifiche anagrafiche ambigue, rendere permanente una preferenza e autorizzare puntualmente eventuali scritture in agenda. Aura mostra prima l'azione e il suo effetto. |
| Manuale in Gmail | Inviare ai destinatari effettivi, inoltrare, assegnare incarichi a terzi, inviare solleciti e assumere impegni commerciali. |

Se serve un nuovo strumento, Aura indica quale passaggio lo richiede, il risultato atteso, i permessi e gli eventuali costi. Può proporre di adottarlo o implementarlo. L'attivazione, l'accesso a nuove fonti e la modifica delle integrazioni richiedono una decisione specifica di Andrea.

### Esempio — Robino e Galandrino

1. Un referente di **Robino e Galandrino**, codice di esempio **XXX**, scrive ad **alessandro.congiu@sacchi.it** per verificare prezzo e disponibilità. Alessandro scrive ad Andrea direttamente o in copia. Andrea inoltra la catena da **andrea.piermarino@sacchi.it** alla propria Gmail di gestione.
2. Aura registra il cliente come origine della richiesta, Alessandro come interlocutore interno e Andrea come destinatario diretto o in copia in base alla fonte. Verifica se Alessandro sta già trattando la richiesta, se chiede un intervento di Andrea e quali dati mancano.
3. Se serve intervenire, Aura propone il prossimo passo. Può preparare in Gmail una bozza per chiedere ad Alessandro condizioni e tempi, suggerire una verifica a un tecnico o preparare una proposta di appuntamento. Aura avvisa Andrea su Telegram. Andrea sceglie, modifica e invia personalmente se necessario.
4. Una nuova comunicazione inoltrata sullo stesso argomento aggiorna la pratica. Se conferma il prezzo ma manca la disponibilità, Aura indica il dato ottenuto, cosa manca, chi deve rispondere e l'eventuale termine. Alla scadenza avvisa Andrea e offre, se utile, una bozza di sollecito. Quando arrivano tutte le risposte, propone l'esito con le relative fonti.

## 4. Flussi operativi

### Gmail in entrata e bozze

- Aura classifica gli inoltri ammessi e prepara una bozza **nella cartella Bozze di Gmail** se la pratica richiede una risposta o una richiesta da sottoporre ad Andrea. Quando il salvataggio è verificato, avvisa Andrea su Telegram con oggetto sintetico e riferimento alla bozza. Un collegamento diretto compare se funziona sul dispositivo.
- Una stessa richiesta riusa la bozza identificata. Una modifica manuale di Andrea ha priorità. Una nuova analisi presenta una revisione separata se il contenuto è cambiato. Il collaudo deve provare modifica simultanea, riscrittura dopo riavvio e duplicazione di eventi.
- L'adattatore espone lettura e operazioni di bozza approvate. Blocca i comandi di invio, inoltro e cancellazione, anche quando l'ambito OAuth concede operazioni più ampie. Le credenziali restano fuori dalla disponibilità del modello. [Bozze Gmail](https://developers.google.com/workspace/gmail/api/guides/drafts), [Ambiti OAuth di Gmail](https://developers.google.com/workspace/gmail/api/auth/scopes)

### Posta inviata, risposte attese e solleciti

- Per un invio dalla casella Sacchi, il pilota usa l'originale inoltrato attraverso il percorso ammesso e l'attestazione di Andrea associata a quell'invio. Se Andrea invia manualmente dalla Gmail già collegata, Aura può usare il messaggio in **Posta inviata** quando il connettore lo rende visibile e il collegamento alla pratica è verificabile. Fino a quel riscontro, la bozza resta una proposta e l'attesa resta da verificare.
- Aura apre un'attesa per destinatario e richiesta effettivamente formulata, dopo l'invio documentato e secondo una regola approvata. Una scadenza esatta prevale. Per «entro due giorni», proporre **due giorni lavorativi, lunedì–venerdì**. Festivi e ora limite sono decisioni da definire. Se il termine manca o è ambiguo, l'attesa resta **Scadenza da completare**. Il promemoria puntuale parte solo con una data e un'ora verificabili.
- Collegare la risposta usando riferimenti della conversazione, identità del mittente, cronologia e contenuto. Un sì parziale chiude solo i punti risolti. Una ricevuta automatica, una mancata consegna o un messaggio ambiguo mantengono uno stato esplicito. Quando manca un riscontro a scadenza, Aura avvisa Andrea e può preparare una bozza di sollecito.
- La preparazione della bozza lascia la pratica disponibile per gli aggiornamenti. A ogni nuovo inoltro correlato Aura ricalcola i fatti acquisiti, le informazioni mancanti, la persona da cui si attende una risposta e il prossimo passo. Collega la nuova evidenza alla pratica esistente quando il riferimento è certo. Per un aggiornamento sostanziale propone una revisione della bozza già modificata da Andrea. I solleciti di Aura sono promemoria privati per Andrea o bozze da usare manualmente.
- I promemoria personali inseriti da chat, Telegram o agenda hanno un'origine e un'eventuale pratica. Restano distinti dalle risposte attese da terzi.

### Appuntamenti da email, chat Aura e Telegram

| Stato | Comportamento |
| --- | --- |
| Proposto | Mostra cliente e codice se pertinenti, data, ora italiana, durata, luogo o link, partecipanti attesi e fonte. Chiede solo i dati necessari a sciogliere un dubbio. |
| In sospeso | Dopo autorizzazione puntuale di Andrea alla scrittura, Aura crea una riserva privata nel calendario di lavoro. Registra lo stato, l'ID e la verifica da effettuare. |
| Confermato | Aura rileva la conferma dell'interessato e propone l'aggiornamento della stessa riserva. L'approvazione di Andrea autorizza la sincronizzazione dell'evento e del promemoria. |
| Spostato o annullato | Mostra il cambiamento e la fonte. Dopo approvazione aggiorna l'evento esistente e annulla i promemoria superati. |
| Da consuntivare, effettuato o non svolto | Alla data trascorsa segue una verifica dell'esito. Il consuntivo registra chi è stato incontrato e gli eventuali affiancamenti. |

Proposta iniziale per gli avvisi. Appuntamento in sospeso **almeno tre giorni prima**, con avviso alla prima occasione utile se il termine è già vicino. Appuntamento confermato con promemoria Calendar **due ore prima**. Aura controlla i conflitti e gestisce gli appuntamenti del calendario designato. Le riserve sono eventi privati senza partecipanti esterni. Gli inviti sono una funzione separata. [Creazione eventi in Google Calendar](https://developers.google.com/workspace/calendar/api/v3/reference/events/insert)

### Chat Aura e bot Telegram

Chat e bot condividono lo stato della pratica. Un comando come «Ricordami di chiamare il cliente domani alle 10» può essere salvato direttamente se Andrea approva quella regola. Per appuntamenti e variazioni, **Conferma**, **Modifica** e **Archivia proposta** operano sulla proposta e sulla versione mostrate. Una conferma precedente scade se il contenuto cambia. Conservare l'identità del destinatario, evitare bozze integrali nei messaggi e provare la ricezione sul telefono. Per Telegram, usare il meccanismo già autorizzato, webhook o polling, e deduplicare per `update_id`. [Telegram Bot API](https://core.telegram.org/bots/api)

Quando Andrea apre o usa la chat, Aura riporta le decisioni e gli impegni più urgenti. A chat chiusa, il bot consegna gli avvisi previsti e può raggruppare le proposte ordinarie in un riepilogo. Le scadenze puntuali seguono l'orario salvato. La stessa notifica ha un riferimento unico, anche se l'elemento rimane visibile in chat.

## 5. CRM e report

### Anagrafica permanente — Documenti → Contatti Spark

Andrea indica che nella sezione **Documenti** di Aura esiste il documento **Contatti Spark**. Il progetto lo usa come anagrafica operativa da consultare e aggiornare progressivamente. Il CRM collega alla stessa anagrafica le interazioni, le attività e le fonti dello storico. Prima del pilota occorre verificare il formato del documento e le operazioni di lettura e scrittura disponibili.

| Dati da aggiornare | Evidenza richiesta |
| --- | --- |
| Cliente, codice con zeri iniziali, società e gruppo | Codice o associazione verificabile nella fonte. Le società collegate conservano i propri codici. |
| Referente, indirizzo professionale, ruolo, recapito di lavoro se pertinente | Identità e rapporto con la società verificabili. Un partecipante in copia o invitato resta distinto da chi ha avuto un effettivo scambio. |
| Relazione tra referente, società o gruppo | Tipo di relazione documentato, fonte e data. La presenza nella stessa conversazione è un indizio da verificare. |

- Da ogni mail ammessa, conversazione nella chat Aura, evento Calendar o messaggio del bot Telegram autorizzato, Aura estrae i nuovi dati pertinenti e cerca la scheda esistente. Registra per ogni aggiunta la fonte, il canale, la data e lo stato della verifica. Conserva i soli dati professionali utili alla pratica.
- Aura integra i campi univoci secondo la regola approvata da Andrea e rilegge la scheda prima di salvarla. Il ripetersi della stessa evidenza riusa il riferimento già registrato. I dati verificati hanno priorità sulle proposte. Per incongruenze, omonimi, fusioni e cambi di società o codice, Aura presenta ad Andrea le fonti a confronto e attende la sua decisione.
- Nella chat Aura e nel bot privato, la risposta cerca prima in **Contatti Spark** e collega la scheda allo storico del CRM. Può riportare chi è il referente, la società, il codice, la correlazione, l'ultimo scambio e la fonte dell'aggiornamento. Se esistono più corrispondenze, chiede quale intendi. Le informazioni ancora da verificare sono indicate come tali.
- L'efficacia deve crescere con le evidenze confermate. Nel pilota misurare quante richieste sui contatti trovano una risposta verificabile, quante richiedono disambiguazione e quanti aggiornamenti Andrea deve correggere.

| Oggetto | Dati essenziali |
| --- | --- |
| Pratica | Cliente, codice, argomento, motivo, persone, stato, fonti |
| Interazione | Canale, data effettiva o data di acquisizione distinta, interlocutori, ruoli, esito e traccia dell'evidenza |
| Attesa o impegno | Responsabile, richiesta, termine, esito completo o parziale e prossima azione |
| Visita o incontro | Partecipanti previsti e persone effettivamente incontrate, luogo e affiancamento con persona, ruolo e brand del fornitore quando serve |
| Proposta e decisione | Fonte, versione, approvazione, operazione eseguita ed eventuale problema |

Una mail ricevuta, un invio effettivo, una riunione in agenda e un incontro avvenuto hanno conteggi distinti. Un duplicato su più canali viene unito quando i riferimenti lo dimostrano. **Teams** può partire da un consuntivo di Andrea o da un'evidenza già autorizzata. Una futura integrazione Graph può importare dati di presenza quando il tenant e i permessi lo consentono. Un invito con link Teams mantiene l'esito da verificare. [Report presenze Teams](https://learn.microsoft.com/en-us/graph/api/meetingattendancereport-list?view=graph-rest-1.0), [Record di presenza](https://learn.microsoft.com/en-us/graph/api/attendancerecord-list?view=graph-rest-1.0)

**HubSpot è opzionale.** Introdurlo se serve un'anagrafica condivisa o se semplifica un flusso concreto. Valutare prima la lettura mirata dei clienti e dei referenti. Eventuali scritture richiedono la scelta esplicita del portale, una regola contro i duplicati e prove su record sintetici.

### Consultazione e report

**Report in Aura.** Risposta breve in testa, con periodo e copertura. Seguono schede o tabella con cliente, codice, interlocutore, ultima evidenza, stato, prossima azione e fonte apribile. Esempi.

- «Chi mi deve ancora rispondere su prezzi e disponibilità?»
- «Riepiloga il mese di questo cliente e dimmi con chi ho parlato.»
- «Quali clienti non ho sentito o visitato negli ultimi tre mesi?»
- «Quali appuntamenti delle prossime due settimane devo ancora confermare?»
- «Chi è il referente di Robino e Galandrino, qual è il codice cliente e con quale società è collegato?»

Per l'inattività, Aura indica quali clienti risultano senza evidenze registrate nel periodo, l'ultima attività, i dati incerti e da quando le fonti sono coperte. Le viste su telefono usano schede compatte con dettagli espandibili.

## 6. Punti di forza, fragilità e migliorie

| Punto di forza | Fragilità concreta | Miglioria proposta |
| --- | --- | --- |
| Bozze native nell'app che Andrea usa già | Permessi Gmail più ampi, modifiche manuali, riscritture involontarie | Limitare le azioni dell'adattatore, riconciliare ID e versione, provare modifiche simultanee. |
| Un solo storico per mail, chat e agenda | Omonimi, CC, inoltri duplicati, fonti incomplete e invii da caselle diverse | Usare codice e identificatori stabili. Classificare le evidenze e mostrare le lacune nel report. |
| Contatti Spark si arricchisce durante il lavoro | Una firma, un invito o una citazione può suggerire un legame errato | Richiedere una fonte per ogni aggiunta, gestire le correzioni versionate e provare la ricerca nella chat e nel bot. |
| Lettura degli allegati con confronti | Scansioni, unità discordanti, riservatezza e istruzioni incorporate nei file | Isolare l'estrazione, citare pagina e riga, usare soltanto formati, canali e modelli approvati. |
| Avvisi con la chat chiusa | Riavvii, permessi scaduti, ritardi del bot o identità ambigua | Coda persistente, riconciliazione, verifica del destinatario univoco e collaudo su telefono. |
| Agenda aggiornata dalla conversazione | Temporanei scaduti, conferme ambigue, cambi di ora e conflitti | Stati espliciti, approvazione versionata, ID evento stabile e controllo di concorrenza. |
| Apprendimento del metodo | Un caso isolato può diventare una regola sbagliata | Richiedere approvazione delle preferenze generali e consentire revisione e disattivazione. |

**Riduzione di token e tempi.** Filtrare e deduplicare prima di analizzare. Rileggere solo le modifiche della conversazione e riusare l'estrazione degli allegati per impronta. Usare query e regole per stati, date, conteggi e report. Riservare il modello alla comprensione, ai confronti complessi e alla scrittura delle bozze. Misurare latenza, chiamate, token per pratica e tasso di correzione rispetto a regole semplici. Il PRD definisce il metodo di misura. Costi e volumi effettivi dipendono dall'installazione.

## 7. Rilasci e collaudo

1. **Gmail e Telegram.** Provare la catena cliente → collega → Andrea → Gmail, sia con Andrea in copia sia con richiesta diretta, e una catena con il primo mittente mancante. Un messaggio da altro mittente resta fuori dal flusso. Una richiesta operativa produce una sola bozza, modificabile in Gmail, e un avviso sul telefono. Il replay conserva la modifica. Verificare il blocco delle funzioni di invio.
2. **Attese e documenti.** Un invio documentato a due colleghi genera due attese distinte. Un nuovo inoltro sullo stesso argomento aggiorna solo quella pertinente. Un riscontro parziale mantiene aperti i dati mancanti. Alla scadenza arriva ad Andrea il promemoria, con bozza di sollecito se utile. Il confronto fra PDF, Word ed Excel riporta i valori, la fonte e le differenze verificabili.
3. **Agenda.** Un'approvazione crea una sola riserva. La conferma successiva aggiorna lo stesso evento. Verificare rettifiche manuali, annulli, ora legale, promemoria e recupero dopo un'interruzione.
4. **CRM e report.** Verificare clienti omonimi con codici diversi, società correlate, invii confermati, visite effettuate e imminenti. Due acquisizioni dello stesso incontro producono un solo fatto quando la correlazione è certa. Nel collaudo di Contatti Spark, un nuovo referente rilevato in una mail di prova viene integrato una sola volta, anche se ricompare su Telegram. Chat e bot restituiscono lo stesso contatto e la fonte. Un legame in conflitto resta da approvare.
5. **Ampliamenti.** Valutare separatamente Teams, HubSpot, recupero dello storico, nuovi canali o permessi.

Soglie iniziali proposte. **Zero email inviate da Aura e zero duplicati** negli scenari di ripetizione. Per i promemoria, obiettivo misurabile di **entro 2 minuti dall'ora prevista quando il servizio è online**. Misurare il tempo di disponibilità della bozza per mail semplici e per pratiche con allegati. Prima del rilascio, collaudare la ricezione sul telefono, il recupero dopo riavvio e il funzionamento con la chat chiusa sul runtime previsto.

## 8. Decisioni da valutare

- Come attestare manualmente l'invio effettivo dalla casella di lavoro e da quale data trattare gli inoltri ammessi. La regola di ingresso è l'inoltro scelto da Andrea dall'indirizzo Sacchi alla Gmail di gestione.
- Calendario di lavoro da gestire, occupazione delle riserve, festività, ora limite e fasce degli avvisi.
- Promemoria e attese che Aura può registrare automaticamente secondo regole approvate. Appuntamenti e aggiornamenti che richiedono la conferma di Andrea.
- Formato di Contatti Spark, permessi e regole di aggiornamento dei codici e dei referenti. Documenti ammessi, conservazione, quota e provider autorizzati.
- Capacità reali dei connettori, del bot e dello schedulatore. Qualsiasi verifica o modifica operativa di Aura resta soggetta all'autorizzazione per la singola azione.

Metodo Andrea v1.0, commit consultato `90097b8656bff84e3bf1781cf102a9eac425f466`. [Riferimento](https://github.com/Andy-PMS/metodo-andrea/commit/90097b8656bff84e3bf1781cf102a9eac425f466)
