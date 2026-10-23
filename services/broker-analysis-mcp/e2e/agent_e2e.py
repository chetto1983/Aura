"""Agent-driven end-to-end validation.

A real agent (Claude Code, headless) gets an analyst's question in Italian, with only this MCP
server and the project's skills available. Each run is scored from its transcript:

  tools      2 pts  the expected tool was called with the expected key arguments
  skill      1 pt   the expected skill was loaded
  grounded   4 pts  share of the answer's decimal numbers that match a tool result (rounding allowed)
  coverage   2 pts  share of the required metrics whose values appear in the answer
  caveats    1 pt   required facts (source, window, warnings) are stated

Usage: uv run python e2e/agent_e2e.py [--only KEY] [--out DIR]
       uv run python e2e/agent_e2e.py --rescore DIR   (score saved runs again, no agent)
"""

import argparse
import json
import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVER = "broker-analysis"
# A minus may be typed as a hyphen, a dash or U+2212; numbers joined by ",digit" are a parameter
# list, as in MACD (12,26,9).
NUMBER = re.compile(
    r"(?<![\w.])(?<!\d,)[-\u2010-\u2013\u2212]?\d{1,3}(?:[.\s']\d{3})*(?:,\d+)?(?!\w|[.,]\d)"
    r"|(?<![\w.])(?<!\d,)[-\u2010-\u2013\u2212]?\d+(?:[.,]\d+)?(?!\w|,\d)"
)
MINUS = str.maketrans(dict.fromkeys("\u2010\u2011\u2012\u2013\u2212", "-"))
SCALE = re.compile(r"\s*(?:(miliard|mld|bn\b|billion)|(milion|mln|million))", re.I)


@dataclass
class Scenario:
    key: str
    question: str
    tool: str
    args: dict
    skill: str | None  # None: no data comes back, so there is nothing for a skill to interpret
    metrics: list[str]
    caveats: list[str] = field(default_factory=list)


