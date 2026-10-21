# Document backend live acceptance

Date: 2026-10-22. Backend commit: `52cb5dbaf8433886ef9b4c427962874eafd0cf77`.

The already-pulled GHCR Aura container was restarted with a temporary, read-only bind of a
Linux binary built from the reviewed source. Other services and volumes were retained. The
binary reports the above commit and Docker reports healthy. This enables live API acceptance
before the frontend is complete; it is not the final published-image deployment.

Authenticated browser requests against `http://127.0.0.1:9080` returned:

| Request | Actual result |
|---|---|
| Documents, omitted file type | 200, 41 documents |
| Explicit All files | 200, 55 files |
| Images / Video | 200, 6 images / 8 videos |
| Code / Audio | 200, zero in the current index |
| `q=Cedar&search_mode=content` | 200, one synthetic study contract |
| `q=Cedar&search_mode=name` | 200, zero matches |
| Inbox / Suggestions | 200, 40 unreviewed / zero suggested |
| Saved views | 200, empty list |
| Ingestion | 200, ready, zero errors, 12,255 finished work components |
| Synthetic contract indexed content | 200, one passage containing Cedar, no more pages |
| Synthetic contract native thumbnail | 200, PNG, 13,802 bytes, 340 by 480 pixels |

The thumbnail plus subsequent content fetch took 538 ms in this run. The thumbnail response
had `Cache-Control: private, no-store` and `X-Content-Type-Options: nosniff`. Counts describe
this identity at this moment; the ingestion counter is not a file count.

No document bytes or metadata were changed by these checks. This does not yet establish
frontend behavior, code/audio ingestion, or the final GHCR image revision. Those are covered
by the following workspace and preview acceptance runs.

The same authenticated API also produced decodable PNGs from one existing indexed file per
native Craft family. Requests ran sequentially, with no persisted edits:

| Family | HTTP | Dimensions | PNG bytes | Request duration |
|---|---|---|---|---|
| Word | 200 | 340 x 480 | 24,903 | 761 ms |
| Spreadsheet | 200 | 371 x 480 | 34,839 | 409 ms |
| Presentation | 200 | 480 x 270 | 47,905 | 883 ms |
| Image | 200 | 480 x 367 | 310,349 | 1,666 ms |
| Video | 200 | 480 x 270 | 34,857 | 652 ms |

These samples verify the production route, tenant source lookup and native Craft transport
together. They do not establish every codec/import variant or document layout fidelity.
