"""HKEX official List of Securities fetcher for HK directory sync.

The Eastmoney HK fund board (fs ``m:116 t:1``) mixes ETF, leveraged/inverse
products, REITs and currency counters without a structured sub-type or
currency field, which previously forced name/code heuristics. The HKEX
"List of Securities" workbook is the authoritative upstream source: it
carries a structured ``Category`` / ``Sub-Category`` (Equity, Exchange
Traded Products, Real Estate Investment Trusts, Leveraged and Inverse) and
the per-counter ``Trading Currency`` (HKD/RMB/USD), so the directory never
has to guess identity from names or code ranges.

Dispatched as the custom operation ``hkex_list_of_securities`` through
``timeout_util.dispatch_upstream_call`` so the hard-timeout child-process
wrapper and test dispatch overrides keep working.
"""

from __future__ import annotations

import io

import pandas as pd
import requests

from ..logutil import get_logger

logger = get_logger(__name__)

_LIST_OF_SECURITIES_URL = (
    "https://www.hkex.com.hk/eng/services/trading/securities/securitieslists/ListOfSecurities.xlsx"
)
_REQUEST_TIMEOUT_SECONDS = 60
# The workbook has two banner rows before the real header row.
_HEADER_ROW = 2

_REQUIRED_COLUMNS = ("Stock Code", "Name of Securities", "Category", "Sub-Category", "Trading Currency")


def hkex_list_of_securities() -> pd.DataFrame:
    """Download and normalize the HKEX List of Securities.

    Returns a DataFrame with columns ``symbol`` (zero-padded 5-digit code),
    ``name_en``, ``category``, ``sub_category`` and ``currency`` (ISO-style:
    HKD/CNY/USD). Raises when the workbook shape changes so directory sync
    fails loudly instead of writing guessed identities.
    """
    resp = requests.get(
        _LIST_OF_SECURITIES_URL,
        headers={"User-Agent": "Mozilla/5.0"},
        timeout=_REQUEST_TIMEOUT_SECONDS,
    )
    resp.raise_for_status()
    df = pd.read_excel(io.BytesIO(resp.content), header=_HEADER_ROW)
    missing = [col for col in _REQUIRED_COLUMNS if col not in df.columns]
    if missing:
        raise RuntimeError(f"hkex list of securities is missing columns: {missing}")
    return _normalize_frame(df)


_CURRENCY_MAP = {"HKD": "HKD", "RMB": "CNY", "USD": "USD"}


def _normalize_frame(df: pd.DataFrame) -> pd.DataFrame:
    symbols: list[str] = []
    names: list[str] = []
    categories: list[str] = []
    sub_categories: list[str] = []
    currencies: list[str] = []
    for _, row in df.iterrows():
        code_raw = row.get("Stock Code")
        if code_raw is None or (isinstance(code_raw, float) and pd.isna(code_raw)):
            continue
        digits = "".join(ch for ch in str(code_raw).split(".")[0] if ch.isdigit())
        if not digits:
            continue
        currency_raw = str(row.get("Trading Currency") or "").strip().upper()
        symbols.append(digits.zfill(5))
        names.append(str(row.get("Name of Securities") or "").strip())
        categories.append(str(row.get("Category") or "").strip())
        sub_categories.append(str(row.get("Sub-Category") or "").strip())
        currencies.append(_CURRENCY_MAP.get(currency_raw, ""))
    return pd.DataFrame(
        {
            "symbol": symbols,
            "name_en": names,
            "category": categories,
            "sub_category": sub_categories,
            "currency": currencies,
        }
    )
