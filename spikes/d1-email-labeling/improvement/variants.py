"""Fixed prompt candidates for a development-only sweep; no changes to model weights."""

import copy
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import english_prompt


LABELS = list(english_prompt.benchmark.CRITERIA)
KEYS = ["customer_service", "internal_work", "personal", "professional_opportunity",
        "billing_paperwork", "parcel_delivery", "marketing", "system_notification"]
SHORT_EN = [
    "A customer's product question, quote request, reservation, complaint or technical support request.",
    "Colleagues coordinating internal tasks, meetings, projects or work schedules.",
    "Private messages about family, friends, school, health, hobbies or leisure.",
    "A personalized offer of a new job, paid assignment, partnership, investment or project collaboration.",
    "Invoices, receipts, taxes, payroll, payments, reimbursements or official paperwork.",
    "Delivery, tracking, pickup, return or customs clearance of an actual parcel.",
    "Mass advertising, promotional offers, public newsletters or selling a course or subscription.",
    "Automated account-security, password, backup, monitoring or service-status messages.",
]
SHORT_IT = [
    "Un cliente chiede informazioni, un preventivo, una prenotazione o assistenza per un prodotto.",
    "Colleghi che organizzano attività interne, progetti, riunioni o turni.",
    "Vita privata: famiglia, amici, scuola, salute, hobby e tempo libero.",
    "Una proposta personale di nuovo lavoro, incarico, collaborazione, investimento o partnership.",
    "Fatture, ricevute, imposte, stipendi, pagamenti, rimborsi e pratiche documentali.",
    "Consegna, tracciamento, ritiro, reso o sdoganamento di un pacco reale.",
    "Pubblicità generica, sconti, newsletter pubbliche e vendita di corsi o abbonamenti.",
    "Avvisi automatici su account, password, sicurezza, backup e stato dei servizi.",
]


def candidates(examples):
    result = {}
    baseline = copy.deepcopy(english_prompt.benchmark.QUESTION)
    identity = dict(zip(LABELS, LABELS))
    result["baseline_english"] = {"question": baseline, "label_map": identity}
    for name, keys, descriptions, instructions in [
        ("short_english", LABELS, SHORT_EN, "Which category best describes the main purpose of this email?"),
        ("english_keys", KEYS, SHORT_EN, "Which category best describes the main purpose of this email?"),
        ("short_italian", LABELS, SHORT_IT, "Quale categoria descrive meglio lo scopo principale di questa email?"),
    ]:
        result[name] = {
            "question": {"type": "choice", "instructions": instructions, "criteria": dict(zip(keys, descriptions))},
            "label_map": dict(zip(keys, LABELS)),
        }
    for source, name in [("short_english", "examples_italian_keys"), ("english_keys", "examples_english_keys")]:
        config = copy.deepcopy(result[source])
        reverse = {label: key for key, label in config["label_map"].items()}
        demo = "\n\nLabeled examples (Italian email => category):\n"
        for case in examples:
            demo += f"Subject: {case['subject']}\n{case['body']}\nCategory: {reverse[case['label']]}\n\n"
        config["question"]["instructions"] += demo
        result[name] = config
    return result
