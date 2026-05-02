# Aura Vision

Aura is your personal, always-on Telegram assistant that thinks in long-term timelines, not single messages.[page:1] The goal is to give power users and developers a reliable companion that can remember context, orchestrate tools, and help manage both information and actions across weeks and months.

## Why Aura exists

Most chatbots are locked inside a single chat window, with shallow memory and limited access to your real workflows. Aura aims to be different:

- Run locally or on your own server so you stay in control of your data.
- Integrate directly into Telegram, where real conversations already happen.
- Combine LLMs, a local wiki, search, and other tools into one cohesive brain.[page:1]

Aura should feel less like “a bot that replies” and more like “an operator that runs your second brain”.

## Core principles

- **Ownership**: You own the deployment, configuration, and data. Aura should be easy to self-host, inspect, and extend.
- **Composability**: Each capability (LLM, wiki, search, budget tracking, skills, tasks) is modular, so you can enable only what you need.[page:1]
- **Transparency**: Behavior should be debuggable and observable through logs, health endpoints, and simple configuration.[page:1]
- **Pragmatism**: Prefer simple, reliable solutions over overly complex architectures. The project should be approachable for contributors.
- **No hand-edit installs**: A non-developer should be able to install Aura, point it at an LLM, and start chatting in under five minutes — without ever opening `.env` or restarting the bot to change a setting.
- **Bounded growth**: Anything Aura writes to disk (conversation archive, wiki sources, embed cache, scheduled tasks) must be inspectable and pruneable from the dashboard. Nothing grows unbounded without a visible control.

## What Aura should feel like

Aura should feel:

- Fast: low-latency responses and lightweight Go services, even on modest hardware.[page:1]
- Consistent: the same behavior across restarts, environments, and upgrades.
- Trustworthy: clear error modes, no silent failures, and predictable behavior when external APIs misbehave.
- Extensible: easy to add new “skills” and tools without rewriting the core.[page:1]

A typical user should be able to:

- Ask questions and get answers grounded in their own wiki and notes.
- Search across conversations and local knowledge.
- Track simple budgets, tasks, and recurring routines over time.
- Connect to different LLM providers, from cloud APIs to local models.[page:1]

## Long-term direction

Over time, Aura should evolve into a robust personal automation platform built on top of Telegram:

- Deeper memory and knowledge: richer wiki and graph views in the web dashboard, smarter retrieval, and better summarization of long-term history.[page:1]
- More skills: reusable, well-documented skills for common workflows (planning, tracking, integrations with external systems).
- Stronger observability: richer health endpoints, metrics, and traces that make it easy to understand what Aura is doing and why.[page:1]
- Safer defaults: guard rails, access controls, and clear separation between read-only and write-capable actions.

## How the community fits in

Aura is intentionally built as an open, contributor-friendly project:

- Developers can add skills, commands, and integrations as first-class modules.
- Power users can propose real-world workflows that shape the roadmap.
- Maintainers focus on stability, DX, and documentation so that contributions stay easy to reason about.[page:1]

If you are interested in this vision and want to help, check out `CONTRIBUTING.md` and open an issue or pull request with your ideas and experiments.
