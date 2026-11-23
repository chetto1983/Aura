# PRD — Aura, assistente per pratiche, comunicazioni e agenda

**Versione 0.7 · 23 novembre 2026**  
**Stato: Requisiti — da misurare**

## 1. Natura del documento

Questo PRD descrive i comportamenti richiesti per realizzare un assistente capace di seguire attività e conversazioni nel tempo.

Non costituisce una specifica tecnica validata e non attesta che le funzionalità descritte siano già disponibili.

La specifica di design dovrà derivare da un Gate 1 comprendente:

- inventario delle capacità esistenti;
- verifica delle integrazioni;
- misure sullo stack autorizzato;
- identificazione delle lacune;
- piano di implementazione e collaudo.

Gli esempi utilizzano denominazioni convenzionali. Account, persone, aziende e configurazioni reali devono rimanere fuori dalla documentazione pubblica.

## 2. Obiettivo del prodotto

Aura deve funzionare come un assistente che conserva il filo delle attività, comprende il contesto, prepara materiali e segue gli sviluppi fino alla conclusione.

Deve integrare le informazioni autorizzate provenienti da:

- email ricevute e inviate nell’account collegato;
- catene email inoltrate;
- chat Aura;
- bot Telegram privato;
- calendari;
- documenti e allegati;
- un eventuale CRM.

Il centro dell’organizzazione è la **pratica**, che può proseguire attraverso più conversazioni, persone e canali.

Aura deve aiutare l’utilizzatore a capire:

- che cosa richiede attenzione;
- chi sta gestendo un’attività;
- quale risposta o risultato si sta aspettando;
- che cosa è già stato fatto;
- quale intervento preparare;
- quali appuntamenti devono essere confermati;
- quali precedenti possono essere utili.

**Aura interagisce esclusivamente con il proprio utilizzatore.** Non contatta autonomamente clienti, colleghi, fornitori o altri soggetti della pratica.

## 3. Oggetti e informazioni da mantenere

| Oggetto | Contenuto essenziale |
|---|---|
| **Pratica** | Richiesta o obiettivo, soggetti coinvolti, stato, fonti, attività e prossimo passo. |
| **Conversazione** | Scambi email, chat o messaggi collegati alla pratica. |
| **Evidenza** | Informazione con fonte, autore e data, distinguendo contenuto osservato e interpretazione. |
| **Attività** | Azione da svolgere, responsabile individuato, risultato atteso ed eventuale termine. |
| **Attesa** | Risposta, aggiornamento o risultato che l’utilizzatore si aspetta di ottenere. |
| **Promemoria** | Avviso privato all’utilizzatore per verificare, decidere o intervenire. |
| **Proposta** | Risposta, documento, analisi o appuntamento da valutare. |
| **Decisione** | Scelta dell’utilizzatore, con ambito e versione della proposta approvata. |

Una pratica può contenere più attività e attese. La chiusura di un’attesa non implica necessariamente la conclusione dell’intera pratica.

## 4. Account di origine e account gestito da Aura

### 4.1 Percorso di acquisizione

L’utilizzatore può ricevere comunicazioni su un **account non completamente fruibile da Aura**, per limitazioni di permessi o integrazione.

Le comunicazioni selezionate vengono inoltrate a un **account collegato e gestito da Aura**.

Quest’ultimo costituisce il punto nel quale Aura:

- acquisisce le email disponibili;
- analizza le catene inoltrate;
- osserva le comunicazioni in uscita accessibili;
- prepara e conserva le bozze;
- rileva gli aggiornamenti pertinenti alle pratiche.

Aura non deve presumere di avere accesso all’account di origine.

### 4.2 Ricostruzione della catena

Aura deve distinguere:

1. il messaggio esterno di inoltro;
2. i messaggi originali riportati;
3. autori, destinatari e persone in copia nei diversi passaggi;
4. richieste, risposte, allegati e riferimenti.

Il mittente dell’inoltro non coincide necessariamente con l’autore della richiesta. Il destinatario dell’inoltro non coincide necessariamente con il soggetto incaricato dell’attività.

Aura costruisce il contesto a partire dalle informazioni effettivamente ricevute. Se una parte della catena manca, deve evidenziarlo senza inventare i passaggi.

### 4.3 Perimetro delle fonti

Devono essere configurati account, percorsi di ingresso e fonti autorizzate.

