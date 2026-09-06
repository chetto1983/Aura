# Handoff — 2026-09-06

Session guidata dallo stack acceso: sweep dei tool con l'agente reale, audit sulle
*classi* dei bachi emersi (non sulle istanze), e infine l'E2E dell'ingestione documenti
— l'unica parte del prodotto mai provata davvero end-to-end. Tutto quello che segue è
misurato; dove non lo è, lo dice.

## Shipped and pushed

| commit | what |
|---|---|
| `e57c54d7` | una libreria vuota non è un guasto ArcadeDB (+ logger di degradazione a rate limite) |
| `0d74bc00` | una consegna che non può riuscire smette di ritentare (allowance 12, poi abbandono esplicito) |
| `192d2f36` | archiviare un nome che non c'è è un errore, non un rename fallito |
| `460f4255` | rigenerato sqlc dopo che 0119 ha droppato le sue due tabelle |
| `f5e3d4fd` | audit sulle classi: 5 altri silent-swallow, predicato stretto su payload misurato, gate pre-push nuovo |
| `56c46d67` | il resume accetta l'interrupt id che il gateway stesso pubblica |
| `fb596ab8` | copertura del reporter dei drop di recall |
| `9204d4ed` | re-pin dei due baseline mossi dalle statement dell'audit |
| `745b5f7c` | il job coverage avviava un daemon che scriveva sul suo stesso database di test |
| `6d003c7c` | la guardia RLS contava due dei tre modi in cui un ruolo bypassa una policy |
| `d187d811` | la TTL sweep leggeva l'unica root dove gli snippet non vengono mai salvati |
| `a45233ca` | due chiamanti usavano l'identità che il prodotto cancella al primo login |
| `b8b475ea` | amendment #223: la prima misura E2E vera dell'ingestione documenti |
| `1e99f7c9` | split del file di test oltre il cap 600 LOC |
| `a924512f` | re-pin di `internal/db` dopo le statement della guardia RLS |

**Stato CI da correggere rispetto a quanto detto a voce durante la sessione:** le run
**#1811** e **#1812** erano `failure`, non verdi. Due cause, entrambe mie e entrambe
chiuse da `1e99f7c9` + `a924512f`: `internal/cron/dispatch_test.go` a 608 LOC oltre il
cap, e il denominatore di `internal/db` passato da 315 a 322. La #1814 sul nuovo head
era ancora in volo alla chiusura di questa sessione: **va verificata prima di
considerare master verde.**

## L'E2E dell'ingestione documenti — chiuso

Il punto della lista dell'operatore rimasto aperto più a lungo. Adesso l'anello si
chiude, dal caricamento fino ai byte riletti dentro la sandbox:

`POST /api/filemanager/upload` → bucket Garage dell'identità → passata live di
CocoIndex (`aura-ingest-supervisor` → un figlio `python -m ingest.app` per identità) →
`IndexedDocument` + `Passage` in `mem_<uuid>` → `document_search` fuso lato Go →
`document_open` → `shell_exec` che rilegge il file **dentro** la box.

Le misure: un markdown di 384 byte compare in `[extract]` entro un intervallo live
(`AURA_INGEST_INTERVAL_SEC=20`), atterra come `doc_698c54f2…` con `passage_count=1`,
`raw_sha256=57a6b2ec…`, `card` scritta da `aura-filecard` e `file_name_words` popolata.
`document_search` risponde `profile: arcadedb-fused-card-v2`, `status: complete`, con
**due gambe accese** sullo stesso documento (`fused` rank 1, `card` rank 1 a
14.175487). `document_open` materializza
`/workspace/documents/document-93e9051d…/contratto-rossini.md` e `sha256sum` **dentro
la box** ridà `57a6b2ec…`: la parità fra il `search_document_id` coniato in Python
(`services/ingest/identity.py`) e quello atteso da Go (`internal/documents/ids.go`) non
è più un'asserzione dei test.

Corollario: `status: complete` con la gamba `card` accesa è la **prova in negativo** del
degradation logger di `e57c54d7` — su uno stack configurato bene non degrada.

