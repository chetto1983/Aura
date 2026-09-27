# Third-Party Notices

This repository adapts a small number of open-source implementation patterns. Keep this file in sync when adapted source code is added, removed, or materially changed.

## google/adk-go

- Source: `https://github.com/google/adk-go`
- License: Apache License 2.0
- Use in Aura: Phase 2 adapts workflow-agent control-flow patterns for `SequentialAgent`, `LoopAgent`, and `ParallelAgent` while not importing `google.golang.org/adk`.
- Planned adapted files:
  - `internal/agent/workflow/sequential.go`
  - `internal/agent/workflow/loop.go`
  - `internal/agent/workflow/parallel.go`
- Required hygiene:
  - Preserve an in-source attribution comment in adapted workflow files.
  - Do not copy upstream NOTICE text selectively; if a relevant upstream NOTICE entry appears, carry the applicable notice here.
  - Keep Aura-specific divergences documented in code comments where they affect behavior: budget exhaustion, shared parent budget, two-phase dedup, captured cancel for escalation, and clean sibling drain.

## smixs/visual-skills

- Source: `https://github.com/smixs/visual-skills`
- License: Creative Commons Attribution 4.0 International (CC BY 4.0)
- Author credit required by the upstream NOTICE: Serge Shima — https://sergeshima.com
- Use in Aura: prompt-writing rules for image and video generation, rewritten (not copied) into
  the builtin skill; the rest of the upstream skills (model selection, reference libraries) is not
  included.
- Adapted file:
  - `internal/skills/embed/media-generation-aura/SKILL.md`
- Required hygiene:
  - Keep the attribution footer at the end of that SKILL.md; it names the author, the source and
    the license, and says the text is adapted.

## assistant-ui/tool-ui

- Source: `https://github.com/assistant-ui/tool-ui`, at commit
  `49a870286facdbf28160cd647f0d337ebdc9b275`
- License: MIT (`LICENSE.md` at that commit, reproduced below)
- Use in Aura: the markup and classes of Question Flow
  (`apps/www/components/tool-ui/question-flow/question-flow.tsx`), ported onto Aura's tokens
  and translated. No Tool UI package or vendored file is installed.
- Adapted files:
  - `web/src/questions/QuestionCard.tsx`
  - `web/src/questions/QuestionOptions.tsx`
  - `web/src/questions/QuestionReceipt.tsx`
- Required hygiene:
  - Keep the attribution comment at the top of each adapted file; it names the commit, the
    copyright holder and the license.
  - A component later installed from the `@tool-ui` registry (`web/components.json`) is listed
    here when it lands.

```text
MIT License

Copyright (c) 2025 AgentbaseAI Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
