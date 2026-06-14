"""Lightweight instrument resolve (spot/metadata only, no history fetch)."""

from __future__ import annotations

import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from ..schemas import ResolveCandidate, ResolveData, ResolveRequest
from ..timeout_util import UpstreamCall, call_with_timeout, resolve_deadline_seconds, resolve_timeout_seconds
from .cn_code import (
    CNExchangeCode,
    build_from_market_id,
    heuristic_cn_stock_from_bare,
    parse_cn_etf_code,
    parse_cn_lof_code,
    parse_cn_stock_code,
)
from .names import (
    _load_etf_name_map,
    _load_hk_name_map,
    _load_lof_name_map,
    _load_stock_name_map,
    get_cn_mutual_fund_name,
    lookup_cn_exchange_fund_name,
    lookup_cn_lof_name,
)
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
        df = call_with_timeout(UpstreamCall("stock_us_spot_em"), remaining)
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


def _maybe_raise_mutual_fund_mismatch(
    code: str,
    bare: str,
    etf_map: dict[str, str],
    lof_map: dict[str, str],
    stock_map: dict[str, str],
) -> None:
    if _has_exchange_prefix(code):
        return
    if bare in etf_map or bare in lof_map or bare in stock_map:
        return
    if get_cn_mutual_fund_name(bare):
        raise ValueError("instrument_type_mismatch")


@dataclass(frozen=True)
class _SpotMaps:
    etf: dict[str, str]
    lof: dict[str, str]
    stock: dict[str, str]
    load_failed: bool


def _load_cn_exchange_spot_maps(deadline: float) -> _SpotMaps:
    etf_map: dict[str, str] = {}
    lof_map: dict[str, str] = {}
    stock_map: dict[str, str] = {}
    load_failed = False
    with ThreadPoolExecutor(max_workers=3) as pool:
        futures = {
            pool.submit(_load_etf_name_map, deadline): "etf",
            pool.submit(_load_lof_name_map, deadline): "lof",
            pool.submit(_load_stock_name_map, deadline): "stock",
        }
        for fut in as_completed(futures):
            key = futures[fut]
            try:
                result = fut.result()
            except (TimeoutError, RuntimeError):
                load_failed = True
                continue
            except Exception:  # noqa: BLE001 - partial spot data is acceptable
                load_failed = True
                continue
            if key == "etf":
                etf_map = result
            elif key == "lof":
                lof_map = result
            else:
                stock_map = result
    return _SpotMaps(etf_map, lof_map, stock_map, load_failed)


def _candidate_from_cn(parsed: CNExchangeCode, name: str, kind: str) -> _Candidate:
    return _Candidate(
        parsed.canonical_code,
        parsed.canonical_code,
        name,
        parsed.exchange,
        kind,
    )


def _candidate_identity(code: str, provider_symbol: str, instrument_kind: str, exchange: str) -> str:
    return f"{code}|{provider_symbol}|{instrument_kind}|{exchange}"


def _to_resolve_candidate(c: _Candidate) -> ResolveCandidate:
    return ResolveCandidate(
        code=c.code,
        provider_symbol=c.provider_symbol,
        name=c.name,
        exchange=c.exchange,
        instrument_kind=c.instrument_kind,
        candidate_id=_candidate_identity(c.code, c.provider_symbol, c.instrument_kind, c.exchange),
    )


def _dedupe_candidates(items: list[_Candidate]) -> list[_Candidate]:
    seen: set[tuple[str, str]] = set()
    out: list[_Candidate] = []
    for item in items:
        key = (item.provider_symbol, item.instrument_kind)
        if key in seen:
            continue
        seen.add(key)
        out.append(item)
    return out


def _stock_candidates_from_bare(bare: str, stock_name: str) -> list[_Candidate]:
    out: list[_Candidate] = []
    for prefix in ("sh", "sz", "bj"):
        parsed_stock = parse_cn_stock_code(f"{prefix}{bare}")
        if parsed_stock is not None:
            out.append(_candidate_from_cn(parsed_stock, stock_name, "stock"))
    return _dedupe_candidates(out)


def _cross_listed_etf_candidate(
    bare: str,
    etf_name: str,
    stock_candidates: list[_Candidate],
) -> _Candidate | None:
    """Same bare code as ETF on one exchange and stock on another (e.g. 000510)."""
    stock_exchanges = {c.exchange for c in stock_candidates if c.instrument_kind == "stock"}
    if len(stock_exchanges) == 1:
        opposite_mid = {"SZ": 1, "SH": 0}.get(next(iter(stock_exchanges)))
        if opposite_mid is not None:
            return _candidate_from_cn(
                build_from_market_id(bare, opposite_mid),
                etf_name,
                "index_etf",
            )
    parsed = parse_cn_etf_code(bare)
    if parsed is None:
        return None
    return _candidate_from_cn(parsed, etf_name, "index_etf")


def _etf_and_stock_candidates_for_bare(
    bare: str,
    *,
    etf_name: str | None,
    in_etf_map: bool,
    in_stock_map: bool,
    stock_name: str | None,
) -> list[_Candidate]:
    stock_part: list[_Candidate] = []
    if in_stock_map and stock_name:
        stock_part = _stock_candidates_from_bare(bare, stock_name)

    out: list[_Candidate] = []
    if in_etf_map and etf_name:
        etf = _cross_listed_etf_candidate(bare, etf_name, stock_part)
        if etf is not None:
            out.append(etf)
    elif stock_part:
        # ETF spot missing but stock exists — still offer the cross-listed ETF candidate.
        name = etf_name or lookup_cn_exchange_fund_name(bare) or bare
        etf = _cross_listed_etf_candidate(bare, name, stock_part)
        if etf is not None:
            out.append(etf)

    out.extend(stock_part)
    return _dedupe_candidates(out)