La presenza di un messaggio nell’account collegato non rende attendibili tutte le affermazioni che contiene. Aura deve distinguere:

- ciò che una fonte dichiara;
- ciò che è confermato da altre evidenze;
- ciò che viene dedotto;
- ciò che resta sconosciuto.

Istruzioni contenute in email o allegati non costituiscono autorizzazioni operative.

## 5. Comprensione delle richieste e dei ruoli

Aura deve riconoscere richieste di natura diversa, fra cui:

- preventivi e offerte;
- comparazioni tecniche;
- disponibilità e alternative di prodotto;
- appuntamenti;
- reclami e problemi;
- analisi di dati;
- preparazione o aggiornamento di documenti;
- attività affidate a colleghi o fornitori.

Per ogni pratica deve individuare:

- chi ha avviato la richiesta;
- quale risultato viene cercato;
- chi deve operare;
- chi deve decidere;
- chi deve fornire informazioni;
- quale ruolo ha l’utilizzatore;
- che cosa manca per procedere.

**La responsabilità non deriva soltanto dai campi A e CC.**

L’utilizzatore può essere destinatario diretto mentre un collega è responsabile dell’esecuzione. Può essere in copia e ricevere comunque una richiesta esplicita di intervento.

Quando la responsabilità è dedotta dal contesto, Aura deve indicarne il fondamento. Un’incertezza che cambia sostanzialmente il prossimo passo richiede un chiarimento mirato.

## 6. Continuità delle pratiche tra canali

Aura deve mantenere una visione coerente della pratica quando gli aggiornamenti arrivano da canali differenti.

Il collegamento deve considerare congiuntamente:

- identificatori e riferimenti delle conversazioni;
- codici e nomi dei clienti;
- società, fornitori e persone;
- prodotti e documenti;
- richieste e risultati attesi;
- date, appuntamenti e impegni;
- indicazioni dell’utilizzatore.

Il solo nome del cliente o un argomento simile non sono sufficienti a fondere due pratiche.

Una risposta ricevuta tramite Telegram può aggiornare un’attesa nata da un’email. Un documento può soddisfare una richiesta formulata in chat. Un appuntamento può diventare il prossimo passo di una comparazione tecnica.

Ogni collegamento deve essere spiegabile e correggibile. Le informazioni contraddittorie devono rimanere visibili fino alla loro risoluzione.

## 7. Relazione esclusiva con l’utilizzatore

Aura deve rivolgere proposte, domande e avvisi soltanto all’utilizzatore, attraverso i canali privati autorizzati.

Può:

- suggerire un prossimo passo;
- proporre un’analisi;
- preparare una risposta;
- elaborare documenti;
- registrare monitoraggi e promemoria secondo le regole approvate;
- proporre appuntamenti;
- aggiornare le informazioni della pratica.

Non deve:

- inviare o inoltrare email a terzi;
- inviare solleciti ai soggetti della pratica;
- assegnare incarichi tramite comunicazioni esterne;
- spedire inviti di calendario;
- assumere impegni commerciali.

Le elaborazioni possono partire dopo accordo sull’attività oppure nell’ambito di una regola già approvata. Nuove fonti, integrazioni, spese o permessi richiedono una decisione specifica.

## 8. Analisi di documenti e preparazione dei risultati

Aura deve utilizzare gli strumenti autorizzati per leggere e confrontare i formati supportati, inclusi PDF, documenti di testo e fogli di calcolo.

Deve:

- distinguere le versioni;
- verificare codici, quantità, unità, valuta, IVA e validità;
- citare pagina, tabella, scheda o riga quando disponibile;
- segnalare dati mancanti o incongruenti;
- dichiarare file protetti, scansioni ambigue o contenuti non leggibili;
- separare fatti verificati e valutazioni.

Può consultare documenti di riferimento e anagrafiche per suggerire a chi chiedere un’informazione e quale domanda formulare.

I documenti aggiornati devono essere preparati come versioni riconoscibili, preservando gli originali salvo autorizzazione esplicita alla sostituzione.

Il risultato deve essere reso disponibile nel canale concordato: bozza email, chat Aura, bot Telegram privato o altra destinazione autorizzata.

## 9. Bozze email in attesa di revisione

Quando serve una comunicazione email, Aura deve preparare una **bozza nativa nell’account collegato**.

La bozza può contenere:

