# Turn recall frozen evaluation — gemma4:31b-cloud, split calibration, 3 trial(s)

Generated 2026-10-07T13:28:40Z by TestTurnRecallFrozenEval.

The teacher answers in the background (spec amendment 2026-10-07): it never decides the turn it is asked about. The seeds + teacher arm is therefore the turn's seeds decision with memory empty, and the background teacher line says how often that arm would have had the turn labelled. The recall arm's memory holds every earlier turn with its decision, upgraded to the teacher's label when the teacher answered.

What this does not show: the replay writes a teacher label before the next turn is read, but production reaches memory only at the next reconcile tick (about a minute). Recall-arm memory hits are therefore an upper bound for follow-ups sent sooner than that.

## calibration (66 readings)

| Arm | Accuracy | 95% Wilson | Hard→none |
|---|---|---|---|
| seeds | 60/66 | 0.816–0.958 | 0 |
| seeds + teacher | 60/66 | 0.816–0.958 | 0 |
| recall | 60/66 | 0.816–0.958 | 0 |

Recall arm sources: map[memory:3 seeds:63].

Background teacher: the seeds + teacher arm asked on 24/66 readings and would have had 24 labelled; the recall arm asked on 21 and learned 21 labels. Outcomes: map[success:24]. Teacher latency p50 403.422413ms, p95 2.236299181s.

Recall miss reasons: map[label:3 no_compatible_label:63].

Memory precision: 3/3. Recall latency p50 29.885807ms, p95 55.874189ms. Whole reading p50 47.435484ms, p95 80.363952ms.

| Decided × accepted | Readings |
|---|---|
| high × low|high | 9 |
| high × none|low | 3 |
| low × low | 12 |
| low × low|none | 9 |
| none × low | 3 |
| none × none | 6 |
| none × none|low | 24 |

| Trial | Turn | Seeds | Seeds + teacher | Recall (source) | Miss | Label distance | Teacher |
|---|---|---|---|---|---|---|---|
| 1 | cal-weather-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-weather-2.1 | low | low | low (seeds) | no_compatible_label | — | success |
| 1 | cal-weather-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-script-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 1 | cal-script-2.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 1 | cal-script-3.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 1 | cal-fact-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-fact-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-fact-3.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-reminder-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-reminder-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-reminder-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-translate-1.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 1 | cal-translate-2.1 | low | low | none (memory) | label | 0.091 | success |
| 1 | cal-percent-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 1 | cal-percent-2.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 1 | cal-greeting-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-greeting-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-thanks-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-thanks-1.2 | none | none | none (seeds) | no_compatible_label | — | — |
| 1 | cal-thanks-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 1 | cal-thanks-2.2 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-weather-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-weather-2.1 | low | low | low (seeds) | no_compatible_label | — | success |
| 2 | cal-weather-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-script-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 2 | cal-script-2.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 2 | cal-script-3.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 2 | cal-fact-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-fact-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-fact-3.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-reminder-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-reminder-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-reminder-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-translate-1.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 2 | cal-translate-2.1 | low | low | none (memory) | label | 0.091 | success |
| 2 | cal-percent-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 2 | cal-percent-2.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 2 | cal-greeting-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-greeting-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-thanks-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-thanks-1.2 | none | none | none (seeds) | no_compatible_label | — | — |
| 2 | cal-thanks-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 2 | cal-thanks-2.2 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-weather-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-weather-2.1 | low | low | low (seeds) | no_compatible_label | — | success |
| 3 | cal-weather-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-script-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 3 | cal-script-2.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 3 | cal-script-3.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 3 | cal-fact-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-fact-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-fact-3.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-reminder-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-reminder-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-reminder-3.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-translate-1.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 3 | cal-translate-2.1 | low | low | none (memory) | label | 0.091 | success |
| 3 | cal-percent-1.1 | high | high | high (seeds) | no_compatible_label | — | success |
| 3 | cal-percent-2.1 | none | none | none (seeds) | no_compatible_label | — | success |
| 3 | cal-greeting-1.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-greeting-2.1 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-thanks-1.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-thanks-1.2 | none | none | none (seeds) | no_compatible_label | — | — |
| 3 | cal-thanks-2.1 | low | low | low (seeds) | no_compatible_label | — | — |
| 3 | cal-thanks-2.2 | none | none | none (seeds) | no_compatible_label | — | — |
