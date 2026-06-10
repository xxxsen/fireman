"""Lightweight instrument resolve (spot/metadata only, no history fetch)."""

from __future__ import annotations

import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from ..schemas import ResolveCandidate, ResolveData, ResolveRequest
from ..timeout_util import call_with_timeout, resolve_deadline_seconds, resolve_timeout_seconds
from .cn_code import (
    CNExchangeCode,
    parse_cn_etf_code,
    parse_cn_lof_code,
    parse_cn_stock_code,
)
from .names import _load_etf_name_map, _load_hk_name_map, _load_lof_name_map, _load_stock_name_map
from .symbols import hk_exchange_symbol

_VALID_MARKET_TYPES: dict[str, frozenset[str]] = {
    "CN": frozenset({"cn_exchange_fund", "cn_exchange_stock", "cn_mutual_fund", "fx_rate"}),
    "HK": frozenset({"hk_stock", "hk_etf"}),
    "US": frozenset({"us_stock", "us_etf"}),
}


@dataclass(frozen=True)
class _Candidate:
    code: str
    provider_symbol: str
    name: str
    exchange: str
    instrument_kind: str


def _validate_market_type(market: str, instrument_type: str) -> None:
    allowed = _VALID_MARKET_TYPES.get(market.upper())
    if allowed is None or instrument_type not in allowed:
        raise ValueError("invalid_request")


def _has_exchange_prefix(code: str) -> bool:
    raw = code.strip().lower()
    return raw.startswith(("sh", "sz", "bj"))


def _bare_digits(code: str) -> str:
    raw = code.strip().lower()
    if raw.startswith(("sh", "sz", "bj")):
        raw = raw[2:]
    digits = "".join(ch for ch in raw if ch.isdigit())
    return digits.zfill(6) if digits else raw


def _load_us_name(symbol: str, deadline: float) -> str | None:
    import akshare as ak

    remaining = max(1, int(deadline - time.monotonic()))
    try:
        df = call_with_timeout(lambda: ak.stock_us_spot_em(), remaining)
    except Exception:  # noqa: BLE001
        return None
    code_col = "symbol" if "symbol" in df.columns else "代码" if "代码" in df.columns else None
    name_col = "name" if "name" in df.columns else "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return None
    target = symbol.strip().upper()
    for _, row in df.iterrows():
        code = str(row.get(code_col, "")).strip().upper()
        if code == target:
            name = str(row.get(name_col, "")).strip()
            return name or target
    return None


def _load_cn_mutual_fund_name(symbol: str, deadline: float) -> str | None:
    import akshare as ak

    bare = _bare_digits(symbol)
    remaining = max(1, int(deadline - time.monotonic()))
    try:
        df = call_with_timeout(lambda: ak.fund_name_em(), remaining)
    except Exception:  # noqa: BLE001
        return None
    code_col = "基金代码" if "基金代码" in df.columns else "代码" if "代码" in df.columns else None
    name_col = "基金简称" if "基金简称" in df.columns else "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return None
    for _, row in df.iterrows():
        code = str(row.get(code_col, "")).strip().zfill(6)
        if code == bare:
            name = str(row.get(name_col, "")).strip()
            return name or bare
    return None


def _candidate_from_cn(parsed: CNExchangeCode, name: str, kind: str) -> _Candidate:
    return _Candidate(
        parsed.canonical_code,
        parsed.canonical_code,
        name,
        parsed.exchange,
        kind,
    )


def _dedupe_candidates(items: list[_Candidate]) -> list[_Candidate]:
    seen: set[str] = set()
    out: list[_Candidate] = []
    for item in items:
        if item.provider_symbol in seen:
            continue
        seen.add(item.provider_symbol)
        out.append(item)
    return out


def _load_cn_spot_maps(deadline: float) -> tuple[dict[str, str], dict[str, str], dict[str, str]]:
    etf_map: dict[str, str] = {}
    lof_map: dict[str, str] = {}
    stock_map: dict[str, str] = {}
    with ThreadPoolExecutor(max_workers=3) as pool:
        futures = {
            pool.submit(_load_etf_name_map, deadline): "etf",
            pool.submit(_load_lof_name_map, deadline): "lof",
            pool.submit(_load_stock_name_map, deadline): "stock",
        }
        for fut in as_completed(futures):
            key = futures[fut]
            result = fut.result()
            if key == "etf":
                etf_map = result
            elif key == "lof":
                lof_map = result
            else:
                stock_map = result
    return etf_map, lof_map, stock_map


