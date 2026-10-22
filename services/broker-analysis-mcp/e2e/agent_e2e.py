"""Agent-driven end-to-end validation.

A real agent (Claude Code, headless) gets an analyst's question in Italian, with only this MCP
server and the project's skills available. Each run is scored from its transcript:

  tools      2 pts  the expected tool was called with the expected key arguments
  skill      1 pt   the expected skill was loaded
  grounded   4 pts  share of the answer's decimal numbers that match a tool result (rounding allowed)
  coverage   2 pts  share of the required metrics whose values appear in the answer
  caveats    1 pt   required facts (source, window, warnings) are stated

Usage: uv run python e2e/agent_e2e.py [--only KEY] [--out DIR]
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
NUMBER = re.compile(r"(?<![\w.])[-\u2212]?\d{1,3}(?:[.\s']\d{3})*(?:,\d+)?(?![\w])|(?<![\w.])[-\u2212]?\d+(?:[.,]\d+)?(?![\w])")


@dataclass
class Scenario:
    key: str
    question: str
    tool: str
    args: dict
    skill: str | None  # None: no data comes back, so there is nothing for a skill to interpret
    metrics: list[str]
    caveats: list[str] = field(default_factory=list)
    expect_error: bool = False


SCENARIOS = [
    Scenario(
        "rischio_eni",
        "Analizza il rischio di ENI (ENI.MI) rispetto al FTSE MIB negli ultimi 3 anni: volatilità, "
        "Sharpe con un tasso privo di rischio del 2%, drawdown massimo, VaR ed expected shortfall al 95%, beta.",
        "risk_metrics",
        {"ticker": "ENI.MI", "benchmark": "FTSEMIB.MI", "risk_free_annual": 0.02},
        "analisi-rischio",
        ["volatility_close_to_close", "sharpe", "max_drawdown", "var_historical_1d", "expected_shortfall_1d", "beta"],
        ["yahoo", r"20\d\d"],
    ),
    Scenario(
        "valutazione_energia",
        "Confronta la valutazione di ENI (ENI.MI) con TotalEnergies (TTE.PA) e Shell (SHEL.L): "
        "multipli, redditività e leva. ENI è cara o a sconto?",
        "peer_comparison",
        {},
        "analisi-fondamentale",
        ["ev_ebitda", "pe"],
        ["yahoo", "USD|dollar|valuta|conver"],
    ),
    Scenario(
        "tecnica_apple",
        "Com'è messa tecnicamente Apple (AAPL)? Trend, momentum e livelli.",
        "technical_indicators",
        {"ticker": "AAPL"},
        "analisi-tecnica",
        ["rsi14", "sma200", "close"],
        ["yahoo|fonte"],
    ),
    Scenario(
        "etf_swda",
        "L'ETF SWDA quotato a Milano (SWDA.MI) replica bene l'MSCI World? Misuralo contro URTH.",
        "etf_analysis",
        {"ticker": "SWDA.MI", "benchmark": "URTH"},
        "selezione-etf",
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
        "analisi-parametrica",
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
        "analisi-parametrica",
        [],
        ["passat|nuov|run_hooks|cron|periodic", "aapl"],
    ),
    Scenario(
        "ticker_inesistente",
        "Qual è il P/E della società con ticker NOTATICKER123?",
        "fundamental_ratios",
        {"ticker": "NOTATICKER123"},
        None,
        [],
        ["non .{0,40}(disponibil|trovat|esist|riconosc)|nessun|errore"],
        expect_error=True,
    ),
]


def _to_float(token: str) -> float | None:
    t = token.replace("\u2212", "-").replace("\u202f", "").replace(" ", "").replace("'", "")
    if "," in t:
        t = t.replace(".", "").replace(",", ".")
    elif t.count(".") > 1:
        t = t.replace(".", "")
    try:
        return float(t)
    except ValueError:
        return None


def _tool_numbers(results: list) -> list[float]:
    found = []

    def walk(x):
        if isinstance(x, dict):
            for v in x.values():
                walk(v)
        elif isinstance(x, list):
            for v in x:
                walk(v)
        elif isinstance(x, (int, float)) and not isinstance(x, bool):
            found.append(float(x))
            found.append(float(x) * 100)  # a fraction the agent may quote as a percentage
        elif isinstance(x, str):
            found.extend(float(n) for n in re.findall(r"\d+\.\d+|\d+", x))  # e.g. FX rates in warnings

    for r in results:
        walk(r)
    return found


def _matches(value: float, pool: list[float], decimals: int = 2) -> bool:
    """A quoted number matches a tool number if it equals it once rounded to the digits shown."""
    tolerance = 0.5 * 10 ** (-decimals) + 1e-9
    return any(abs(abs(value) - abs(p)) <= tolerance for p in pool)


def _answer_decimals(text: str) -> list[tuple[float, int]]:
    """Decimal numbers quoted in the answer, with the number of digits after the decimal mark."""
    values = []
    for token in NUMBER.findall(text):
        v = _to_float(token)
        if v is None or re.fullmatch(r"20\d\d[-.]\d\d", token):
            continue
        mark = "," if "," in token else ("." if token.count(".") == 1 else None)
        if mark:
            values.append((v, len(token.rsplit(mark, 1)[1])))
    return values


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
    decimals = _answer_decimals(run["answer"])
    ungrounded = [v for v, nd in decimals if not _matches(v, pool, nd)]
    grounded_share = 1.0 if not decimals else 1 - len(ungrounded) / len(decimals)
    if scenario.expect_error:
        grounded_share = 1.0 if not decimals else grounded_share
    grounded = 4.0 * grounded_share

    covered = sum(
        1 for m in scenario.metrics
        if any(_matches(q, [v, v * 100], nd) for v in _metric_values(run["results"], m) for q, nd in decimals)
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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--only")
    parser.add_argument("--out", default=str(ROOT / "e2e" / "runs"))
    args = parser.parse_args()
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
            (out / f"{sc.key}.json").write_text(json.dumps({"scenario": sc.__dict__, "run": run, "score": s}, ensure_ascii=False, indent=2))
            print(json.dumps(s, ensure_ascii=False))
    mean = sum(s["total"] for s in scores) / len(scores)
    print(f"MEAN SCORE {mean:.2f} / 10 over {len(scores)} scenarios")


if __name__ == "__main__":
    main()