**Come è stato acceso senza la sua immagine.** `docker.io` risponde 429 sull'immagine
base e l'indice apt prende 405 dal proxy, quindi `docker/aura-ingest/Dockerfile` in
questo box non si costruisce. La pipeline è stata accesa lo stesso perché non ha niente
di specifico al container:

```
go build -o /tmp/bin/ ./cmd/aura-filecard ./cmd/aura-media-index ./cmd/aura-ingest-supervisor
python3 -m venv venv && venv/bin/pip install 'cocoindex[amazon_s3,postgres]==1.0.20' iscc-tika==0.6.0 neo4j==5.28.1
cd services/ && PATH=venv/bin:/tmp/bin:$PATH aura-ingest-supervisor   # lancia `python -m ingest.app`
```

Il supervisore risolve da Postgres il binding Garage cifrato di ogni identità attiva e
inietta `AURA_INGEST_S3_*` nel figlio; l'unica cosa che va data a mano è che il cwd sia
`services/` (nel container è `/app`, con `services/ingest` → `/app/ingest`). È una
scorciatoia diagnostica utile, **non** un modo di esercizio supportato.

**Cosa NON è stato provato** (dettaglio in `prd.md` §Amendment #223): nessuna immagine
costruita, quindi LibreOffice e poppler non verificati e con loro `.xls` legacy e il
testo dei PDF; solo testo/markdown ingerito, nessun XLSX, nessun OCR/STT (`aura-ocr-vl`
e `aura-stt` non erano nemmeno accesi); **solo il percorso *add*** — modify e delete
della riconciliazione CocoIndex restano non misurati; una sola identità, quindi
l'isolamento fra `/state/<identity>/coco.db` diversi non è provato; corpus di quattro
documenti con un passaggio a testa, quindi il tetto dei 2048 token non è stato sfiorato
e sui punteggi di ranking questa misura non autorizza nessuna affermazione.

## I due difetti più seri chiusi

**La guardia RLS contava due termini su tre** (`6d003c7c`). `VerifyRLSEnforced` rifiuta
di avviare un daemon il cui ruolo di pool ignora le policy: controllava `rolsuper` e
`rolbypassrls`. La migration 0087 aveva già scritto il terzo, due volte — *un table
owner bypassa RLS di default*. Misurato su database usa-e-getta: `aura_migrate`
**possiede tutte e 19 le tabelle con RLS** e `pg_roles` gli dà `rolsuper=false
rolbypassrls=false`, quindi `VerifyRLSEnforced(aura_migrate)` tornava `nil`. Un daemon
puntato su `AURA_DB_MIGRATE_URL` — l'altro DSN che il deployment già possiede — partiva
pulito e serviva le righe di ogni tenant a ogni tenant, senza un sintomo. Il nuovo
termine conta i possessi invece di confrontare un nome, così un deployment che migra con
un altro ruolo è coperto gratis.

**Due chiamanti usavano l'identità che il prodotto cancella al primo login**
(`a45233ca`). Il più grave è `internal/cron`: `scheduler_tasks.identity_id` è text con
DEFAULT `'local'` e i sei seeder di boot creano le loro sweep **senza identità**, quindi
il sentinel raggiungeva il fallback a ogni boot — mentre
`idempotency_operations.identity_id` è FK su `aura.identities` ON DELETE CASCADE. Dal
primo enrolment in poi lo scheduler scriveva una chiave esterna verso una riga che non
esisteva più, sui propri task di sistema, a ogni tick. È lo stesso difetto che
`idempotency_http.go` aveva già chiuso per le mutazioni pubbliche, nello stesso
registry.

## Aperti, in ordine di priorità

1. **Verificare la CI #1814** sul head `a924512f`. Se rossa, la diagnosi va fatta prima
   di qualunque altra cosa: master non è verde finché non lo dice una run.
2. **I percorsi non provati dell'ingestione** elencati sopra — in particolare *modify* e
   *delete* della riconciliazione, che sono esattamente ciò che l'emendamento #118 dice
   di aver comprato da CocoIndex senza codice nostro. È un'affermazione ancora non
   misurata su questo stack.
3. **L'immagine `aura-ingest` non è costruibile in questo ambiente.** Chi ha un box con
   accesso a `docker.io` e ad apt dovrebbe costruirla e ripetere l'E2E *dentro* il
   container, che è l'unico modo per esercitare LibreOffice, poppler e il percorso PDF.
4. **Il letterale `local` ancora vivo nelle colonne text.** Il conteggio delle righe che
   lo portano resta ignoto: su un database appena migrato `scheduler_tasks`,
   `pending_notifications` e `skill_audit` danno zero perché sono **vuote**, non perché
   il letterale sia sparito. Quel numero decide se allinearle a `uuid` sia un `UPDATE` da
   cinque righe o una migrazione vera, e si prende solo su un deployment vissuto.
5. **`LEFT JOIN` attraverso un confine RLS degrada a NULL, non a errore.**
   `purgeableQuery` (`cmd/aura/serve_provisioning.go`) fa `LEFT JOIN
   aura.identity_auth_links`; se quella tabella prendesse RLS, il purge cancellerebbe
   l'identità Aura e **non** l'utente Authula, in silenzio. Oggi non è un baco: è il
   vincolo da leggere prima che qualcuno gliela metta.

## Coverage, misurato 2026-09-06

Sul database usa-e-getta `aura_cov`, stessa tier della CI (`db_integration`, covdata
nativo, `-coverpkg=./internal/...`):

- aggregato owned-surface: **34022/39254 = 86.7%** ≥ 85%
- `internal/db`: **269/322 = 83.5404%** (pin precedente 263/315 = 83.4921%) — il rapporto
  **sale**, quindi il re-pin registra codice nuovo meglio coperto della media del
  package, non un'asticella abbassata. Resta in `mode: baseline`: 83.5% è sotto il target
  85% e tiene il pavimento di non-regressione, non un'esenzione.

Il gate fallisce **closed** su qualunque cambio di denominatore, deliberatamente: un
denominatore non si deduce, si rimisura e si scrive. Le run #1811 e #1812 sono la prova
che funziona.

## Note di processo — errori commessi in questa sessione

- **`pgrep -f <script>` matcha la propria shell.** Preso due volte. Ha prodotto tre
  affermazioni sbagliate a voce ("il run è ancora in corso" quando era finito).
- **`api.github.com` via curl risponde 403 dal proxy.** Ogni watcher scritto con curl è
  andato in timeout sembrando "una run lenta". Per GitHub si usano i tool
  `mcp__github__*`, punto.
- **Un finding ritirato:** avevo riportato che `tool_search select:` caricava 1 tool su 6.
  Era il driver che rispondeva al turno pendente di una run **morta**. `select:` con più
  nomi funziona.
- **Un finding evitato di misura:** `searchable_text` e `description` tornavano NULL da
  `IndexedDocument`. Non è un baco — quelle colonne non esistono, si chiamano
  `file_name_words` e `card`. ArcadeDB restituisce null invece di errore su un nome
  inesistente, il che rende una query sbagliata indistinguibile da un dato mancante.
- **Un secondo finding evitato:** `cmd/aura-ingest-supervisor` passa
  `identityctx.LocalOperatorIdentity` a `objectstore.NewIdentityStore` e sembrava un
  terzo caso del difetto di `a45233ca`. Non lo è: lì l'argomento è
  `localSeededIdentityID`, cioè *quale* identità è quella seed, non chi possiede il
  lavoro. Va lasciato com'è.
- **Il conteggio delle tabelle RLS l'ho sbagliato tre volte** prima di misurarlo. La
  causa dell'ultimo errore: la regex non vedeva i `DROP TABLE IF EXISTS` multilinea
  (0098) né l'`ENABLE ROW LEVEL SECURITY` dentro un `FOREACH ... EXECUTE format()`
  PL/pgSQL (0093). Il numero vero, confermato live: **47 tabelle, 19 con RLS**.
