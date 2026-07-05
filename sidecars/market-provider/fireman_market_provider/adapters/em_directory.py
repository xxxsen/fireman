"""Eastmoney quote-center listing fetchers for HK/US directory sync.

AKShare has no dedicated HK-ETF / US-ETF list endpoints, and its full-board
spot functions (``stock_hk_spot_em`` / ``stock_us_spot_em``) mix security
categories that the local directory taxonomy must keep apart (equities vs ETF
vs bonds/warrants). These fetchers hit the same Eastmoney ``qt/clist`` API
akshare wraps, but pinned to explicit category filters so every directory
instrument_type maps to exactly one upstream board:

- HK equities:   fs ``m:116 t:3`` (main board) + ``m:116 t:4`` (GEM)
- HK funds:      fs ``m:116 t:1`` (ETF / L&I / REIT / currency counters)
- US equities:   fs ``t:1`` common, ``t:2`` preferred, ``t:3`` ADR,
                 ``t:4`` closed-end funds, ``t:10`` other (e.g. BRK_A)
                 across ``m:105/106/107``
- US ETF:        fs ``t:5`` across ``m:105/106/107``

Bonds/notes (HK ``t:2``, US ``t:6``), warrants (``t:8``) and SPAC
units/rights (``t:9``) are intentionally not listed anywhere.

The functions return DataFrames shaped like the akshare spot outputs
(``代码`` / ``名称``) so the directory executor's row handling stays uniform.
They are dispatched as custom operations through
``timeout_util.dispatch_upstream_call`` (operation names ``em_*_list``), which
keeps the hard-timeout child-process wrapper and the test dispatch overrides
working unchanged.
"""

from __future__ import annotations

import time
from typing import Any

import pandas as pd
import requests

from ..logutil import get_logger

logger = get_logger(__name__)

# Delayed quotes are fine for directory listings (only codes/names are used);
# push2delay is the public endpoint, 72.push2 is the host akshare targets.
_CLIST_HOSTS = (
    "https://push2delay.eastmoney.com/api/qt/clist/get",
    "https://72.push2.eastmoney.com/api/qt/clist/get",
)
_PAGE_SIZE = 100
_MAX_PAGES = 500
_PAGE_SLEEP_SECONDS = 0.2
_REQUEST_TIMEOUT_SECONDS = 15
_UT_TOKEN = "bd1d9ddb04089700cf9c27f6f7426281"

_HK_EQUITY_FS = "m:116 t:3,m:116 t:4"
_HK_FUND_FS = "m:116 t:1"
_US_EQUITY_FS = ",".join(f"m:{m} t:{t}" for m in (105, 106, 107) for t in (1, 2, 3, 4, 10))
_US_ETF_FS = ",".join(f"m:{m} t:5" for m in (105, 106, 107))
# CN fund boards (the filters akshare's fund_etf_spot_em / fund_lof_spot_em
# use). Each row's f13 is Eastmoney's authoritative market id (1=SH, 0=SZ),
# which replaces any code-prefix exchange inference in directory sync.
_CN_ETF_FS = "b:MK0021,b:MK0022,b:MK0023,b:MK0024,b:MK0827"
_CN_LOF_FS = "b:MK0404,b:MK0405,b:MK0406,b:MK0407"
# CN A-share equity boards (the exact filters akshare's per-exchange spot
# functions use). akshare targets the realtime push2 hosts, which drop
# connections from some networks at the TLS layer regardless of headers;
# these listings go through the delayed-quote host chain above instead.
# The exchange is a property of the queried board, never of per-row fields:
# BJ board rows carry f13=0 (same as SZ), so f13 must not be used for stock
# exchange identity.
_CN_SH_A_FS = "m:1 t:2,m:1 t:23"
_CN_SZ_A_FS = "m:0 t:6,m:0 t:80"
_CN_BJ_A_FS = "m:0 t:81 s:2048"


def _fetch_page(url: str, fs: str, page: int) -> dict[str, Any]:
    resp = requests.get(
        url,
        params={
            "pn": page,
            "pz": _PAGE_SIZE,
            "po": 0,
            "np": 1,
            "ut": _UT_TOKEN,
            "fltt": 2,
            "invt": 2,
            "fid": "f12",
            "fs": fs,
            "fields": "f12,f13,f14",
        },
        headers={"User-Agent": "Mozilla/5.0"},
        timeout=_REQUEST_TIMEOUT_SECONDS,
    )
    resp.raise_for_status()
    return resp.json().get("data") or {}


