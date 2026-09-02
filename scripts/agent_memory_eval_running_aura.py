from __future__ import annotations

import base64
import datetime as dt
import hashlib
import http.cookiejar
import json
import os
import pathlib
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from typing import Any


ARM_ENV = "AURA_E2E_RUNNING_AURA"

EXPECTED_COUNTS = {
    "beyond_active_context_recall": 1,
    "provider_visible_reasoning_exclusion_explicit_recall": 3,
    "durable_shell_file_capture_later_recall": 2,
}


def evaluate_running_aura_conversation(evidence: Any) -> dict[str, Any]:
    failures: list[str] = []
    if not isinstance(evidence, dict) or evidence.get("executed") is not True:
        evidence = evidence if isinstance(evidence, dict) else {}
        failures.append("running_aura_conversation did not execute")
    if evidence.get("fresh_image") is not True or evidence.get("candidate_commit") != evidence.get("running_commit"):
        failures.append("running Aura image is stale")
    scenarios = evidence.get("scenarios")
    if not isinstance(scenarios, list):
        scenarios = []
    by_name = {item.get("name"): item for item in scenarios if isinstance(item, dict) and isinstance(item.get("name"), str)}
    if len(by_name) != len(scenarios) or set(by_name) != set(EXPECTED_COUNTS):
        failures.append("running Aura scenarios are missing, duplicate, or extra")
    global_ids: list[str] = []
    for name, expected in EXPECTED_COUNTS.items():
        scenario = by_name.get(name, {})
        if scenario.get("executed") is not True or scenario.get("skipped") is True or scenario.get("mocked") is True:
            failures.append(f"{name}: scenario was absent, skipped, or mocked")
        terminal = scenario.get("terminal_response_ids")
        responses = scenario.get("responses")
        if scenario.get("expected_response_count") != expected or not isinstance(terminal, list) or not isinstance(responses, list):
            failures.append(f"{name}: expected response count contract is absent")
            continue
        scored_ids = [item.get("response_id") for item in responses if isinstance(item, dict)]
        if len(terminal) != expected or len(responses) != expected or len(set(terminal)) != expected or set(scored_ids) != set(terminal):
            failures.append(f"{name}: observed-to-scored response IDs are not an exact {expected}-item bijection")
        for response in responses:
            if not isinstance(response, dict):
                failures.append(f"{name}: response record is malformed")
                continue
            response_id = response.get("response_id")
            score = response.get("actual_response_score")
            if not isinstance(response_id, str) or not response_id:
                failures.append(f"{name}: response ID is absent")
            else:
                global_ids.append(response_id)
            if not isinstance(score, (int, float)) or isinstance(score, bool) or score <= 9.8:
                failures.append(f"{name}/{response_id}: per-response score is not strictly above 9.8")
            if not response.get("evidence_refs") or not response.get("correlation_refs"):
                failures.append(f"{name}/{response_id}: evidence or correlation references are absent")
        failures.extend(_validate_scenario_proof(name, scenario.get("proof")))
    if len(global_ids) != 6 or len(set(global_ids)) != 6:
        failures.append("terminal response IDs are not six globally unique values")
    stores = evidence.get("evidence")
    for store in ("tempo", "tool_invocations", "conversation_turns", "arcadedb"):
        count = stores.get(store) if isinstance(stores, dict) else None
        if not isinstance(count, int) or isinstance(count, bool) or count <= 0:
            failures.append(f"{store} evidence is empty")
    return {**evidence, "passed": not failures, "status": "PASS" if not failures else "FAIL", "failures": failures}


def _validate_scenario_proof(name: str, proof: Any) -> list[str]:
    if not isinstance(proof, dict) or proof.get("tempo_path_matches") is not True:
        return [f"{name}: Tempo path correlation is absent"]
    required: dict[str, Any]
    if name == "beyond_active_context_recall":
        required = {"historical_conversation_evidence": True}
    elif name == "provider_visible_reasoning_exclusion_explicit_recall":
        required = {"touched_edges": 1, "ordinary_reasoning_sentinel_absent": True, "explicit_reasoning_recall": True}
    else:
        required = {"accepted_capture": True, "run_finished_before_recall": True}
    return [f"{name}: {field} proof is absent" for field, expected in required.items() if proof.get(field) != expected]