def _resolve_cn_exchange_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    etf_map, lof_map, stock_map = _load_cn_spot_maps(deadline)

    if _has_exchange_prefix(code):
        prefixed = parse_cn_etf_code(code)
        if prefixed is None:
            return []
        name = etf_map.get(bare) or lof_map.get(bare)
        if not name:
            return []
        kind = "lof" if bare in lof_map and bare not in etf_map else "etf"
        if bare in etf_map and bare not in lof_map:
            kind = "index_etf" if kind == "etf" else kind
        return [_candidate_from_cn(prefixed, name, kind)]

    if bare in etf_map:
        parsed = parse_cn_etf_code(bare)
        if parsed:
            out = [_candidate_from_cn(parsed, etf_map[bare], "index_etf")]
        else:
            out = []
    else:
        out = []
    if bare in lof_map:
        parsed_lof = parse_cn_lof_code(bare)
        if parsed_lof:
            out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))
    if bare in stock_map:
        parsed_stock = parse_cn_stock_code(bare)
        if parsed_stock:
            out.append(_candidate_from_cn(parsed_stock, stock_map[bare], "stock"))

    return _dedupe_candidates(out)


def _resolve_cn_exchange_stock(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    _, _, stock_map = _load_cn_spot_maps(deadline)
    if bare not in stock_map:
        return []

    prefixed = parse_cn_stock_code(code)
    if prefixed is not None:
        return [_candidate_from_cn(prefixed, stock_map[bare], "stock")]

    out: list[_Candidate] = []
    for prefix in ("sh", "sz"):
        candidate_code = f"{prefix}{bare}"
        parsed = parse_cn_stock_code(candidate_code)
        if parsed is not None:
            out.append(_candidate_from_cn(parsed, stock_map[bare], "stock"))
    return _dedupe_candidates(out)


def _resolve_cn_mutual_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    name = _load_cn_mutual_fund_name(bare, deadline)
    if not name:
        return []
    return [_Candidate(bare, bare, name, "", "mutual_fund")]


def _resolve_hk(code: str, kind: str, deadline: float) -> list[_Candidate]:
    sym = hk_exchange_symbol(code)
    name = _load_hk_name_map(deadline).get(sym)
    if not name:
        return []
    instrument_kind = "etf" if kind == "hk_etf" else "stock"
    return [_Candidate(sym, sym, name, "HK", instrument_kind)]


def _resolve_us(code: str, kind: str, deadline: float) -> list[_Candidate]:
    sym = code.strip().upper()
    name = _load_us_name(sym, deadline)
    if not name:
        return []
    instrument_kind = "etf" if kind == "us_etf" else "stock"
    return [_Candidate(sym, sym, name, "US", instrument_kind)]


def resolve_instrument(req: ResolveRequest) -> ResolveData:
    code = req.code.strip()
    if not code:
        raise ValueError("code is required")

    _validate_market_type(req.market, req.instrument_type)

    deadline = time.monotonic() + resolve_deadline_seconds()

    resolver_map = {
        "cn_exchange_fund": lambda c: _resolve_cn_exchange_fund(c, deadline),
        "cn_exchange_stock": lambda c: _resolve_cn_exchange_stock(c, deadline),
        "cn_mutual_fund": lambda c: _resolve_cn_mutual_fund(c, deadline),
        "hk_stock": lambda c: _resolve_hk(c, "hk_stock", deadline),
        "hk_etf": lambda c: _resolve_hk(c, "hk_etf", deadline),
        "us_stock": lambda c: _resolve_us(c, "us_stock", deadline),
        "us_etf": lambda c: _resolve_us(c, "us_etf", deadline),
    }
    resolver = resolver_map.get(req.instrument_type)
    if resolver is None:
        raise ValueError(f"unsupported instrument_type {req.instrument_type}")

    found = resolver(code)
    if not found:
        raise ValueError("instrument_not_found")

    if len(found) == 1:
        c = found[0]
        return ResolveData(
            ambiguous=False,
            resolved=ResolveCandidate(
                code=c.code,
                provider_symbol=c.provider_symbol,
                name=c.name,
                exchange=c.exchange,
                instrument_kind=c.instrument_kind,
            ),
        )

    return ResolveData(
        ambiguous=True,
        candidates=[
            ResolveCandidate(
                code=c.code,
                provider_symbol=c.provider_symbol,
                name=c.name,
                exchange=c.exchange,
                instrument_kind=c.instrument_kind,
            )
            for c in found
        ],
    )
