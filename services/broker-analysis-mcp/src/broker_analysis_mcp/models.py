"""Input models for the parametric tools; the MCP server validates requests against them."""

from typing import Annotated, Literal

from pydantic import BaseModel, Field

SeriesId = Annotated[str, Field(pattern=r"^[a-z][a-z0-9_]{0,31}$", description="lowercase id, e.g. ema_fast")]
Color = Annotated[str, Field(pattern=r"^#[0-9a-fA-F]{6}$")]


class IndicatorSpec(BaseModel):
    """One catalog indicator with its parameters, e.g. {"id": "rsi14", "fn": "rsi", "params": {"n": 14}}."""

    id: SeriesId
    fn: str = Field(description="catalog name: see list_indicator_catalog")
    params: dict[str, float | int | str] = Field(default_factory=dict)
    pane: Literal["auto", "price", "lower"] = "auto"
    color: Color | None = None


class SeriesSpec(BaseModel):
    """A series derived by expression, e.g. {"id": "gap", "expr": "close / sma200 - 1"}."""

    id: SeriesId
    expr: str
    pane: Literal["price", "lower"] = "lower"
    color: Color | None = None


class ConditionSpec(BaseModel):
    """A condition evaluated on every bar, e.g. {"name": "oversold in uptrend", "when": "rsi14 < 30 and close > sma200"}.

    mode "edge" fires on the bar the condition becomes true; "state" counts every true bar."""

    name: Annotated[str, Field(min_length=1, max_length=60)]
    when: str
    mode: Literal["edge", "state"] = "edge"
    marker: Literal["up", "down"] = "up"


class LevelSpec(BaseModel):
    """A horizontal level drawn on the price chart."""

    price: float
    label: Annotated[str, Field(max_length=40)] = ""


class HookAction(BaseModel):
    """Where a fired hook is delivered: an HTTP POST of JSON, or a line appended to a JSONL file."""

    type: Literal["webhook", "file"]
    target: Annotated[str, Field(max_length=500, description="URL for webhook, file name for file")]