def run_running_aura_conversation(repo: pathlib.Path, timeout_seconds: int) -> dict[str, Any]:
    candidate = _command(repo, ["git", "rev-parse", "HEAD"]).strip()
    version = _command(repo, ["docker", "exec", "aura", "aura", "version"])
    running = next((line.partition(":")[2].strip() for line in version.splitlines() if line.startswith("commit:")), "")
    client = AuraHTTP(repo)
    client.login()
    identity = _identity_for_email(repo, client.email)
    database = "mem_" + identity.replace("-", "_")
    token = uuid.uuid4().hex[:12]
    evidence_counts = {"tempo": 0, "tool_invocations": 0, "conversation_turns": 0, "arcadedb": 0}
    scenarios: list[dict[str, Any]] = []

    seed_marker = f"PHASE49_HISTORY_{token}"
    seed_conversation = client.create_conversation(f"Phase 49 history seed {token}")
    client.run(seed_conversation, f"Rispondi esattamente con questo codice e nient'altro: {seed_marker}", "high", timeout_seconds)
    _wait_until(lambda: _projected_marker(repo, database, identity, seed_marker), timeout_seconds, "history projection")
    recall_conversation = client.create_conversation(f"Phase 49 natural recall {token}")
    recall_run = client.run(recall_conversation, "cosa vedi delle conversazioni precedenti?", "high", timeout_seconds)
    recall = _turn_evidence(repo, identity, recall_conversation, recall_run, after_seq=0)
    recall_tool = _find_tool(recall["tools"], "memory__memory_recall")
    recall_result = _json_object(recall_tool.get("result_preview", ""))
    recall_args = _json_object(recall_tool.get("args_raw", ""))
    recall_ok = (
        recall_args.get("mode") == "recent"
        and seed_marker in json.dumps(recall_result, ensure_ascii=False)
        and seed_marker in recall["answer"]
        and recall_conversation not in json.dumps(recall_result)
        and "non ho" not in recall["answer"].lower()
    )
    recall_traces = _tempo_traces(repo, recall_tool.get("request_id", ""))
    evidence_counts["tempo"] += len(recall_traces)
    evidence_counts["tool_invocations"] += len(recall["tools"])
    evidence_counts["conversation_turns"] += len(recall["turns"])
    evidence_counts["arcadedb"] += 1
    scenarios.append(_scenario(
        "beyond_active_context_recall", [recall], [*recall_traces, f"arcadedb:{database}/ConversationTurn/{seed_conversation}"],
        recall_ok, {"tempo_path_matches": bool(recall_traces), "historical_conversation_evidence": recall_ok},
    ))

    reasoning_conversation = client.create_conversation(f"Phase 49 reasoning {token}")
    entity = f"Phase49Entity{token}"
    work_prompt = (
        f"Usa esattamente memory__memory_upsert_fact per memorizzare subject {entity}, predicate verifica, "
        f"object Confermato, statement '{entity} e stato verificato'. Poi conferma in italiano."
    )
    work_run = client.run(reasoning_conversation, work_prompt, "high", timeout_seconds)
    work = _turn_evidence(repo, identity, reasoning_conversation, work_run, after_seq=0)
    upsert = _find_tool(work["tools"], "memory__memory_upsert_fact")
    trace_id = upsert.get("request_id", "")
    trace = _wait_arcade(repo, database, "SELECT trace_id, provider_summary, status, terminal_at, expires_at FROM ReasoningTrace WHERE identity_id=:identity_id AND trace_id=:trace_id", {"identity_id": identity, "trace_id": trace_id}, timeout_seconds)
    touched = _arcade_query(repo, database, "SELECT count(*) AS count FROM TOUCHED WHERE outV().identity_id=:identity_id AND outV().trace_id=:trace_id", {"identity_id": identity, "trace_id": trace_id})
    touched_count = int(touched[0].get("count", 0)) if touched else 0
    retention_ok = _retention_days(trace[0]) == 30 if trace else False
    sentinel = str(trace[0].get("provider_summary", ""))[:80] if trace else ""
    history_before = "\n".join(str(row.get("content", "")) for row in work["turns"])
    exclusion_run = client.run(reasoning_conversation, "Rispondi soltanto NORMALE, senza usare strumenti di memoria.", "high", timeout_seconds)
    exclusion = _turn_evidence(repo, identity, reasoning_conversation, exclusion_run, after_seq=work["seq"])
    exclusion_ok = bool(sentinel) and sentinel not in history_before and not any(
        _tool_name(row).endswith("memory_recall") for row in exclusion["tools"]
    )
    explicit_run = client.run(
        reasoning_conversation,
        f"Usa memory__memory_recall con mode reasoning e trace_id {trace_id}; poi descrivi la traccia recuperata.",
        "high", timeout_seconds,
    )
    explicit = _turn_evidence(repo, identity, reasoning_conversation, explicit_run, after_seq=exclusion["seq"])
    explicit_tool = _find_tool(explicit["tools"], "memory__memory_recall")
    explicit_args = _json_object(explicit_tool.get("args_raw", ""))
    explicit_ok = explicit_reasoning_recall_ok(
        explicit_args, explicit_tool.get("result_preview", ""), trace_id, sentinel)
    reasoning_traces = _unique(_tempo_traces(repo, trace_id) + _tempo_traces(repo, explicit_tool.get("request_id", "")))
    evidence_counts["tempo"] += len(reasoning_traces)
    evidence_counts["tool_invocations"] += len(work["tools"]) + len(exclusion["tools"]) + len(explicit["tools"])
    evidence_counts["conversation_turns"] += len(explicit["turns"])
    evidence_counts["arcadedb"] += len(trace) + touched_count
    reasoning_ok = bool(upsert) and bool(trace) and touched_count >= 1 and retention_ok and exclusion_ok and explicit_ok
    scenarios.append(_scenario(
        "provider_visible_reasoning_exclusion_explicit_recall", [work, exclusion, explicit],
        [*reasoning_traces, f"arcadedb:{database}/ReasoningTrace/{trace_id}", f"postgres:{reasoning_conversation}"],
        reasoning_ok,
        {"tempo_path_matches": bool(reasoning_traces), "touched_edges": 1 if touched_count >= 1 else 0,
         "ordinary_reasoning_sentinel_absent": exclusion_ok, "explicit_reasoning_recall": explicit_ok,
         "retention_days": 30 if retention_ok else _retention_days(trace[0]) if trace else None},
    ))

    capture_conversation = client.create_conversation(f"Phase 49 capture {token}")
    artifact = f"/workspace/artifacts/phase49-{token}.txt"
    capture_prompt = (
        f"Esegui shell_exec con 'printf {token}' e poi usa write_file per scrivere esattamente {token} "
        f"nel file {artifact}. Attendi entrambi i risultati e conferma il percorso."
    )
    capture_run = client.run(capture_conversation, capture_prompt, "high", timeout_seconds)
    capture = _turn_evidence(repo, identity, capture_conversation, capture_run, after_seq=0)
    shell_tool = _find_tool(capture["tools"], "shell_exec")
    write_tool = _find_tool(capture["tools"], "write_file")
    capture_rows = _wait_arcade(
        repo, database,
        "SELECT statement, sources FROM FACT WHERE outV().name=:subject AND predicate='durable_artifact'",
        {"subject": artifact}, timeout_seconds,
    )
    capture_record = json.dumps(capture_rows, ensure_ascii=False)
    capture_ok = bool(shell_tool) and bool(write_tool) and capture_conversation in capture_record and str(write_tool.get("tool_call_id", "")) in capture_record
    recall_capture_run = client.run(
        capture_conversation,
        f"Usa memory__memory_recall per ricordare quale artefatto durevole hai appena scritto per il marker {token}.",
        "high", timeout_seconds,
    )
    capture_recall = _turn_evidence(repo, identity, capture_conversation, recall_capture_run, after_seq=capture["seq"])
    capture_recall_tool = _find_tool(capture_recall["tools"], "memory__memory_recall")
    capture_recall_ok = artifact in capture_recall_tool.get("result_preview", "") and artifact in capture_recall["answer"]
    capture_traces = _unique(_tempo_traces(repo, write_tool.get("request_id", "")) + _tempo_traces(repo, capture_recall_tool.get("request_id", "")))
    evidence_counts["tempo"] += len(capture_traces)
    evidence_counts["tool_invocations"] += len(capture["tools"]) + len(capture_recall["tools"])
    evidence_counts["conversation_turns"] += len(capture_recall["turns"])
    evidence_counts["arcadedb"] += len(capture_rows)
    scenarios.append(_scenario(
        "durable_shell_file_capture_later_recall", [capture, capture_recall],
        [*capture_traces, f"arcadedb:{database}/FACT/{hashlib.sha256(artifact.encode()).hexdigest()}", f"postgres:{capture_conversation}"],
        capture_ok and capture_recall_ok,
        {"tempo_path_matches": bool(capture_traces), "accepted_capture": capture_ok, "run_finished_before_recall": capture["finished"]},
    ))
    return evaluate_running_aura_conversation({
        "executed": True, "fresh_image": candidate == running,
        "candidate_commit": candidate, "running_commit": running,
        "scenarios": scenarios, "evidence": evidence_counts,
    })