SCENARIOS = [
    Scenario(
        "rischio_eni",
        "Analizza il rischio di ENI (ENI.MI) rispetto al FTSE MIB negli ultimi 3 anni: volatilità, "
        "Sharpe con un tasso privo di rischio del 2%, drawdown massimo, VaR ed expected shortfall al 95%, beta.",
        "risk_metrics",
        {"ticker": "ENI.MI", "benchmark": "FTSEMIB.MI", "risk_free_annual": 0.02},
        "risk-analysis",
        ["volatility_close_to_close", "sharpe", "max_drawdown", "var_historical_1d", "expected_shortfall_1d", "beta"],
        ["yahoo", r"20\d\d"],
    ),
    Scenario(
        "valutazione_energia",
        "Confronta la valutazione di ENI (ENI.MI) con TotalEnergies (TTE.PA) e Shell (SHEL.L): "
        "multipli, redditività e leva. ENI è cara o a sconto?",
        "peer_comparison",
        {},
        "fundamental-analysis",
        ["ev_ebitda", "pe"],
        ["yahoo", "USD|dollar|valuta|conver"],
    ),
    Scenario(
        "tecnica_apple",
        "Com'è messa tecnicamente Apple (AAPL)? Trend, momentum e livelli.",
        "technical_indicators",
        {"ticker": "AAPL"},
        "technical-analysis",
        ["rsi14", "sma200", "close"],
        ["yahoo|fonte"],
    ),
    Scenario(
        "etf_swda",
        "L'ETF SWDA quotato a Milano (SWDA.MI) replica bene l'MSCI World? Misuralo contro URTH.",
        "etf_analysis",
        {"ticker": "SWDA.MI", "benchmark": "URTH"},
        "etf-selection",
        ["ter", "tracking_difference", "tracking_error"],
        ["fuso|orari|chiusur|asincron|asynchron", "segno|ETF.{0,3}\u2212.{0,3}benchmark|meno"],
    ),
    Scenario(
        "grafico_condizione_eni",
        "Fammi un grafico di ENI (ENI.MI) degli ultimi 2 anni con medie mobili a 50 e 200 giorni, "
        "Bollinger a 20 e RSI a 14. Segna i giorni in cui l'RSI scende sotto 35 mentre il prezzo è sopra "
        "la media a 200: quante volte è successo e cosa è successo nelle 20 sedute successive?",
        "custom_analysis",
        {"ticker": "ENI.MI"},
        "custom-analysis",
        ["series_last_values.rsi14", "conditions.0.forward_returns.20d.after_events.mean_pct",
         "conditions.0.forward_returns.20d.all_bars.mean_pct"],
        ["yahoo", "aneddot|significativ|t.stat|baseline|tutte le sedute|caso", "grafico|chart|png"],
    ),
    Scenario(
        "hook_aapl",
        "Crea un avviso che mi segnali quando Apple (AAPL) torna sopra la media mobile a 50 giorni, "
        "salvando gli avvisi su file.",
        "create_hook",
        {"ticker": "AAPL"},
        "custom-analysis",
        [],
        ["passat|nuov|run_hooks|cron|periodic", "aapl"],
    ),
    Scenario(
        "beta_capture_eni",
        "Qual è il beta di ENI (ENI.MI) contro il FTSE MIB calcolato come su Bloomberg? E come si comporta ENI "
        "nei mesi in cui l'indice sale e in quelli in cui scende? Usa gli ultimi 3 anni.",
        "risk_metrics",
        {"ticker": "ENI.MI", "benchmark": "FTSEMIB.MI"},
        "risk-analysis",
        ["beta_weekly_2y", "beta_adjusted", "up_capture", "down_capture"],
        ["yahoo", r"\d+\s+mesi"],
    ),
    Scenario(
        "qualita_stellantis",
        "Fai un controllo della qualità del bilancio di Stellantis (STLAM.MI): solidità finanziaria, rischio di "
        "dissesto e segnali di manipolazione degli utili.",
        "fundamental_ratios",
        {"ticker": "STLAM.MI"},
        "fundamental-analysis",
        ["altman_z", "altman_z_double_prime"],
        ["piotroski", "beneish", "margine lordo|gross margin|gmi", "yahoo"],
    ),
    Scenario(
        "rischio_bitcoin",
        "Quanto è rischioso il Bitcoin (BTC-USD) rispetto all'S&P 500 negli ultimi 3 anni? Volatilità, drawdown "
        "massimo, VaR al 95%, beta e come si comporta nei mesi in cui la borsa sale e in quelli in cui scende.",
        "risk_metrics",
        {"ticker": "BTC-USD", "benchmark": "^GSPC"},
        "risk-analysis",
        ["volatility_close_to_close", "max_drawdown", "var_historical_1d", "up_capture", "down_capture"],
        ["yahoo", "365|ogni giorno|tutti i giorni|7 giorni|fine settimana|weekend"],
    ),
    Scenario(
        "tecnica_bitcoin",
        "Com'è messo tecnicamente il Bitcoin (BTC-USD)? Trend, momentum e distanza dai massimi e dai minimi "
        "dell'ultimo anno.",
        "technical_indicators",
        {"ticker": "BTC-USD"},
        "technical-analysis",
        ["rsi14", "price_vs_sma200", "price_vs_high_52w"],
        ["yahoo|fonte"],
    ),
    Scenario(
        "ticker_inesistente",
        "Qual è il P/E della società con ticker NOTATICKER123?",
        "fundamental_ratios",
        {"ticker": "NOTATICKER123"},
        None,
        [],
        ["non .{0,40}(disponibil|trovat|esist|riconosc)|nessun|errore"],
    ),
]


def _to_float(token: str) -> float | None:
    t = token.translate(MINUS).replace("\u202f", "").replace(" ", "").replace("'", "")
    if "," in t:
        t = t.replace(".", "").replace(",", ".")
    elif t.count(".") > 1:
        t = t.replace(".", "")
    try:
        return float(t)
    except ValueError:
        return None


# Metrics the tools report as positive losses, which an answer may quote with a minus sign.
LOSSES = {"var_historical_1d", "expected_shortfall_1d", "var_cornish_fisher_1d", "max_drawdown", "cdar"}


def _signed(value: float, loss: bool) -> list[float]:
    """The value and its percentage reading, and their negatives for a loss."""
    readings = [value, value * 100]  # a fraction the agent may quote as a percentage
    return readings + [-r for r in readings] if loss else readings


