# Quality Bench Fixtures — License Attribution

Tutti i file in questa directory sono distribuiti sotto licenze permissive. Lista per file.

| File | Origine | Licenza | Note |
|---|---|---|---|
| `arxiv-enigma.pdf` | [arXiv:2008.03122](https://arxiv.org/abs/2008.03122) — "Sulla decifratura di Enigma" by Claudia Violante & Fabio S. Priuli | arXiv non-exclusive distribution license | 39 pagine, italiano, downloaded 2026-05-21 |
| `autori-italiani.json` | self-built dataset | CC0-1.0 (Public Domain Dedication) | 20 autori italiani, fatti verificabili contro Wikipedia IT |
| `istat-covid-regioni.csv` | [pcm-dpc/COVID-19](https://github.com/pcm-dpc/COVID-19) — Presidenza del Consiglio, Dipartimento Protezione Civile | CC-BY 4.0 | 21 regioni, dati COVID snapshot, downloaded 2026-05-21 |
| `tika-testEXCEL.xlsx` | [Apache Tika test resources](https://github.com/apache/tika/) — `tika-parser-microsoft-module/src/test/resources/test-documents/testEXCEL.xlsx` | Apache-2.0 | Fixture canonica di Apache Tika |
| `tika-testWORD.docx` | [Apache Tika test resources](https://github.com/apache/tika/) — `tika-parser-microsoft-module/src/test/resources/test-documents/testWORD.docx` | Apache-2.0 | Fixture canonica di Apache Tika |
| `tika-testPPT.pptx` | [Apache Tika test resources](https://github.com/apache/tika/) — `tika-parser-microsoft-module/src/test/resources/test-documents/testPPT.pptx` | Apache-2.0 | Fixture canonica di Apache Tika |
| `wiki-collodi.epub` | Generated from [Wikipedia IT — Carlo Collodi](https://it.wikipedia.org/wiki/Carlo_Collodi) | CC-BY-SA 4.0 (content) | EPUB valido (zip + container.xml + opf + ncx + xhtml) costruito a mano da snapshot Wikipedia 2026-05-21. Contenuto reale, formato reale, no mock. |
| `wiki-dante.txt` | [Wikipedia IT — Dante Alighieri](https://it.wikipedia.org/wiki/Dante_Alighieri) via REST API `/page/html/` + HTML strip | CC-BY-SA 4.0 | Plain text snapshot 2026-05-21 |
| `wiki-galileo.html` | [Wikipedia IT — Galileo Galilei](https://it.wikipedia.org/wiki/Galileo_Galilei) | CC-BY-SA 4.0 | HTML snapshot 2026-05-21 |
| `wiki-galileo.md` | [Wikipedia IT — Galileo Galilei](https://it.wikipedia.org/wiki/Galileo_Galilei) via API `prop=wikitext` | CC-BY-SA 4.0 | Wikitext snapshot 2026-05-21 (estensione `.md` per testare il path passthrough markdown) |

## Note di compliance

- **Apache-2.0** (Tika fixtures): nessun adattamento necessario, attribuzione via questo file
- **CC-BY 4.0** (ISTAT/Protezione Civile): attribuzione completa nel file + link al dataset originale
- **CC-BY-SA 4.0** (Wikipedia/Wikisource): attribuzione completa + se ridistribuito devono mantenersi le stesse condizioni di licenza
- **CC0-1.0** (autori-italiani.json): dedicato al Pubblico Dominio, nessuna restrizione
- **arXiv license**: distribuzione libera per ricerca; autore conserva copyright

## Note di completezza

- `wiki-collodi.epub` è generato da contenuto reale Wikipedia IT, formato EPUB 2.0 valido (zip con mimetype uncompressed + META-INF/container.xml + OEBPS/content.opf + OEBPS/toc.ncx + OEBPS/chapter1.xhtml). Necessario per il bench perché tutte le sorgenti EPUB pubbliche di classici italiani (Gutenberg, Wikisource, Internet Archive) erano irraggiungibili al momento del download (404 / 503 / cert expired). Il pattern "real content + real format, self-assembled" preserva la validità del test del format handler.

## Re-download

Ogni file può essere ri-scaricato eseguendo gli script in `docs/quality-bench/` (TBD). Le URL sorgente sono nella colonna "Origine" sopra.