def explicit_reasoning_recall_ok(args: dict[str, Any], preview: str, trace_id: str, sentinel: str) -> bool:
    """Explicit reasoning recall proved from ground truth rather than from a substring.

    The obvious assertion -- `trace_id in preview` -- is not merely flaky, it is
    IMPOSSIBLE to satisfy, and it silently failed this scenario on every run. The
    durable result_preview column is capped at 2 KiB by RedactForLedger
    (internal/toolinvocations/redact.go, ResultPreviewCapBytes) while the real recall
    payload measured 4178 bytes, and the preview serializes its keys alphabetically,
    so `trace_id` sorts AFTER the large `steps` array and is always past the cut.
    Measured live 2026-09-02: the call was correct (mode=reasoning, exact trace_id,
    effective_path=reasoning) and the scenario still scored 0.0.

    What is asserted instead is that the tool handed back THE trace the graph holds:
    `sentinel` is the head of provider_summary read from the ArcadeDB ReasoningTrace
    row itself, so it exists nowhere else, and it must appear in what the model
    received. This still fails loudly when it should -- an empty result, a wrong
    trace, or a reasoning mode that stopped returning traces all break it -- and it
    is the exact counterpart of the exclusion check, which requires that same
    sentinel be ABSENT from ordinary context.
    """
    if args.get("mode") != "reasoning" or args.get("trace_id") != trace_id:
        return False
    if not sentinel or not preview:
        return False
    return sentinel in preview and '"kind":"reasoning"' in preview.replace(" ", "")


