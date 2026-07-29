"""Transaction boundary for public memory mutations."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from functools import wraps
from typing import Any, ParamSpec, TypeVar, cast

_P = ParamSpec("_P")
_R = TypeVar("_R")


class AtomicMutationError(RuntimeError):
    """A domain failure that must abort the active graph transaction."""

    def __init__(self, result: dict[str, Any]):
        super().__init__(str(result["error"]))
        self.result = result


def atomic_memory_mutation(
    method: Callable[_P, Awaitable[_R]],
) -> Callable[_P, Awaitable[_R]]:
    """Run one integration mutation and its nested writes atomically."""

    @wraps(method)
    async def wrapped(*args: _P.args, **kwargs: _P.kwargs) -> _R:
        owner = args[0]

        async def work() -> _R:
            result = await method(*args, **kwargs)
            if isinstance(result, dict) and result.get("error"):
                raise AtomicMutationError(result)
            return result

        try:
            return await owner.client.graph.execute_write_unit(work)
        except AtomicMutationError as exc:
            return cast(_R, exc.result)

    return wrapped
