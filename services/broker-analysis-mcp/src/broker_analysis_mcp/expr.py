"""Safe expressions over price and indicator series.

Users and agents write conditions such as `rsi14 < 30 and close > sma200` or
`crosses_above(fast, slow)`. They are parsed with `ast` and evaluated by walking a small
whitelist of node types; nothing is passed to `eval`, there is no attribute access beyond
`indicator.output`, no subscripts, no comprehensions, no keyword arguments, and only the
functions below can be called. Size limits keep a hostile expression from exhausting the server.
"""

import ast
import operator
from collections.abc import Callable

import numpy as np
import pandas as pd

from .catalog import SpecError

MAX_LENGTH = 400
MAX_NODES = 120
MAX_WINDOW = 1000

Value = pd.Series | float | bool


def _window(n: Value) -> int:
    if not isinstance(n, (int, float)) or isinstance(n, bool) or n != int(n) or not 1 <= n <= MAX_WINDOW:
        raise SpecError(f"window must be an integer between 1 and {MAX_WINDOW}, got {n!r}")
    return int(n)


def _series(x: Value, like: pd.Series) -> pd.Series:
    return x if isinstance(x, pd.Series) else pd.Series(x, index=like.index)


def _crosses_above(a: Value, b: Value) -> pd.Series:
    ref = a if isinstance(a, pd.Series) else b
    a, b = _series(a, ref), _series(b, ref)
    return (a > b) & (a.shift(1) <= b.shift(1))


def _crosses_below(a: Value, b: Value) -> pd.Series:
    ref = a if isinstance(a, pd.Series) else b
    a, b = _series(a, ref), _series(b, ref)
    return (a < b) & (a.shift(1) >= b.shift(1))


def _elementwise(fn: Callable) -> Callable:
    def apply(a: Value, b: Value) -> Value:
        if isinstance(a, pd.Series) or isinstance(b, pd.Series):
            ref = a if isinstance(a, pd.Series) else b
            return pd.Series(fn(_series(a, ref), _series(b, ref)), index=ref.index)
        return fn(a, b)

    return apply


FUNCTIONS: dict[str, tuple[Callable, str]] = {
    "crosses_above": (_crosses_above, "crosses_above(a, b): a moves above b on this bar"),
    "crosses_below": (_crosses_below, "crosses_below(a, b): a moves below b on this bar"),
    "prev": (lambda x, n=1: x.shift(_window(n)), "prev(x, n=1): value n bars ago"),
    "change": (lambda x, n=1: (x / x.shift(_window(n)) - 1) * 100, "change(x, n=1): % change over n bars"),
    "highest": (lambda x, n: x.rolling(_window(n)).max(), "highest(x, n): rolling maximum"),
    "lowest": (lambda x, n: x.rolling(_window(n)).min(), "lowest(x, n): rolling minimum"),
    "mean": (lambda x, n: x.rolling(_window(n)).mean(), "mean(x, n): rolling mean"),
    "abs": (lambda x: x.abs() if isinstance(x, pd.Series) else abs(x), "abs(x)"),
    "max": (_elementwise(np.maximum), "max(a, b): elementwise maximum"),
    "min": (_elementwise(np.minimum), "min(a, b): elementwise minimum"),
    "between": (lambda x, lo, hi: (x >= lo) & (x <= hi), "between(x, low, high)"),
}

_BINARY = {ast.Add: operator.add, ast.Sub: operator.sub, ast.Mult: operator.mul, ast.Div: operator.truediv}
_COMPARE = {
    ast.Lt: operator.lt, ast.LtE: operator.le, ast.Gt: operator.gt,
    ast.GtE: operator.ge, ast.Eq: operator.eq, ast.NotEq: operator.ne,
}


def parse(text: str) -> ast.Expression:
    if len(text) > MAX_LENGTH:
        raise SpecError(f"expression longer than {MAX_LENGTH} characters")
    try:
        tree = ast.parse(text, mode="eval")
    except SyntaxError as err:
        raise SpecError(f"invalid expression {text!r}: {err.msg}") from err
    if sum(1 for _ in ast.walk(tree)) > MAX_NODES:
        raise SpecError(f"expression has more than {MAX_NODES} parts")
    return tree