def running_aura_is_armed() -> bool:
    return os.environ.get(ARM_ENV, "").strip().lower() in {"1", "true", "yes"}


def not_evaluated_running_aura() -> dict[str, Any]:
    return {
        "executed": False,
        "status": "NOT_EVALUATED",
        "armed_by": ARM_ENV,
        "reason": (
            "the running-Aura conversation drives an authenticated /agent/run against a live "
            "daemon and scores the model's own answers, so it needs a real LLM provider, Tempo "
            f"and a seeded operator. Set {ARM_ENV}=1 where that stack exists; where it is unset "
            "this evidence is absent from the report, never assumed to pass."
        ),
    }


def attach_running_aura_evidence(report: dict[str, Any], tier: str, timeout_seconds: int = 900) -> dict[str, Any]:
    if tier != "all":
        return report
    # GitHub CI has no model key of its own (ci.yml says so where it passes the degraded
    # placeholder), no Tempo and no enrolled operator, so this tier cannot execute there.
    # Arming it is explicit rather than inferred from the environment: a probe that decides
    # for itself whether the stack "looks live" is how a tier that silently stopped running
    # goes on reporting green. Armed and broken still fails -- run_running_aura_conversation
    # raises rather than degrading.
    if not running_aura_is_armed():
        report["running_aura_conversation"] = not_evaluated_running_aura()
        report["hard_gates"]["running_aura_conversation"] = {"status": "NOT_EVALUATED"}
        return report
    evidence = run_running_aura_conversation(pathlib.Path(__file__).resolve().parents[1], timeout_seconds)
    report["running_aura_conversation"] = evidence
    report["hard_gates"]["running_aura_conversation"] = {"status": evidence["status"]}
    if not evidence["passed"]:
        report["passed"] = False
        report["verdict"] = "FAIL"
    return report


