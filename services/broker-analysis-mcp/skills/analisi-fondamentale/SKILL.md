---
name: analisi-fondamentale
description: Analisi fondamentale e valutazione relativa di società quotate con i tool fundamental_ratios e peer_comparison del server broker-analysis. Usala per multipli (P/E, EV/EBITDA, EV/EBIT, P/B, P/S), redditività e scomposizione DuPont (ROE, ROA, ROIC, margini), ciclo del capitale circolante (giorni crediti, magazzino, debiti), liquidità (current, quick, cash ratio), leva e copertura degli interessi, costo del capitale (CAPM, WACC, EVA), dividendi, crescita dei ricavi, e per confrontare una società con i concorrenti.
---

# Analisi fondamentale

## Regole
- Ogni rapporto viene dai tool: non ricalcolarlo e non stimarlo.
- I rapporti si basano sull'**ultimo bilancio annuale** (`fiscal_year_end`); `yahoo_trailing_pe_ttm` è il dato di Yahoo sugli ultimi 12 mesi. Dichiara sempre quale stai citando.
- Un valore `null` significa "non significativo" (perdite, EBITDA negativo, dato mancante): dillo, non sostituirlo.
- Riporta i `warnings`, in particolare la conversione di valuta fra prezzo e bilancio.
- È analisi interna, non una raccomandazione personalizzata a un cliente.

## Costo del capitale
- WACC, costo dell'equity ed EVA si calcolano solo se l'analista indica `risk_free_annual` e `equity_risk_premium` (decimali). Non sceglierli tu: chiedili, o proponi valori espliciti (per esempio rendimento del BTP decennale e un premio al rischio dichiarato) e fai confermare.
- Il beta è quello di Yahoo salvo `beta` indicato dall'analista; `parameters.beta_source` dice quale è stato usato.
- EVA positivo: la società rende più del costo del capitale. Il WACC usa il debito contabile come approssimazione di quello di mercato.
- Se un avviso dice che gli interessi sono una quota implausibile del debito medio, il dato di bilancio contiene oneri finanziari lordi (cambi, derivati): non presentare copertura interessi, costo del debito, WACC ed EVA come affidabili, cita l'avviso.

## Come scegliere i concorrenti
- Stesso settore e modello di business, dimensione comparabile, 3-6 società. Esempi: ENI.MI, TTE.PA, SHEL.L, BP.L, REP.MC per l'energia integrata europea; ISP.MI, UCG.MI, BNP.PA, SAN.MC per le banche.
- Riporta i ticker in `errors` invece di ignorarli.

## Quale multiplo per quale settore
| Settore | Multipli centrali | Da evitare |
| --- | --- | --- |
| Banche e assicurazioni | P/B, ROE, P/E | EV/EBITDA, debito netto/EBITDA: il debito è materia prima |
| Industria, energia, consumi | EV/EBITDA, P/E, FCF yield, debito netto/EBITDA | — |
| Crescita e tecnologia | EV/EBITDA, crescita dei ricavi, margini, ROIC | P/E se gli utili sono negativi o compressi |
| Utility e immobiliare | Debito netto/EBITDA, payout, copertura interessi | P/B senza leggere gli attivi |

## Come leggere i risultati
- **DuPont**: ROE = margine netto × rotazione dell'attivo × moltiplicatore del patrimonio (valori medi d'esercizio). Spiega da dove viene il ROE: margini, efficienza o leva.
- **Ciclo di cassa**: giorni crediti + giorni magazzino − giorni debiti. Un ciclo che si allunga assorbe cassa. I giorni debiti sono calcolati sul costo del venduto.
- **Liquidità**: current ratio, quick ratio e cash ratio; per banche e assicurazioni sono `null` per costruzione e non vanno sostituiti.
- **Valutazione**: confronta con la mediana dei concorrenti, non con soglie assolute. Uno sconto ha bisogno di una spiegazione (crescita, rischio, leva).
- **Qualità**: ROIC sopra il costo del capitale crea valore; un ROE alto con leva alta vale meno.
- **Solidità**: debito netto/EBITDA oltre 3 e copertura interessi sotto 4 vanno segnalati per i settori non finanziari.
- **Cassa**: FCF yield e payout dicono se il dividendo è coperto; un payout oltre 100% non regge a lungo.
- Il P/E annuale e quello TTM possono differire molto dopo un anno eccezionale: spiega la differenza.

## Formato della risposta
1. Una frase di giudizio: cara/in linea/a sconto rispetto ai concorrenti, con i due multipli che lo mostrano.
2. Tabella: una riga per società, colonne per i multipli del settore, unità nell'intestazione.
3. Qualità e solidità in due o tre punti.
4. Riga finale: esercizio di riferimento, valuta di bilancio, fonte, avvisi.