def names(text: str) -> set[str]:
    """Series names an expression reads, e.g. {'rsi14', 'bb.lower'}."""
    found = set()
    for node in ast.walk(parse(text)):
        if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name):
            found.add(f"{node.value.id}.{node.attr}")
        elif isinstance(node, ast.Name) and node.id not in FUNCTIONS:
            found.add(node.id)
    return {n for n in found if not any(m.startswith(n + ".") for m in found if m != n)}


def evaluate(text: str, namespace: dict[str, pd.Series]) -> Value:
    try:
        return _Evaluator(namespace).visit(parse(text).body)
    except (ArithmeticError, AttributeError) as err:  # 1 / 0, or a series function given a number
        raise SpecError(f"cannot evaluate {text!r}: {err}") from err


class _Evaluator:
    def __init__(self, namespace: dict[str, pd.Series]):
        self.ns = namespace

    def visit(self, node: ast.AST) -> Value:
        method = getattr(self, f"_{type(node).__name__}", None)
        if method is None:
            raise SpecError(f"'{type(node).__name__}' is not allowed in an expression")
        return method(node)

    def _Constant(self, node: ast.Constant) -> Value:
        if isinstance(node.value, bool) or not isinstance(node.value, (int, float)):
            raise SpecError(f"only numbers are allowed as constants, got {node.value!r}")
        return float(node.value)

    def _Name(self, node: ast.Name) -> Value:
        if node.id in ("true", "True"):
            return True
        if node.id not in self.ns:
            raise SpecError(f"unknown series {node.id!r}; available: {sorted(self.ns)}")
        return self.ns[node.id]

    def _Attribute(self, node: ast.Attribute) -> Value:
        if not isinstance(node.value, ast.Name):
            raise SpecError("only indicator.output attribute access is allowed")
        key = f"{node.value.id}.{node.attr}"
        if key not in self.ns:
            raise SpecError(f"unknown series {key!r}; available: {sorted(self.ns)}")
        return self.ns[key]

    def _UnaryOp(self, node: ast.UnaryOp) -> Value:
        value = self.visit(node.operand)
        if isinstance(node.op, ast.USub):
            return -value
        if isinstance(node.op, ast.UAdd):
            return value
        if isinstance(node.op, ast.Not):
            return ~value.astype(bool) if isinstance(value, pd.Series) else not value
        raise SpecError("unsupported unary operator")

    def _BinOp(self, node: ast.BinOp) -> Value:
        op = _BINARY.get(type(node.op))
        if op is None:
            raise SpecError(f"operator {type(node.op).__name__} is not allowed (use + - * /)")
        return op(self.visit(node.left), self.visit(node.right))

    def _BoolOp(self, node: ast.BoolOp) -> Value:
        values = [self.visit(v) for v in node.values]
        combine = operator.and_ if isinstance(node.op, ast.And) else operator.or_
        result = values[0]
        for v in values[1:]:
            result = combine(_as_bool(result), _as_bool(v))
        return result

    def _Compare(self, node: ast.Compare) -> Value:
        left, result = self.visit(node.left), None
        for op_node, right_node in zip(node.ops, node.comparators, strict=True):
            op = _COMPARE.get(type(op_node))
            if op is None:
                raise SpecError(f"comparison {type(op_node).__name__} is not allowed")
            right = self.visit(right_node)
            step = op(left, right)
            result = step if result is None else result & step
            left = right
        return result

    def _Call(self, node: ast.Call) -> Value:
        if not isinstance(node.func, ast.Name) or node.func.id not in FUNCTIONS:
            raise SpecError(f"unknown function; allowed: {sorted(FUNCTIONS)}")
        if node.keywords:
            raise SpecError("keyword arguments are not allowed")
        fn = FUNCTIONS[node.func.id][0]
        try:
            return fn(*[self.visit(a) for a in node.args])
        except TypeError as err:
            raise SpecError(f"{node.func.id}: {FUNCTIONS[node.func.id][1]}") from err


def _as_bool(value: Value) -> Value:
    return value.fillna(False).astype(bool) if isinstance(value, pd.Series) else bool(value)