- oggetto;
- testo completo;
- eventuali allegati preparati e verificati.

**I campi A, CC e CCN devono restare vuoti.**

L’utilizzatore deve poter:

- leggere e modificare il contenuto;
- scegliere gli allegati;
- inserire personalmente i destinatari;
- inviare dall’account collegato oppure utilizzare il materiale attraverso un altro account.

Aura lascia la bozza in attesa e non la invia.

Dopo il salvataggio verificato, avvisa l’utilizzatore sul bot Telegram indicando la pratica, lo scopo della proposta e dove esaminarla.

Una nuova analisi non deve sovrascrivere silenziosamente modifiche manuali. Le revisioni sostanziali devono essere presentate separatamente.

Un testo disponibile soltanto in chat non soddisfa il requisito di bozza nativa nell’account email.

## 10. Comunicazioni in uscita e presa in carico delle attese

### 10.1 Percorsi diretti e indiretti

L’utilizzatore può inviare una comunicazione:

- direttamente dall’account collegato ai soggetti interessati;
- a un proprio account con limitazioni, per proseguire da lì;
- attraverso altri percorsi, successivamente documentati nelle fonti accessibili.

Aura deve analizzare il significato operativo della comunicazione, senza limitarsi al destinatario esterno del messaggio.

Codice cliente, nomi, riferimenti, contenuto della catena e istruzioni possono indicare a quale pratica appartiene la comunicazione e da chi si attende un risultato.

### 10.2 Regola di presa in carico

**Una comunicazione inviata o inoltrata dall’utilizzatore è sufficiente ad avviare il monitoraggio quando il contesto contiene un’attività, una richiesta di risposta o un esito da attendere.**

La regola vale anche quando il messaggio viene trasferito a un altro account dell’utilizzatore, anziché direttamente al soggetto finale.

Il trasferimento viene considerato un passaggio operativo della pratica. Aura prende in carico l’aspettativa risultante dal contenuto e applica le regole di promemoria approvate.

Non deve attendere una prova della consegna finale per ricordare all’utilizzatore che quell’attività merita attenzione.

### 10.3 Monitoraggio e consegna sono informazioni distinte

Aura deve distinguere:

| Informazione | Trattamento |
|---|---|
| **Attività da monitorare** | Acquisita dal contesto della comunicazione dell’utilizzatore. |
| **Risposta o esito atteso** | Registrato con oggetto, soggetto individuato e fonte. |
| **Consegna finale** | Registrata solo quando osservabile; la sua assenza non blocca il monitoraggio. |
| **Ricezione o lettura** | Non dichiarata senza evidenza. |

Un promemoria appropriato può essere:

> «Per la pratica Cliente A risulta ancora da acquisire l’esito della comparazione richiesta a Collega B. Ci sono aggiornamenti?»

Non deve trasformarsi in un’affermazione non verificata come:

> «Collega B ha ricevuto la mail e non ti ha risposto.»

### 10.4 Quando non aprire un’attesa

Non ogni trasferimento deve generare un promemoria.

Aura non apre automaticamente un’attesa quando il contenuto:

- serve soltanto ad archiviare o conservare un documento;
- comunica un’attività già conclusa;
- è puramente informativo e non comporta un seguito;
- duplica un’attesa già registrata.

Una bozza creata da Aura e mai utilizzata non costituisce, da sola, una richiesta operativa dell’utilizzatore. Può generare un avviso di revisione della bozza, distinto dal monitoraggio di una risposta esterna.

## 11. Regole per attese e promemoria

### 11.1 Informazioni da registrare

Per ogni attesa Aura deve mantenere:

- pratica collegata;
- richiesta o risultato atteso;
- persona o ruolo coinvolto, se individuabile;
- comunicazione o istruzione che ha avviato il monitoraggio;
- data di presa in carico;
- eventuale termine esplicito;
- prossimo controllo;
- aggiornamenti ricevuti e punti ancora aperti.

Una pratica può avere più attese distinte. Il replay dello stesso messaggio non deve crearne altre né far ripartire il conteggio del tempo.

### 11.2 Controlli dopo 24 o 48 ore

Espressioni come «fammi sapere», «verifica questa alternativa» o «attendo aggiornamenti» devono poter attivare un controllo automatico secondo regole approvate.

Il prodotto deve consentire, per esempio, un controllo dopo 24 oppure 48 ore.

