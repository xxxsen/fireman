"""Normalize AKShare DataFrames into standardized historical points."""

from __future__ import annotations

from datetime import date
from typing import Any

import pandas as pd

from .schemas import HistoricalPoint


DATE_CANDIDATES = ("日期", "date", "trade_date", "净值日期", "时间")
VALUE_CANDIDATES = (
    "收盘",
    "收盘价",
    "close",
    "adj_close",
    "单位净值",
    "累计净值",
    "净值",
    "value",
)


def _pick_column(df: pd.DataFrame, candidates: tuple[str, ...]) -> str | None:
    lower_map = {str(c).lower(): c for c in df.columns}
    for cand in candidates:
        if cand in df.columns:
            return cand
        if cand.lower() in lower_map:
            return lower_map[cand.lower()]
    return None


def _parse_date(value: Any) -> str | None:
    if value is None or (isinstance(value, float) and pd.isna(value)):
        return None
    if isinstance(value, date):
        return value.isoformat()
    text = str(value).strip()
    if not text:
        return None
    if " " in text:
        text = text.split(" ", 1)[0]
    parsed = pd.to_datetime(text, errors="coerce")
    if pd.isna(parsed):
        return None
    return parsed.date().isoformat()


def normalize_dataframe(df: pd.DataFrame) -> list[HistoricalPoint]:
    """Convert a provider DataFrame into ascending unique daily points."""
    if df is None or df.empty:
        return []

    date_col = _pick_column(df, DATE_CANDIDATES)
    value_col = _pick_column(df, VALUE_CANDIDATES)
    if date_col is None or value_col is None:
        return []

    rows: list[tuple[str, float]] = []
    for _, row in df.iterrows():
        trade_date = _parse_date(row[date_col])
        if trade_date is None:
            continue
        raw_value = row[value_col]
        if raw_value is None or (isinstance(raw_value, float) and pd.isna(raw_value)):
            continue
        try:
            value = float(raw_value)
        except (TypeError, ValueError):
            continue
        if value <= 0:
            continue
        rows.append((trade_date, value))

    if not rows:
        return []

    # Sort ascending; keep last value per date.
    rows.sort(key=lambda item: item[0])
    dedup: dict[str, float] = {}
    for trade_date, value in rows:
        dedup[trade_date] = value

    return [HistoricalPoint(date=d, value=v) for d, v in sorted(dedup.items())]
