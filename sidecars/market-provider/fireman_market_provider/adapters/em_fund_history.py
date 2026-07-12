"""Eastmoney open-fund NAV history without evaluating upstream JavaScript.

Eastmoney publishes NAV arrays in ``pingzhongdata/{code}.js``.  AKShare
evaluates that whole file with a JavaScript engine, which fails for legacy
back-end subscription codes whose data URL redirects to a not-found page.
This adapter follows the explicit canonical-code redirect exposed by the
fund's own page and decodes only the two JSON-compatible NAV assignments.
"""

from __future__ import annotations

import json
import re
from typing import Any

import pandas as pd
import requests

_FUND_HOST = "https://fund.eastmoney.com"
_REQUEST_TIMEOUT_SECONDS = 20
_HEADERS = {
    "Referer": f"{_FUND_HOST}/",
    "User-Agent": "Mozilla/5.0",
}
_CODE_RE = re.compile(r"\d{6}")
_CANONICAL_REDIRECT_RE = re.compile(
    r"location\.href\s*=\s*['\"]https?://fund\.eastmoney\.com/(\d{6})\.html['\"]",
    re.IGNORECASE,
)


def _request_text(url: str) -> tuple[int, str]:
    response = requests.get(
        url,
        headers=_HEADERS,
        timeout=_REQUEST_TIMEOUT_SECONDS,
        allow_redirects=False,
    )
    response.raise_for_status()
    return response.status_code, response.content.decode("utf-8-sig", errors="replace")


def _script_for_code(symbol: str, expected_canonical_symbol: str | None = None) -> str:
    if _CODE_RE.fullmatch(symbol) is None:
        raise ValueError(f"invalid mutual fund code {symbol!r}")

    status, script = _request_text(f"{_FUND_HOST}/pingzhongdata/{symbol}.js")
    if status == 200 and "Data_netWorthTrend" in script:
        if expected_canonical_symbol and expected_canonical_symbol != symbol:
            raise ValueError(
                f"fund {symbol} serves its own history but directory expects canonical "
                f"code {expected_canonical_symbol}"
            )
        return script

    _, fund_page = _request_text(f"{_FUND_HOST}/{symbol}.html")
    match = _CANONICAL_REDIRECT_RE.search(fund_page)
    if match is None or match.group(1) == symbol:
        return ""

    canonical_code = match.group(1)
    if expected_canonical_symbol and canonical_code != expected_canonical_symbol:
        raise ValueError(
            f"fund {symbol} redirects to {canonical_code}, not directory canonical "
            f"code {expected_canonical_symbol}"
        )
    canonical_status, canonical_script = _request_text(
        f"{_FUND_HOST}/pingzhongdata/{canonical_code}.js"
    )
    if canonical_status != 200:
        return ""
    return canonical_script


def _json_assignment(script: str, variable: str) -> Any:
    match = re.search(rf"(?:var\s+)?{re.escape(variable)}\s*=\s*", script)
    if match is None:
        return []
    try:
        value, _ = json.JSONDecoder().raw_decode(script[match.end() :])
    except (json.JSONDecodeError, TypeError):
        return []
    return value


def _dates_from_milliseconds(values: pd.Series) -> pd.Series:
    return (
        pd.to_datetime(values, unit="ms", errors="coerce", utc=True)
        .dt.tz_convert("Asia/Shanghai")
        .dt.date
    )


def _cumulative_nav_frame(script: str) -> pd.DataFrame:
    rows = _json_assignment(script, "Data_ACWorthTrend")
    if not isinstance(rows, list) or not rows:
        return pd.DataFrame()
    frame = pd.DataFrame(rows)
    if frame.shape[1] < 2:
        return pd.DataFrame()
    frame = frame.iloc[:, :2].copy()
    frame.columns = ["净值日期", "累计净值"]
    frame["净值日期"] = _dates_from_milliseconds(frame["净值日期"])
    frame["累计净值"] = pd.to_numeric(frame["累计净值"], errors="coerce")
    return frame.dropna(subset=["净值日期", "累计净值"])


def _unit_nav_frame(script: str) -> pd.DataFrame:
    rows = _json_assignment(script, "Data_netWorthTrend")
    if not isinstance(rows, list) or not rows:
        return pd.DataFrame()
    frame = pd.DataFrame(rows)
    if "x" not in frame.columns or "y" not in frame.columns:
        return pd.DataFrame()
    frame = frame[["x", "y"]].copy()
    frame.columns = ["净值日期", "单位净值"]
    frame["净值日期"] = _dates_from_milliseconds(frame["净值日期"])
    frame["单位净值"] = pd.to_numeric(frame["单位净值"], errors="coerce")
    return frame.dropna(subset=["净值日期", "单位净值"])


def em_fund_open_history(
    symbol: str,
    indicator: str,
    expected_canonical_symbol: str | None = None,
) -> pd.DataFrame:
    """Return cumulative or unit NAV history for an open mutual fund."""
    script = _script_for_code(
        symbol.strip(), (expected_canonical_symbol or "").strip() or None
    )
    if not script:
        return pd.DataFrame()
    if indicator == "累计净值走势":
        return _cumulative_nav_frame(script)
    if indicator == "单位净值走势":
        return _unit_nav_frame(script)
    raise ValueError(f"unsupported open fund indicator {indicator!r}")