_SH_FUND_PREFIXES = ("51", "56", "58")
_SZ_FUND_PREFIXES = ("15", "16", "18")
_CROSS_LIST_BARE_PREFIXES = ("00", "30")


def _heuristic_stock_candidates_from_bare(bare: str, stock_name: str) -> list[_Candidate]:
    parsed = heuristic_cn_stock_from_bare(bare)
    if parsed is None:
        return []
    return [_candidate_from_cn(parsed, stock_name, "stock")]


def _fallback_bare_cn_exchange_fund(bare: str, deadline: float) -> list[_Candidate]:
    """Resolve bare fund codes without spot tables."""
    if bare.startswith(_SH_FUND_PREFIXES) or bare.startswith(_SZ_FUND_PREFIXES):
        parsed_etf = parse_cn_etf_code(bare)
        if parsed_etf is not None:
            name = bare
            if time.monotonic() < deadline - 2:
                name = lookup_cn_exchange_fund_name(bare) or bare
            return [_candidate_from_cn(parsed_etf, name, "index_etf")]

    if bare.startswith(_CROSS_LIST_BARE_PREFIXES):
        stock_part = _heuristic_stock_candidates_from_bare(bare, bare)
        if stock_part:
            cross_etf = _cross_listed_etf_candidate(bare, bare, stock_part)
            if cross_etf is not None and cross_etf.exchange != stock_part[0].exchange:
                return _dedupe_candidates([cross_etf, *stock_part])

    parsed_etf = parse_cn_etf_code(bare)
    if parsed_etf is not None:
        return [_candidate_from_cn(parsed_etf, bare, "index_etf")]

    stock_part = _heuristic_stock_candidates_from_bare(bare, bare)
    return stock_part


def _fallback_cn_exchange_fund_candidates(code: str, bare: str, deadline: float) -> list[_Candidate]:
    """Resolve via code parsing when spot tables timed out or missed the symbol."""
    if not _has_exchange_prefix(code):
        fast = _fallback_bare_cn_exchange_fund(bare, deadline)
        if fast:
            return fast

    resolve_code = code.strip().lower()
    parsed_etf = parse_cn_etf_code(resolve_code)
    out: list[_Candidate] = []
    if parsed_etf is not None:
        name = bare
        if time.monotonic() < deadline - 2:
            name = lookup_cn_exchange_fund_name(bare) or bare
        out.append(_candidate_from_cn(parsed_etf, name, "index_etf"))

    if time.monotonic() < deadline - 2:
        try:
            parsed_lof = parse_cn_lof_code(resolve_code, deadline=deadline)
        except Exception:  # noqa: BLE001 - LOF map lookup is best-effort
            parsed_lof = None
        if parsed_lof is not None:
            name = lookup_cn_lof_name(bare) or bare
            out.append(_candidate_from_cn(parsed_lof, name, "lof"))

    return _dedupe_candidates(out)


def _resolve_cn_exchange_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    spots = _load_cn_exchange_spot_maps(deadline)
    etf_map, lof_map, stock_map = spots.etf, spots.lof, spots.stock

    if _has_exchange_prefix(code):
        out: list[_Candidate] = []
        if bare in etf_map:
            parsed_etf = parse_cn_etf_code(code)
            if parsed_etf:
                kind = "etf" if bare in lof_map else "index_etf"
                out.append(_candidate_from_cn(parsed_etf, etf_map[bare], kind))
        if bare in lof_map:
            parsed_lof = parse_cn_lof_code(code, deadline=deadline)
            if parsed_lof:
                out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))
        result = _dedupe_candidates(out)
        if not result and spots.load_failed:
            result = _fallback_cn_exchange_fund_candidates(code, bare, deadline)
        return result

    out: list[_Candidate] = []
    if bare in lof_map:
        parsed_lof = parse_cn_lof_code(bare, deadline=deadline)
        if parsed_lof:
            out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))

    out = _dedupe_candidates(
        _etf_and_stock_candidates_for_bare(
            bare,
            etf_name=etf_map.get(bare),
            in_etf_map=bare in etf_map,
            in_stock_map=bare in stock_map,
            stock_name=stock_map.get(bare),
        )
        + out
    )

    result = out
    if not result and spots.load_failed:
        result = _fallback_cn_exchange_fund_candidates(code, bare, deadline)
    if not result:
        _maybe_raise_mutual_fund_mismatch(code, bare, etf_map, lof_map, stock_map)
    return result


def _resolve_cn_exchange_stock(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    spots = _load_cn_exchange_spot_maps(deadline)
    stock_map = spots.stock
    if bare not in stock_map:
        return []

    prefixed = parse_cn_stock_code(code)
    if prefixed is not None:
        return [_candidate_from_cn(prefixed, stock_map[bare], "stock")]

    out: list[_Candidate] = []
    for prefix in ("sh", "sz", "bj"):
        candidate_code = f"{prefix}{bare}"
        parsed = parse_cn_stock_code(candidate_code)
        if parsed is not None:
            out.append(_candidate_from_cn(parsed, stock_map[bare], "stock"))
    return _dedupe_candidates(out)


def _resolve_cn_mutual_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    name = get_cn_mutual_fund_name(bare)
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
        return ResolveData(ambiguous=False, resolved=_to_resolve_candidate(found[0]))

    return ResolveData(
        ambiguous=True,
        candidates=[_to_resolve_candidate(c) for c in found],
    )
