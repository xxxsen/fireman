"""Market-data metadata extraction for AKShare frames.

The provider never decides FIRE asset classes (equity/bond/cash): that
classification comes exclusively from the user's plan holdings. This module
only extracts descriptive market metadata (display name, fund type text,
expense info) from upstream frames.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Literal

import pandas as pd

CnMutualFundSourceKind = Literal["open_fund", "money_fund", "financial_fund"]


@dataclass
class FundMeta:
    """Descriptive metadata for a fetched fund history frame."""

    name: str
    region: str
    expense_ratio: float | None = None
    expense_ratio_status: str = "unavailable"
    components: dict[str, Any] | None = None


def default_region(market: str, instrument_type: str) -> str:
    if market in ("US", "HK"):
        return "foreign"
    if instrument_type == "fx_rate":
        return "domestic"
    return "domestic"


def describe_cn_mutual_fund(
    df: pd.DataFrame, symbol: str, resolved_name: str | None = None
) -> FundMeta:
    """Extract display metadata from a CN mutual fund history frame.

    Never classifies the fund into FIRE asset classes and never rejects a
    frame because its name/type text is unrecognized: whether the data is
    usable is decided solely by point parsing downstream.
    """
    from .names import lookup_cn_mutual_fund_name_readonly, name_from_dataframe

    name = resolved_name or name_from_dataframe(df, symbol) or symbol
    fund_type = ""
    if "基金类型" in df.columns and not df["基金类型"].empty:
        fund_type = str(df["基金类型"].iloc[0])

    # akshare NAV history frames omit name/type metadata; use resolve name or read-only cache.
    if name == symbol or not name.strip():
        cached = lookup_cn_mutual_fund_name_readonly(symbol)
        if cached:
            name = cached

    return FundMeta(
        name=name,
        region="domestic",
        components={"fund_type": fund_type, "region": "domestic"},
    )
