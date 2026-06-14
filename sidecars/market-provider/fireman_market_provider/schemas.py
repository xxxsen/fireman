"""Request and response schemas for the AKShare market provider HTTP API."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

Market = Literal["CN", "HK", "US"]
InstrumentType = Literal[
    "cn_exchange_stock",
    "cn_exchange_fund",
    "cn_mutual_fund",
    "hk_stock",
    "hk_etf",
    "us_stock",
    "us_etf",
    "fx_rate",
]
AssetClass = Literal["equity", "bond", "cash", "fx"]
PointType = Literal["adjusted_close", "nav", "total_return_index", "fx_rate"]
ExpenseRatioStatus = Literal["provider_verified", "unavailable", "not_applicable"]
SourceQuality = Literal["full", "partial", "empty"]


class FetchRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    market: Market
    instrument_type: InstrumentType
    source_code: str = Field(min_length=1, max_length=64)
    start_date: str | None = None
    end_date: str = Field(min_length=10, max_length=10)
    adjust_policy: Literal["none", "qfq", "hfq"] = "none"
    resolved_name: str | None = None


class HistoricalPoint(BaseModel):
    model_config = ConfigDict(extra="forbid")

    date: str = Field(min_length=10, max_length=10)
    value: float


class FetchData(BaseModel):
    model_config = ConfigDict(extra="forbid")

    provider: str
    provider_symbol: str
    name: str
    asset_class: AssetClass
    currency: str
    point_type: PointType
    expense_ratio_status: ExpenseRatioStatus
    expense_ratio_components: dict[str, Any] = Field(default_factory=dict)
    points: list[HistoricalPoint]
    source_name: str
    source_quality: SourceQuality
    source_kind: str | None = None


class FetchResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: int
    message: str
    data: FetchData


class HealthResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    status: Literal["ok"]


class ResolveRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    market: Market
    instrument_type: InstrumentType
    code: str = Field(min_length=1, max_length=64)


class ResolveCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: str
    provider_symbol: str
    name: str
    exchange: str
    instrument_kind: str
    candidate_id: str


class ResolveData(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ambiguous: bool
    resolved: ResolveCandidate | None = None
    candidates: list[ResolveCandidate] = Field(default_factory=list)


class ResolveResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: int
    message: str
    data: ResolveData


MetadataRefreshTarget = Literal["cn_mutual_fund_names"]


class MetadataRefreshRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: MetadataRefreshTarget


class MetadataRefreshData(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: MetadataRefreshTarget
    entry_count: int
    refreshed_at: str
    cache_path: str


class MetadataRefreshResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: int
    message: str
    data: MetadataRefreshData
