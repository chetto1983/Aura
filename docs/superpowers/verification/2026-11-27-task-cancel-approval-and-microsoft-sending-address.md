# Live check: foreign task cancel approval (7a) and Microsoft sending address (7c)

Date: 2026-11-27. Lab VM, after the operator applied the update:
- aura `03e78459c`, which contains 78c2e77c8..231143339;
- aura-pim-mcp `68f8935`.

The updater replaced the PIM after aura started and restarted aura once to remount it, as designed (third live run
of that path).

## 7c: a Microsoft account reports its sending address

- **Before.** On 2026-11-25, `list_accounts` logged `Sending address of account …__microsoft not found: ODataError`
  on every call, and the Microsoft account had no address. Graph `GET /me` needs `User.Read`, which no account was
  granted.
- **Measured.**
  - `list_accounts` was called through the mounted PIM MCP. The operator re-authenticated it, and the call is
    read-only.
  - All three accounts came back with an `email`. The Microsoft 365 one now carries its sign-in address.
  - `docker logs aura-pim-mcp` since the restart has **0** "Sending address" warnings.
- **Verdict: PASS.**
- **Not shown.**
  - That the sign-in name equals the From of a mail actually sent from that account. It does for a personal account
    signed in with its own mailbox address. No mail was sent for this check.
  - The Gmail cache surviving a Google re-link as another user under the same id until restart. This was accepted
    in review.

## 7a: cancelling a task the conversation did not schedule

- **Seed.** A reminder `[aura-misura] promemoria creato dal CLI (controllo 7a)`, scheduled with
  `aura task schedule --kind reminder --at <tomorrow 06:00 UTC>` from the CLI, so it has no origin conversation.
- **Run.** A new cockpit chat: "Cancella il promemoria … previsto per domani alle 6 UTC."
- **Measured.**
  - **The card.** `task cancel` returned the challenge, and the cockpit rendered the Aura card **in Italian**:
    "Approva l'annullamento di un'attività non pianificata in questa chat?", followed by the task, type, schedule,
    next run, the payload verbatim, and sha256.
  - **The cancel.** After the accept, the task was cancelled, and the model's last call returned
    `cancelled task <id>`.
  - **Defect 1: two approvals.**
    1. Before calling `cancel`, the model asked with its own `ask_user`. The description's "first asks the operator"
       was read as an instruction.
    2. After the first "sì" it called `cancel`, relayed the challenge, and needed a second "sì".
  - **Defect 2: slowness.**
    - The run took 7 LLM calls and about 80 s of model time, against milliseconds for tool time. The phases:
      - message to the first question: 28 s;
      - first answer to the Aura card: 37 s, of which the `ask_user` that only re-copies the challenge took 23.8 s;
      - second answer to the end: 14 s.
    - The card also appeared in the page some seconds after its `ask_user` row.
- **Verdict: PARTIAL.** The gate holds and the card is right. The operator rejected the flow: "lentezza e doppia
  approvazione".
- **Follow-ups.**
  1. The description no longer invites a pre-question. This is the interim fix.
  2. A common approval mechanism on the industrial pattern: the host parks the call and shows the card itself, then
     runs the original call on accept. It removes the model-copied challenge and the repeated call for every
     approval: gateway, shell_exec, plugin_pack and task cancel. Spec in progress.
- **Not shown.**
  - The own-chat cancel with no card. It was not run this time, and the unit tests cover it.
  - Telegram rendering.
  - The db_integration adapter path, which CI's db tier runs.