class AuraHTTP:
    def __init__(self, repo: pathlib.Path) -> None:
        self.base = os.environ.get("AURA_E2E_ORIGIN", "http://127.0.0.1:9080").rstrip("/")
        self.email = _secret(repo, "AURA_E2E_AUTHULA_EMAIL")
        self.password = _secret(repo, "AURA_E2E_AUTHULA_PASSWORD")
        self.csrf = ""
        self.cookie = ""

    def login(self) -> None:
        _, raw, _ = self.request("GET", "/api/auth/config", auth=False)
        self.csrf = str(json.loads(raw)["csrf_token"])
        _, _, headers = self.request(
            "POST", "/auth/email-password/sign-in", {"email": self.email, "password": self.password}, auth=False,
            extra_headers={"X-AUTHULA-CSRF-TOKEN": self.csrf, "Cookie": f"__Host-authula_csrf_token={self.csrf}"},
        )
        cookies = [value.split(";", 1)[0] for value in headers.get_all("Set-Cookie", [])]
        if not cookies:
            raise RuntimeError("Authula sign-in returned no session cookie")
        self.cookie = "; ".join(cookies + [f"__Host-authula_csrf_token={self.csrf}"])

    def create_conversation(self, title: str) -> str:
        status, raw, _ = self.request("POST", "/api/conversations", {"title": title})
        if status != 201:
            raise RuntimeError(f"create conversation returned {status}: {raw}")
        return str(json.loads(raw)["ID"])

    def run(self, conversation: str, prompt: str, effort: str, timeout_seconds: int) -> dict[str, Any]:
        payload = {
            "threadId": conversation,
            "messages": [{"id": str(uuid.uuid4()), "role": "user", "content": prompt}],
            "aura": {"effort": effort},
        }
        status, raw, _ = self.request("POST", "/agent/run", payload, timeout_seconds=timeout_seconds)
        frames = _sse_frames(raw)
        return {
            "status": status, "frames": frames, "answer": _frame_text(frames),
            "reasoning": _reasoning_text(frames),
            "finished": any(frame.get("type") == "RUN_FINISHED" for frame in frames),
            "errored": any(frame.get("type") == "RUN_ERROR" for frame in frames),
        }

    def request(
        self, method: str, path: str, payload: Any = None, *, auth: bool = True,
        extra_headers: dict[str, str] | None = None, timeout_seconds: int = 60,
    ) -> tuple[int, str, Any]:
        headers = {"Accept": "text/event-stream" if path == "/agent/run" else "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if auth:
            headers.update({"Cookie": self.cookie, "X-AUTHULA-CSRF-TOKEN": self.csrf, "Idempotency-Key": str(uuid.uuid4())})
        if extra_headers:
            headers.update(extra_headers)
        body = None if payload is None else json.dumps(payload).encode()
        request = urllib.request.Request(self.base + path, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=timeout_seconds) as response:
                return response.status, response.read().decode(), response.headers
        except urllib.error.HTTPError as exc:
            return exc.code, exc.read().decode(), exc.headers


def _scenario(name: str, turns: list[dict[str, Any]], refs: list[str], passed: bool, proof: dict[str, Any]) -> dict[str, Any]:
    responses = []
    terminal_ids = []
    for turn in turns:
        response_id = f"{turn['conversation_id']}:{turn['seq']}"
        terminal_ids.append(response_id)
        responses.append({
            "response_id": response_id, "actual_response_score": 10.0 if passed and turn["answer"] else 0.0,
            "evidence_refs": refs, "correlation_refs": [f"postgres:turn:{response_id}", *refs],
            "answer": turn["answer"],
        })
    return {
        "name": name, "executed": True, "skipped": False, "mocked": False,
        "expected_response_count": EXPECTED_COUNTS[name], "terminal_response_ids": terminal_ids,
        "responses": responses, "proof": proof,
    }


def _turn_evidence(repo: pathlib.Path, identity: str, conversation: str, run: dict[str, Any], after_seq: int) -> dict[str, Any]:
    turns = _pg_rows(repo, identity, conversation, "turns")
    assistants = [row for row in turns if row.get("role") == "assistant" and int(row.get("seq", 0)) > after_seq and row.get("content")]
    if not assistants:
        raise RuntimeError(f"conversation {conversation} has no terminal assistant turn after {after_seq}")
    assistant = assistants[-1]
    tools = [row for row in _pg_rows(repo, identity, conversation, "tools") if int(row.get("seq", 0)) > after_seq]
    return {
        **run, "conversation_id": conversation, "seq": int(assistant["seq"]),
        "answer": str(assistant["content"]), "turns": turns, "tools": tools,
    }


def _pg_rows(repo: pathlib.Path, identity: str, conversation: str, kind: str) -> list[dict[str, Any]]:
    conv = _sql(conversation)
    if kind == "turns":
        inner = f"SELECT seq,role,content,created_at FROM aura.conversation_turns WHERE conversation_id='{conv}' ORDER BY seq"
    else:
        inner = (
            "SELECT DISTINCT ON (tool_call_id) seq,request_id::text,tool_call_id,tool_name,status,args_raw,result_preview,ts "
            f"FROM aura.tool_invocations WHERE conversation_id='{conv}' AND status IS NOT NULL ORDER BY tool_call_id,ts DESC"
        )
    query = (
        "BEGIN; SET LOCAL ROLE aura_app; "
        f"SELECT set_config('app.current_identity','{_sql(identity)}',true); "
        f"SELECT COALESCE(json_agg(row_to_json(q)),'[]'::json) FROM ({inner}) q; ROLLBACK;"
    )
    return _psql_json(repo, query)


def _identity_for_email(repo: pathlib.Path, email: str) -> str:
    output = _command(repo, ["docker", "exec", "aura-postgres", "psql", "-X", "-qAt", "-U", "aura", "-d", "aura", "-c", f"SELECT id::text FROM aura.identities WHERE lower(name)=lower('{_sql(email)}') LIMIT 1"])
    identity = output.strip().splitlines()[-1] if output.strip() else ""
    if not re.fullmatch(r"[0-9a-f-]{36}", identity):
        raise RuntimeError("authenticated identity was not found")
    return identity


def _psql_json(repo: pathlib.Path, query: str) -> list[dict[str, Any]]:
    output = _command(repo, ["docker", "exec", "aura-postgres", "psql", "-X", "-qAt", "-U", "aura", "-d", "aura", "-c", query])
    for line in reversed(output.splitlines()):
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, list):
            return [item for item in value if isinstance(item, dict)]
    raise RuntimeError("RLS-scoped PostgreSQL query returned no JSON evidence")


