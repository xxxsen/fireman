"""Asset classification helpers for AKShare metadata."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Literal

import pandas as pd

from ..schemas import AssetClass

CnMutualFundSourceKind = Literal["open_fund", "money_fund", "financial_fund", "lof"]


@dataclass
class FundMeta:
    name: str
    asset_class: AssetClass | None
    region: str
    expense_ratio: float | None = None
    expense_ratio_status: str = "unavailable"
    components: dict[str, Any] | None = None


UNSUPPORTED_KEYWORDS = (
    "FOF",
    "REIT",
    "商品",
    "黄金",
    "期货",
    "另类",
)


def default_region(market: str, instrument_type: str) -> str:
    if market in ("US", "HK"):
        return "foreign"
    if instrument_type == "fx_rate":
        return "domestic"
    return "domestic"


def classify_cn_mutual_fund(df: pd.DataFrame, symbol: str) -> FundMeta:
    name = symbol
    fund_type = ""
    if "基金简称" in df.columns and not df["基金简称"].empty:
        name = str(df["基金简称"].iloc[0])
    if "基金类型" in df.columns and not df["基金类型"].empty:
        fund_type = str(df["基金类型"].iloc[0])

    if "每万份收益" in df.columns and "累计净值" not in df.columns:
        return FundMeta(
            name=name,
            asset_class="cash",
            region="domestic",
            components={"fund_type": fund_type or "货币基金", "region": "domestic"},
        )

    text = f"{name} {fund_type}"
    for keyword in UNSUPPORTED_KEYWORDS:
        if keyword in text:
            return FundMeta(name=name, asset_class=None, region="domestic")

    asset_class: AssetClass | None
    if any(k in text for k in ("货币", "货币基金")):
        asset_class = "cash"
    elif any(k in text for k in ("债券", "纯债", "利率")):
        asset_class = "bond"
    elif any(
        k in text
        for k in ("混合", "混合基金", "股票", "指数", "ETF联接", "QDII", "权益")
    ):
        asset_class = "equity"
    else:
        asset_class = None

    region = "foreign" if "QDII" in text else "domestic"
    return FundMeta(
        name=name,
        asset_class=asset_class,
        region=region,
        components={"fund_type": fund_type, "region": region},
    )


def detect_cn_mutual_fund_source_kind(symbol: str) -> CnMutualFundSourceKind:
    """Pick the allowed fetch source family before trying AKShare providers."""
    from .names import get_cn_mutual_fund_name, lookup_cn_lof_name

    bare = symbol.strip()
    if lookup_cn_lof_name(bare):
        return "lof"

    name = get_cn_mutual_fund_name(bare) or bare
    text = f"{name}"
    upper = text.upper()
    if "LOF" in upper:
        return "lof"
    if any(k in text for k in ("货币", "货币基金")):
        return "money_fund"
    if "理财" in text:
        return "financial_fund"
    return "open_fund"


def classify_us_symbol(symbol: str, default_type: AssetClass) -> FundMeta:
    return FundMeta(name=symbol, asset_class=default_type, region="foreign")