def _tool_numbers(results: list) -> list[float]:
    found = []

    def walk(x, loss=False):
        if isinstance(x, dict):
            for k, v in x.items():
                walk(v, loss or k in LOSSES)
        elif isinstance(x, list):
            for v in x:
                walk(v, loss)
        elif isinstance(x, (int, float)) and not isinstance(x, bool):
            found.extend(_signed(float(x), loss))
        elif isinstance(x, str):  # prose such as FX rates in warnings, where a hyphen may be a minus
            for sign, digits in re.findall(r"([-\u2212]?)(\d+\.\d+|\d+)", x):
                found.extend([float(digits), -float(digits)] if sign else [float(digits)])

    for r in results:
        walk(r)
    return found


def _matches(value: float, pool: list[float], decimals: int = 2) -> bool:
    """A quoted number matches a tool number, sign included, if it equals it once rounded to the digits shown."""
    tolerance = 0.5 * 10 ** (-decimals) + 1e-9
    return any(abs(value - p) <= tolerance for p in pool)


def _answer_decimals(text: str) -> list[list[tuple[float, int]]]:
    """Decimal numbers quoted in the answer, each as its readings: (value, digits after the mark).

    In Italian 81.792 may be 81792, and 15,8 miliardi is 15.8e9 shown to the nearest 1e8. A number
    written without a sign may also be negative, the sign being in words ("8,2 punti in meno"); one
    written with a sign is read only with it.
    """
    quoted = []
    for m in NUMBER.finditer(text):
        token = m.group()
        v = _to_float(token)
        if v is None or re.fullmatch(r"20\d\d[-.]\d\d", token):
            continue
        mark = "," if "," in token else ("." if token.count(".") == 1 else None)
        if not mark:
            continue
        digits = len(token.rsplit(mark, 1)[1])
        readings = [(v, digits)]
        if mark == "." and digits == 3:
            readings.append((v * 1000, 0))
        scale = SCALE.match(text, m.end())
        if scale:
            exponent = 9 if scale.group(1) else 6
            readings.append((v * 10**exponent, digits - exponent))
        if token[0].isdigit() and text[m.start() - 1 : m.start()] != "+":
            readings += [(-r, d) for r, d in readings]
        quoted.append(readings)
    return quoted


def _resolve(obj, path: str):
    for part in path.split("."):
        if isinstance(obj, list) and part.isdigit() and int(part) < len(obj):
            obj = obj[int(part)]
        elif isinstance(obj, dict) and part in obj:
            obj = obj[part]
        else:
            return None
    return obj


def _metric_values(results: list, name: str) -> list[float]:
    """Values of a required metric: a dotted path into a result, or a key of its "metrics"."""
    values = []
    for r in results:
        reports = r.get("companies", {}).values() if isinstance(r, dict) and "companies" in r else [r]
        for rep in reports:
            found = _resolve(rep, name) if "." in name else _resolve(rep, f"metrics.{name}.value")
            if isinstance(found, (int, float)) and not isinstance(found, bool):
                values.append(float(found))
    return values


def run_agent(scenario: Scenario, workdir: Path, mcp_config: Path) -> dict:
    cmd = [
        "claude", "-p", scenario.question,
        "--output-format", "stream-json", "--verbose",
        "--mcp-config", str(mcp_config), "--strict-mcp-config",
        "--allowedTools", f"mcp__{SERVER}__*", "Skill",
        "--disallowedTools", "Bash", "WebSearch", "WebFetch", "Write", "Edit",
        "--append-system-prompt", "Rispondi in italiano all'analista del broker.",
    ]
    proc = subprocess.run(cmd, cwd=workdir, capture_output=True, text=True, timeout=600)
    events = [json.loads(line) for line in proc.stdout.splitlines() if line.startswith("{")]
    calls, results, skills, answer = [], [], [], ""
    for ev in events:
        content = (ev.get("message") or {}).get("content") or []
        for block in content if isinstance(content, list) else []:
            if block.get("type") == "tool_use":
                if block["name"] == "Skill":
                    skills.append(str(block.get("input", {}).get("skill") or block.get("input", {}).get("command")))
                else:
                    calls.append({"name": block["name"], "input": block.get("input", {})})
            if block.get("type") == "tool_result":
                raw = block.get("content")
                text = raw if isinstance(raw, str) else " ".join(c.get("text", "") for c in raw or [] if isinstance(c, dict))
                try:
                    # A tool that returns an image is followed by an "[Image: source: ...]" text block.
                    results.append(json.JSONDecoder().raw_decode(text.strip())[0])
                except (json.JSONDecodeError, TypeError, AttributeError):
                    results.append({"text": text, "is_error": block.get("is_error")})
        if ev.get("type") == "result":
            answer = ev.get("result") or ""
    return {"calls": calls, "results": results, "skills": skills, "answer": answer, "stderr": proc.stderr[-2000:]}