Occorre distinguere:

- **termine esplicito**, presente nella comunicazione;
- **momento del controllo interno**, stabilito per l’assistente.

Il controllo interno non deve essere presentato come una scadenza accettata dal soggetto esterno.

Prima dell’attivazione devono essere definiti:

- quando applicare 24 o 48 ore;
- ore solari oppure lavorative;
- festività e ora limite;
- fasce degli avvisi;
- gestione delle urgenze;
- frequenza dei controlli successivi.

### 11.3 Rivalutazione contestuale

Prima di inviare un promemoria, Aura deve verificare gli aggiornamenti disponibili nei canali autorizzati.

| Aggiornamento | Effetto richiesto |
|---|---|
| Risposta completa | Chiudere l’attesa soddisfatta. |
| Risposta parziale | Mantenere aperti soltanto i punti mancanti. |
| Nuovo termine concordato | Aggiornare il controllo conservando la fonte. |
| Soluzione dichiarata dall’utilizzatore | Registrare l’esito e disattivare i controlli superati. |
| Appuntamento concordato come prossimo passo | Sostituire il sollecito superato con la verifica pertinente all’appuntamento o al suo esito. |
| Appuntamento soltanto simile o non chiaramente collegato | Non chiudere né sospendere automaticamente l’attesa. |
| Risposta automatica o mancata consegna | Registrare il problema e adattare il promemoria rivolto all’utilizzatore. |
| Fonte temporaneamente indisponibile | Dichiarare la copertura incompleta, senza simulare una verifica riuscita. |

La presenza di un aggiornamento non basta: deve essere pertinente alla specifica richiesta monitorata.

## 12. Appuntamenti provvisori e confermati

Aura deve proporre e gestire, entro le autorizzazioni concesse, appuntamenti privati sul calendario designato.

| Stato | Comportamento |
|---|---|
| **Proposto** | Presentare scopo, pratica, data, ora, durata, luogo e persone previste. |
| **In sospeso** | Dopo autorizzazione, creare una riserva privata che occupa la fascia. |
| **Confermato** | Aggiornare lo stesso evento sulla base della conferma e dell’autorizzazione previste. |
| **Spostato o annullato** | Aggiornare l’evento e i promemoria collegati. |
| **Da consuntivare** | Verificare l’esito dopo l’appuntamento. |
| **Effettuato o non svolto** | Registrare risultato, persone effettivamente incontrate e prossime attività. |

Gli appuntamenti in sospeso devono essere distinguibili dai confermati e partecipare al controllo dei conflitti.

Le persone previste possono essere indicate nella descrizione privata, ma non devono essere inserite come invitati ai quali inviare comunicazioni.

Se manca un orario preciso, Aura deve chiedere il dato senza inventare una fascia.

Devono essere gestiti:

- modifiche manuali;
- riserve scadute;
- fusi orari e cambio dell’ora;
- approvazioni legate alla versione della proposta;
- annullamento dei promemoria superati.

Le proposte iniziali di controllo almeno tre giorni prima e avviso due ore prima restano da valutare e configurare.

## 13. Memoria, chiusura e apprendimento

Aura deve mantenere una memoria operativa consultabile nelle conversazioni successive, composta da fatti, fonti, relazioni, decisioni e risultati.

Quando una pratica viene dichiarata chiusa:

- registra esito, data e fonte;
- disattiva i promemoria non più necessari;
- conserva lo storico secondo le regole approvate;
- rende recuperabili i precedenti pertinenti.

Una nuova richiesta dello stesso cliente può richiamare lo storico, senza fondersi automaticamente con una pratica conclusa.

Informazioni storiche come prezzi, disponibilità, ruoli e recapiti devono essere presentate con la loro data e non assunte come ancora valide.

L’apprendimento del metodo dell’utilizzatore deve distinguere:

1. correzione della pratica corrente;
2. proposta di una preferenza generale;
3. regola permanente approvata, versionata e revocabile.

Le correzioni ripetute non costituiscono da sole un’autorizzazione a modificare il comportamento generale.

## 14. Contatti, documenti di riferimento e CRM

Aura deve poter consultare fonti autorizzate contenenti contatti, aziende, competenze, brand e relazioni professionali.

Queste informazioni servono a suggerire all’utilizzatore:

