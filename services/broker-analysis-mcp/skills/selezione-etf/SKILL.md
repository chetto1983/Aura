---
name: selezione-etf
description: Valutazione e confronto di ETF con i tool etf_analysis e risk_metrics del server broker-analysis. Usala quando l'analista chiede costo di un ETF, TER, tracking difference, tracking error, quale ETF replica meglio un indice, o un confronto fra ETF sullo stesso indice.
---

# Selezione ETF

## Regole
- Costi e qualità della replica vengono da `etf_analysis`, il rischio da `risk_metrics`: non calcolare nulla.
- Convenzione di segno: **tracking difference = rendimento ETF − rendimento benchmark**, annualizzata. Negativa = l'ETF ha reso meno dell'indice. Dichiarala sempre: alcuni siti usano il segno opposto.
- Riporta tutti i `warnings`.
- È analisi interna, non una raccomandazione personalizzata a un cliente.

## Scegliere il benchmark
1. **Stesso fuso orario** del fondo: con chiusure diverse il tracking error si gonfia. Misurato: SWDA.MI contro URTH (New York) 6,5% settimanale, contro IWDA.AS (Amsterdam) 0,2%. Per un ETF quotato a Milano usa un ETF europeo sullo stesso indice o l'indice locale.
2. **Total return** (dividendi inclusi): con un indice di prezzo la tracking difference risulta gonfiata del rendimento da dividendi.
3. Il tool converte la valuta del benchmark in quella del fondo con i cambi Yahoo giornalieri.

## Come leggere i risultati
| Metrica | Lettura | Attenzione |
| --- | --- | --- |
| `ter` | Costo dichiarato annuo | Esclude spread e costi di negoziazione |
| `tracking_difference` | Costo reale della replica | Può superare il TER (costi nascosti) o essere migliore (prestito titoli, ritenute ottimizzate) |
| `tracking_error` | Fedeltà della replica, su rendimenti settimanali | Valido solo con benchmark nello stesso fuso orario |
| `aligned_days` | Giorni confrontati | Sotto un anno la tracking difference non è affidabile |

## Altri criteri da segnalare (non coperti dal tool)
- Domicilio (Irlanda o Lussemburgo), metodo di replica (fisica o sintetica), politica dei proventi (accumulazione o distribuzione), patrimonio del fondo, spread di negoziazione su Borsa Italiana.
- Fiscalità italiana: le plusvalenze da ETF armonizzati sono redditi di capitale (26%) e non compensano minusvalenze pregresse.

## Formato della risposta
1. Una frase: quale ETF costa meno davvero (tracking difference) e replica meglio (tracking error).
2. Tabella per ETF: TER, tracking difference, tracking error, finestra.
3. Avvisi su benchmark e dati, e criteri non coperti da verificare sul KID.