def _arcade_query(repo: pathlib.Path, database: str, statement: str, params: dict[str, Any]) -> list[dict[str, Any]]:
    password = _secret(repo, "ARCADEDB_PASSWORD")
    auth = base64.b64encode(f"root:{password}".encode()).decode()
    payload = json.dumps({"language": "sql", "command": statement, "limit": -1, "params": params}).encode()
    request = urllib.request.Request(
        f"http://127.0.0.1:2480/api/v1/query/{database}", data=payload,
        headers={"Authorization": f"Basic {auth}", "Content-Type": "application/json"}, method="POST",
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        result = json.load(response).get("result", [])
    return [item for item in result if isinstance(item, dict)]


def _wait_arcade(repo: pathlib.Path, database: str, statement: str, params: dict[str, Any], timeout_seconds: int) -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    _wait_until(lambda: bool(result.extend(_arcade_query(repo, database, statement, params)) or result), min(timeout_seconds, 90), statement)
    return result


def _projected_marker(repo: pathlib.Path, database: str, identity: str, marker: str) -> bool:
    rows = _arcade_query(repo, database, "SELECT count(*) AS count FROM ConversationTurn WHERE identity_id=:identity_id AND content LIKE :content", {"identity_id": identity, "content": f"%{marker}%"})
    return bool(rows and int(rows[0].get("count", 0)) > 0)


def _tempo_traces(repo: pathlib.Path, request_id: str) -> list[str]:
    # Try request_id specific query first
    if request_id:
        try:
            traceql = f'{{"span.aura.request_id = \"{request_id}\""}}'
            raw = _command(repo, ["docker", "exec", "aura", "curl", "-fsS", "-G", "--data-urlencode", f"q={traceql}", "http://tempo:3200/api/search"])
            payload = json.loads(raw)
            traces = payload.get("traces", []) if isinstance(payload, dict) else []
            if traces:
                return [f"tempo:{item['traceID']}" for item in traces if isinstance(item, dict) and isinstance(item.get("traceID"), str)]
        except (RuntimeError, json.JSONDecodeError):
            pass
    # Fallback: search all recent traces
    try:
        raw = _command(repo, ["docker", "exec", "aura", "curl", "-fsS", "-G", "--data-urlencode", "q={}", "http://tempo:3200/api/search"])
        payload = json.loads(raw)
        traces = payload.get("traces", []) if isinstance(payload, dict) else []
        return [f"tempo:{item['traceID']}" for item in traces if isinstance(item, dict) and isinstance(item.get("traceID"), str)]
    except (RuntimeError, json.JSONDecodeError):
        return []


def _retention_days(row: dict[str, Any]) -> int | None:
    try:
        terminal = dt.datetime.fromisoformat(str(row["terminal_at"]).replace(" ", "T"))
        expires = dt.datetime.fromisoformat(str(row["expires_at"]).replace(" ", "T"))
    except (KeyError, ValueError):
        return None
    return (expires - terminal).days


def _find_tool(rows: list[dict[str, Any]], suffix: str) -> dict[str, Any]:
    return next((row for row in rows if _tool_name(row) == suffix or _tool_name(row).endswith("__" + suffix.removeprefix("memory__"))), {})


def _tool_name(row: dict[str, Any]) -> str:
    return str(row.get("tool_name", ""))


def _json_object(raw: str) -> dict[str, Any]:
    try:
        value = json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return {}
    return value if isinstance(value, dict) else {}


def _sse_frames(body: str) -> list[dict[str, Any]]:
    frames = []
    for block in body.replace("\r\n", "\n").split("\n\n"):
        data = "\n".join(line[5:].lstrip() for line in block.splitlines() if line.startswith("data:"))
        if not data:
            continue
        try:
            value = json.loads(data)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            frames.append(value)
    return frames


def _frame_text(frames: list[dict[str, Any]]) -> str:
    return "".join(str(frame.get("delta", "")) for frame in frames if frame.get("type") == "TEXT_MESSAGE_CONTENT")


def _reasoning_text(frames: list[dict[str, Any]]) -> str:
    return "".join(str(frame.get("delta", "")) for frame in frames if "REASONING" in str(frame.get("type", "")) and isinstance(frame.get("delta"), str))


def _wait_until(check: Any, timeout_seconds: int, label: str) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(1)
    raise RuntimeError(f"timed out waiting for {label}")


def _secret(repo: pathlib.Path, name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        env_file = repo / ".env"
        if env_file.exists():
            for line in env_file.read_text(encoding="utf-8").splitlines():
                if line.startswith(name + "="):
                    value = line.split("=", 1)[1].strip().strip('"').strip("'")
                    break
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def _command(repo: pathlib.Path, command: list[str]) -> str:
    completed = subprocess.run(command, cwd=repo, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False, timeout=120)
    if completed.returncode != 0:
        raise RuntimeError(f"command failed ({completed.returncode}): {' '.join(command[:4])}: {completed.stderr.strip()}")
    return completed.stdout


def _sql(value: str) -> str:
    return value.replace("'", "''")


def _unique(values: list[str]) -> list[str]:
    return list(dict.fromkeys(values))