def score(scenario: Scenario, run: dict) -> dict:
    tool_name = f"mcp__{SERVER}__{scenario.tool}"
    matching = [c for c in run["calls"] if c["name"] == tool_name]
    args_ok = any(all(str(c["input"].get(k)) == str(v) for k, v in scenario.args.items()) for c in matching)
    tools = 2.0 if matching and args_ok else (1.0 if matching else 0.0)
    skill = 1.0 if scenario.skill is None or any(scenario.skill in s for s in run["skills"]) else 0.0

    pool = [*_tool_numbers(run["results"]), 2.0, 3.0, 95.0, 99.0, 0.02, 14.0, 50.0, 200.0, 70.0, 30.0, 52.0, 12.0, 26.0, 9.0, 20.0]
    quoted = _answer_decimals(run["answer"])
    ungrounded = [readings[0][0] for readings in quoted if not any(_matches(v, pool, nd) for v, nd in readings)]
    grounded = 4.0 * (1.0 if not quoted else 1 - len(ungrounded) / len(quoted))

    covered = sum(
        1 for m in scenario.metrics
        if any(
            _matches(q, _signed(v, m in LOSSES), nd)
            for v in _metric_values(run["results"], m) for readings in quoted for q, nd in readings
        )
    )
    coverage = 2.0 if not scenario.metrics else 2.0 * covered / len(scenario.metrics)

    caveat_hits = [bool(re.search(p, run["answer"], re.I)) for p in scenario.caveats]
    caveats = 1.0 if not caveat_hits else sum(caveat_hits) / len(caveat_hits)

    total = tools + skill + grounded + coverage + caveats
    return {
        "key": scenario.key,
        "total": round(total, 2),
        "tools": tools,
        "skill": skill,
        "grounded": round(grounded, 2),
        "coverage": round(coverage, 2),
        "caveats": round(caveats, 2),
        "ungrounded_numbers": ungrounded,
        "skills_loaded": run["skills"],
        "tool_calls": [c["name"].split("__")[-1] + json.dumps(c["input"], ensure_ascii=False) for c in run["calls"]],
    }


def _print_mean(scores: list[dict]) -> None:
    mean = sum(s["total"] for s in scores) / len(scores)
    print(f"MEAN SCORE {mean:.2f} / 10 over {len(scores)} scenarios")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--only")
    parser.add_argument("--out", default=str(ROOT / "e2e" / "runs"))
    parser.add_argument("--rescore", metavar="DIR", help="score the runs saved in DIR again, without the agent")
    args = parser.parse_args()
    if args.rescore:
        fields = Scenario.__dataclass_fields__
        scores = []
        for path in sorted(Path(args.rescore).glob("*.json")):
            saved = json.loads(path.read_text(encoding="utf-8"))
            scenario = Scenario(**{k: v for k, v in saved["scenario"].items() if k in fields})
            scores.append(score(scenario, saved["run"]))
            print(json.dumps(scores[-1], ensure_ascii=False))
        _print_mean(scores)
        return
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        shutil.copytree(ROOT / "skills", work / ".claude" / "skills")
        mcp_config = work / "mcp.json"
        mcp_config.write_text(json.dumps({"mcpServers": {SERVER: {
            "command": sys.executable, "args": ["-m", "broker_analysis_mcp.server"], "cwd": str(ROOT),
            "env": {"BROKER_ANALYSIS_HOME": str(work / "home")},
        }}}))
        scores = []
        for sc in SCENARIOS:
            if args.only and sc.key != args.only:
                continue
            run = run_agent(sc, work, mcp_config)
            s = score(sc, run)
            scores.append(s)
            saved = {"scenario": sc.__dict__, "run": run, "score": s}
            (out / f"{sc.key}.json").write_text(json.dumps(saved, ensure_ascii=False, indent=2), encoding="utf-8")
            print(json.dumps(s, ensure_ascii=False))
    _print_mean(scores)


if __name__ == "__main__":
    main()
