"""fx_rate_sync executor: full history for the system FX pairs."""

from __future__ import annotations

import time
from typing import Any

import pandas as pd

from ...logutil import get_logger
from ...normalize import _parse_date
from ...timeout_util import UpstreamCall, call_with_timeout, fetch_timeout_seconds
from ..errors import TaskFailure

logger = get_logger(__name__)

# Bank-of-China (via sina) daily rate series per pair. Values are quoted per
# 100 units of foreign currency.
_PAIR_LABELS = {
    "USDCNY": "美元",
    "HKDCNY": "港币",
}
_FX_SOURCE = "ak.currency_boc_sina"

# Preferred value columns, most authoritative first.
_VALUE_COLUMNS = ("央行中间价", "中行汇买价", "中行钞买价", "中间价")


def _today_compact() -> str:
    return time.strftime("%Y%m%d")


def _extract_rates(df: pd.DataFrame, pair: str) -> list[dict[str, Any]]:
    if df is None or df.empty:
        return []
    date_col = None
    for cand in ("日期", "date"):
        if cand in df.columns:
            date_col = cand
            break
    if date_col is None:
        return []
    value_col = None
    for cand in _VALUE_COLUMNS:
        if cand in df.columns:
            value_col = cand
            break
    if value_col is None:
        return []

    dedup: dict[str, float] = {}
    for _, row in df.iterrows():
        date = _parse_date(row[date_col])
        if date is None:
            continue
        try:
            value = float(row[value_col])
        except (TypeError, ValueError):
            continue
        if value <= 0 or pd.isna(value):
            continue
        dedup[date] = value / 100.0
    return [{"date": d, "pair": pair, "value": v} for d, v in sorted(dedup.items())]


def _fetch_pair(pair: str) -> list[dict[str, Any]]:
    label = _PAIR_LABELS.get(pair)
    if label is None:
        raise TaskFailure("invalid_task_payload", f"unsupported fx pair {pair}")
    try:
        df = call_with_timeout(
            UpstreamCall(
                "currency_boc_sina",
                kwargs=(("symbol", label), ("start_date", "19900101"), ("end_date", _today_compact())),
            ),
            fetch_timeout_seconds(),
        )
    except TimeoutError as exc:
        raise TaskFailure("market_provider_timeout", f"fx fetch for {pair} timed out") from exc
    except Exception as exc:  # noqa: BLE001
        raise TaskFailure("market_provider_unavailable", f"fx fetch for {pair} failed: {exc}") from exc

    rates = _extract_rates(df, pair)
    if not rates:
        raise TaskFailure("provider_data_incomplete", f"fx fetch for {pair} returned no usable rates")
    return rates


def execute_fx_sync(payload: dict[str, Any]) -> dict[str, Any]:
    pairs = [str(p).upper() for p in (payload.get("pairs") or [])]
    if not pairs:
        raise TaskFailure("invalid_task_payload", "fx payload needs pairs")

    rates: list[dict[str, Any]] = []
    for pair in pairs:
        pair_rates = _fetch_pair(pair)
        logger.info("fx sync %s: %d rates", pair, len(pair_rates))
        rates.extend(pair_rates)

    return {
        "type": "fx_rate_sync",
        "pairs": pairs,
        "source_name": _FX_SOURCE,
        "rates": rates,
    }