- a chi potrebbe chiedere;
- che cosa chiedere;
- perché quella persona è pertinente;
- che cosa è già stato chiesto;
- quando e con quale esito.

Il suggerimento rimane una comunicazione privata all’utilizzatore e non comporta la compilazione dei destinatari nelle bozze.

Devono essere preservati codici con zeri iniziali, società distinte e relazioni documentate. Omonimie e conflitti richiedono disambiguazione.

### HubSpot

HubSpot può essere valutato come supporto per contatti, aziende, visite, appuntamenti, attività e riferimenti alle comunicazioni.

La progettazione deve definire:

- quali dati sono autorevoli nel CRM;
- quali stati sono gestiti da Aura;
- come prevenire duplicati e sincronizzazioni circolari;
- come collegare lo storico senza fondere pratiche diverse;
- quali letture e scritture sono autorizzate.

L’integrazione è opzionale e non viene attivata dal presente PRD.

Il documento denominato **Contatti Spark** nel PRD precedente è una possibile fonte dell’installazione personale. **Formato, disponibilità e permessi restano da verificare.**

## 15. Consultazione e report

Aura deve rispondere con una sintesi iniziale, seguita dai dettagli necessari e dalle fonti.

Esempi:

- «Quali risultati stiamo ancora aspettando?»
- «Che cosa è successo con Cliente A nell’ultimo mese?»
- «Chi segue questa comparazione e quale aggiornamento manca?»
- «Quali appuntamenti sono ancora provvisori?»
- «Quando abbiamo chiesto questa informazione e a chi?»
- «Quali clienti risultano senza attività registrate negli ultimi tre mesi?»

I report devono distinguere:

- comunicazioni ricevute;
- comunicazioni inviate o trasferite;
- risposte attese;
- appuntamenti previsti;
- incontri effettivamente svolti;
- attività concluse.

Devono dichiarare periodo, fonti disponibili e limiti di copertura. L’assenza di evidenze non deve essere presentata automaticamente come assenza di attività reale.

## 16. Esempio completo

1. **Cliente A** invia una richiesta di comparazione tecnica a un account dell’utilizzatore con accesso limitato, coinvolgendo **Collega B**.
2. L’utilizzatore inoltra la catena all’account gestito da Aura.
3. Aura ricostruisce ruoli e richiesta, analizza gli allegati e collega il contenuto alla pratica.
4. L’utilizzatore concorda con Aura la preparazione di indicazioni per verificare un’alternativa presso **Fornitore C**.
5. Aura prepara una bozza con oggetto, testo ed eventuali documenti. A, CC e CCN restano vuoti. Il bot avvisa l’utilizzatore.
6. L’utilizzatore modifica il testo, includendo «fammi sapere», e lo inoltra al proprio account con accesso limitato per proseguire la comunicazione.
7. **Aura prende in carico l’attesa della comparazione**, riconoscendo la pratica e il risultato richiesto dal contesto. Una regola approvata prevede un controllo dopo 48 ore. La verifica della consegna finale non è un prerequisito.
8. Nel frattempo l’utilizzatore scrive sul bot che è stata concordata una visita per esaminare l’alternativa.
9. Aura aggiorna la pratica e, se la visita sostituisce il passaggio prima atteso, riprogramma il controllo. Dopo autorizzazione gestisce la riserva privata sul calendario.
10. Dopo la visita acquisisce l’esito. Se serve una comunicazione, prepara una nuova bozza senza destinatari e informa l’utilizzatore.
11. Alla conclusione documentata archivia la pratica e conserva lo storico per richieste future.

## 17. Capacità mancanti e Gate 1

| Area | Verifica necessaria |
|---|---|
| **Bozze native Gmail** | Secondo il riscontro del manutentore alla PR, il PIM dispone dell’invio ma non della creazione o aggiornamento delle bozze nel provider. Questa capacità deve essere realizzata e collaudata. |
| **Fondamenta email** | Verificare messaggio grezzo, threading, identificatori stabili, rilevamento modifiche, posta inviata e allegati, coordinandosi con il client di posta del cockpit. |
| **Monitoraggio contestuale** | Verificare riconoscimento delle richieste, collegamento delle pratiche e attivazione delle attese anche nei trasferimenti fra account. |
| **Documenti e contatti** | Accertare formati, lettura, scrittura, provenienza e gestione delle modifiche concorrenti. |
| **Agenda** | Provare riserve occupate, stati, conflitti, aggiornamento dello stesso evento e modifiche manuali. |
| **Avvisi** | Verificare scheduler, Telegram, funzionamento a chat chiusa e recupero dopo interruzione. |
| **Memoria tra canali** | Provare persistenza, isolamento per utilizzatore e recupero del contesto nelle conversazioni successive. |

