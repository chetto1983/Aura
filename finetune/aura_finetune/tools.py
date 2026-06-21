"""Load Aura's exported tool surface and project it to FunctionGemma's view.

The exporter (`go run ./finetune/exporter`) writes the canonical spec. Here we
load it and expose two views the rest of the pipeline needs:

  * `default_tooldefs()` — exactly what the agent shows on a fresh turn
    (deferred tools contribute only name + summary). This is the manifest a
    trained model must learn to drive tool_search from.
  * `full_tooldefs(names)` — the schemas the model sees AFTER a successful
    `tool_search(select:...)`, used to build the post-load turns in a trace.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class ToolSpec:
    name: str
    summary: str
    description: str
    parameters: dict
    deferred: bool
    mutating: bool

    def tooldef(self, *, full: bool) -> dict:
        """OpenAI/HF function-tool dict.

        A deferred tool rendered in the default view carries only its summary
        and an empty schema (matching Registry.RenderToolDefs); `full=True`
        promotes it to the complete description + parameters.
        """
        if full or not self.deferred:
            return {
                "type": "function",
                "function": {
                    "name": self.name,
                    "description": self.description or self.summary,
                    "parameters": self.parameters,
                },
            }
        return {
            "type": "function",
            "function": {"name": self.name, "description": self.summary},
        }


class ToolCatalog:
    def __init__(self, specs: list[ToolSpec], default_defs: list[dict]):
        self._by_name = {s.name: s for s in specs}
        self._specs = specs
        self._default_defs = default_defs

    @classmethod
    def load(cls, path: str | Path) -> "ToolCatalog":
        blob = json.loads(Path(path).read_text())
        specs = [
            ToolSpec(
                name=t["name"],
                summary=t.get("summary", ""),
                description=t.get("description", ""),
                parameters=t.get("parameters") or {"type": "object", "properties": {}},
                deferred=bool(t.get("deferred")),
                mutating=bool(t.get("mutating")),
            )
            for t in blob["tools"]
        ]
        default_defs = json.loads(blob["default_tooldefs"]) if isinstance(
            blob.get("default_tooldefs"), str
        ) else blob.get("default_tooldefs", [])
        return cls(specs, default_defs or [])

    @property
    def names(self) -> list[str]:
        return [s.name for s in self._specs]

    def get(self, name: str) -> ToolSpec | None:
        return self._by_name.get(name)

    def default_tooldefs(self) -> list[dict]:
        """The fresh-turn manifest (deferred = stub). Falls back to deriving
        from specs if the exporter predates the default_tooldefs field."""
        if self._default_defs:
            return self._default_defs
        return [s.tooldef(full=False) for s in self._specs]

    def full_tooldefs(self, names: list[str]) -> list[dict]:
        out = []
        for n in names:
            spec = self._by_name.get(n)
            if spec is not None:
                out.append(spec.tooldef(full=True))
        return out

    def deferred_names(self) -> list[str]:
        return [s.name for s in self._specs if s.deferred]

    def active_names(self) -> list[str]:
        return [s.name for s in self._specs if not s.deferred]