def _fetch_board(fs: str) -> list[dict[str, Any]]:
    """Fetch every row of one category board, trying hosts in order."""
    last_error: Exception | None = None
    for url in _CLIST_HOSTS:
        try:
            return _fetch_board_from(url, fs)
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            logger.warning("em clist host %s failed for fs=%s: %s", url, fs, exc)
    raise RuntimeError(f"all eastmoney clist hosts failed for fs={fs}: {last_error}")


def _fetch_board_from(url: str, fs: str) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    first = _fetch_page(url, fs, 1)
    total = int(first.get("total") or 0)
    rows.extend(first.get("diff") or [])
    page = 2
    while len(rows) < total and page <= _MAX_PAGES:
        time.sleep(_PAGE_SLEEP_SECONDS)
        data = _fetch_page(url, fs, page)
        diff = data.get("diff") or []
        if not diff:
            break
        rows.extend(diff)
        page += 1
    if len(rows) < total:
        raise RuntimeError(f"eastmoney clist fs={fs} returned {len(rows)} of {total} rows")
    return rows


def _to_frame(rows: list[dict[str, Any]], with_market_prefix: bool) -> pd.DataFrame:
    codes: list[str] = []
    names: list[str] = []
    for row in rows:
        code = str(row.get("f12") or "").strip()
        if not code:
            continue
        if with_market_prefix:
            code = f"{row.get('f13')}.{code}"
        codes.append(code)
        names.append(str(row.get("f14") or "").strip())
    return pd.DataFrame({"代码": codes, "名称": names})


def _to_frame_with_market_id(rows: list[dict[str, Any]]) -> pd.DataFrame:
    """Frame with the upstream market id kept as a structured column."""
    codes: list[str] = []
    names: list[str] = []
    market_ids: list[int | None] = []
    for row in rows:
        code = str(row.get("f12") or "").strip()
        if not code:
            continue
        raw_market = row.get("f13")
        try:
            market_id: int | None = int(raw_market)
        except (TypeError, ValueError):
            market_id = None
        codes.append(code)
        names.append(str(row.get("f14") or "").strip())
        market_ids.append(market_id)
    return pd.DataFrame({"代码": codes, "名称": names, "市场标识": market_ids})


def em_hk_equity_list() -> pd.DataFrame:
    """HK main-board + GEM equities (no funds, bonds, warrants or CBBCs)."""
    return _to_frame(_fetch_board(_HK_EQUITY_FS), with_market_prefix=False)


def em_hk_fund_list() -> pd.DataFrame:
    """HK exchange-traded fund board: ETF/L&I/REIT plus currency counters."""
    return _to_frame(_fetch_board(_HK_FUND_FS), with_market_prefix=False)


def em_us_equity_list() -> pd.DataFrame:
    """US common/preferred/ADR/closed-end-fund listings as ``105.AAPL`` codes."""
    return _to_frame(_fetch_board(_US_EQUITY_FS), with_market_prefix=True)


def em_us_etf_list() -> pd.DataFrame:
    """US exchange-traded funds as ``107.SPY`` style codes."""
    return _to_frame(_fetch_board(_US_ETF_FS), with_market_prefix=True)


def em_cn_etf_list() -> pd.DataFrame:
    """CN exchange ETF board with the authoritative per-code market id."""
    return _to_frame_with_market_id(_fetch_board(_CN_ETF_FS))


def em_cn_lof_list() -> pd.DataFrame:
    """CN exchange LOF board with the authoritative per-code market id."""
    return _to_frame_with_market_id(_fetch_board(_CN_LOF_FS))


def em_cn_sh_a_list() -> pd.DataFrame:
    """SSE A-share equities (main board + STAR)."""
    return _to_frame(_fetch_board(_CN_SH_A_FS), with_market_prefix=False)


def em_cn_sz_a_list() -> pd.DataFrame:
    """SZSE A-share equities (main board + ChiNext)."""
    return _to_frame(_fetch_board(_CN_SZ_A_FS), with_market_prefix=False)


def em_cn_bj_a_list() -> pd.DataFrame:
    """BSE listings from the Eastmoney BJ board."""
    return _to_frame(_fetch_board(_CN_BJ_A_FS), with_market_prefix=False)