La specifica tecnica dovrà separare capacità presenti, mancanti e non verificate.

Occorre riusare i componenti esistenti prima di introdurre nuovi servizi o archivi. Una skill testuale non garantisce da sola persistenza, isolamento, blocco degli invii o affidabilità dei promemoria.

## 18. Criteri di accettazione

| Scenario | Risultato richiesto |
|---|---|
| Utilizzatore in A oppure CC | Ruolo ricostruito dal contenuto, non soltanto dalle intestazioni. |
| Catena incompleta | Lacune dichiarate, nessun passaggio inventato. |
| Pratica distribuita fra canali | Contesto coerente e fonti recuperabili. |
| Due pratiche simili dello stesso cliente | Nessuna fusione non giustificata. |
| Bozza preparata da Aura | A, CC e CCN vuoti; nessun invio. |
| Modifica manuale della bozza | Contenuto preservato nelle elaborazioni successive. |
| Trasferimento fra account contenente una richiesta o un esito atteso | **Monitoraggio attivato secondo le regole approvate, senza richiedere prova della consegna finale.** |
| Trasferimento puramente informativo | Nessuna attesa creata senza un motivo contestuale. |
| Consegna finale non osservabile | Monitoraggio attivo; nessuna falsa affermazione di ricezione o lettura. |
| Replay dello stesso messaggio | Nessuna attesa duplicata né riavvio ingiustificato del termine. |
| «Fammi sapere» senza data | Controllo interno secondo regola approvata. |
| Risposta parziale | Restano aperti soltanto i punti mancanti. |
| Soluzione comunicata tramite chat o bot | Attesa aggiornata e promemoria superati disattivati. |
| Appuntamento collegato | Controllo adattato senza falsa conclusione della pratica. |
| Riserva in sospeso | Fascia occupata, stato riconoscibile, nessun invito esterno. |
| Chiusura della pratica | Storico conservato e promemoria superati disattivati. |
| Fonte indisponibile | Limite dichiarato, nessuna verifica simulata. |
| Riavvio o consegna incerta | Recupero riconciliato, senza ripetizioni cieche. |

Obiettivi iniziali da misurare:

- **zero email o inviti inviati autonomamente a terzi**;
- zero destinatari inseriti automaticamente nelle bozze;
- zero duplicati negli scenari di ripetizione collaudati;
- promemoria entro due minuti dall’istante pianificato quando servizio e canale sono disponibili;
- ricezione verificata sul telefono e funzionamento a chat chiusa.

Misurare anche collegamenti errati, monitoraggi mancati, promemoria inutili, correzioni dell’utilizzatore, tempi delle elaborazioni e consumi per pratica.

## 19. Sequenza di realizzazione e decisioni aperte

Ordine proposto:

1. Verificare le fondamenta PIM e delimitare le fonti.
2. Provare una pratica completa: inoltro, analisi, bozza senza destinatari, avviso e presa in carico del seguito.
3. Integrare monitoraggio delle comunicazioni dirette e dei trasferimenti fra account.
4. Collegare aggiornamenti da chat, Telegram, documenti e calendario.
5. Gestire riserve, conferme e consuntivi.
6. Integrare contatti, eventuale CRM e memoria storica.
7. Collaudare continuità, isolamento e recupero sullo stack autorizzato.

Prima del pilota devono essere definite:

- fonti, account e calendario coperti;
- data iniziale e gestione dello storico;
- regole delle 24/48 ore e controlli successivi;
- giorni lavorativi, festività e fasce degli avvisi;
- autorizzazioni per analisi, promemoria e scritture in agenda;
- canali di consegna dei materiali;
- conservazione, provider, quote e budget;
- ruolo dell’eventuale CRM.

Il criterio centrale è la **continuità dell’assistenza**: Aura prende in carico ciò che l’utilizzatore sta facendo, segue gli esiti attesi e prepara gli interventi utili, mantenendo comunicazioni e decisioni esterne sotto il controllo dell’utilizzatore.
