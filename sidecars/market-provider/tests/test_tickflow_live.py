"""Optional live TickFlow smoke + reconciliation tests (td/074 §7.3/§7.4).

Not run in default CI. Run explicitly:

    FIREMAN_LIVE_TICKFLOW=1 MARKET_PROVIDER_TICKFLOW_ENABLED=true \
        uv run pytest -m live tests/test_tickflow_live.py
"""

import os

import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.tickflow import (
    fetch_tickflow_instruments,
    try_tickflow_klines,
)
from fireman_market_provider.schemas import FetchRequest

pytestmark = [
    pytest.mark.live,
    pytest.mark.skipif(
        os.getenv("FIREMAN_LIVE_TICKFLOW") != "1",
        reason="set FIREMAN_LIVE_TICKFLOW=1 to run",
    ),
]

_EXCHANGE_SAMPLES = ["510300.SH", "159915.SZ", "600000.SH", "000001.SZ"]


def _request(instrument_type: str, source_code: str, kind: str | None = None) -> FetchRequest:
    return FetchRequest(
        market="CN",
        instrument_type=instrument_type,  # type: ignore[arg-type]
        source_code=source_code,
        start_date="2024-01-01",
        end_date="2026-07-04",
        adjust_policy="none",
        instrument_kind=kind,
    )


@pytest.mark.parametrize("symbol", _EXCHANGE_SAMPLES)
def test_live_exchange_samples_have_instruments_and_klines(symbol: str) -> None:
    instruments = fetch_tickflow_instruments([symbol])
    assert instruments, f"expected instrument metadata for {symbol}"
    assert instruments[0].get("symbol") == symbol

    bare, _exchange = symbol.split(".")
    instrument_type = "cn_exchange_fund" if bare.startswith(("51", "15")) else "cn_exchange_stock"
    kind = "etf" if instrument_type == "cn_exchange_fund" else None
    df = try_tickflow_klines(_request(instrument_type, bare, kind), symbol, "20240101", "20260704")
    assert df is not None and not df.empty, f"expected klines for {symbol}"


def test_live_mutual_fund_sample_is_unsupported() -> None:
    """110022.OF must return empty metadata or empty klines — never usable data."""
    instruments = fetch_tickflow_instruments(["110022.OF"])
    df = try_tickflow_klines(
        _request("cn_exchange_stock", "110022"), "110022.OF", "20240101", "20260704"
    )
    assert not instruments or df is None


def test_live_reconciliation_tickflow_vs_akshare(monkeypatch: pytest.MonkeyPatch) -> None:
    """Close reconciliation over the most recent 60 trading days, adjust=none.

    The AKShare reference series comes from the sidecar's own fetch path with
    TickFlow disabled, so the comparison uses the same fallback chain production
    would. Symbols whose AKShare upstreams are all unreachable are skipped —
    without a reference series there is nothing to reconcile.
    """
    import pandas as pd

    compared = 0
    for symbol in _EXCHANGE_SAMPLES:
        bare, _exchange = symbol.split(".")
        is_fund = bare.startswith(("51", "15"))
        instrument_type = "cn_exchange_fund" if is_fund else "cn_exchange_stock"
        kind = "etf" if is_fund else None

        tf_df = try_tickflow_klines(
            _request(instrument_type, bare, kind), symbol, "20240101", "20261231"
        )
        assert tf_df is not None, f"tickflow returned no data for {symbol}"
        tf_tail = tf_df.tail(60)
        tf_close = {str(d): float(c) for d, c in zip(tf_tail["日期"], tf_tail["收盘"])}

        monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_ENABLED", "false")
        client = TestClient(create_app())
        response = client.post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": instrument_type,
                "source_code": bare,
                "instrument_kind": kind,
                "start_date": "2024-01-01",
                "end_date": "2026-12-31",
                "adjust_policy": "none",
            },
        )
        monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_ENABLED", "true")
        if response.status_code != 200 or response.json().get("code") != 0:
            continue  # AKShare reference unavailable for this symbol right now.
        ak_points = response.json()["data"]["points"][-60:]
        ak_close = {p["date"]: float(p["value"]) for p in ak_points}
        compared += 1

        shared = sorted(set(tf_close) & set(ak_close))
        assert len(shared) >= int(0.95 * min(len(tf_close), len(ak_close))), (
            f"{symbol}: date intersection below 95% (tf={len(tf_close)} ak={len(ak_close)} shared={len(shared)})"
        )

        tf_last = max(tf_close)
        ak_last = max(ak_close)
        lag_days = (pd.to_datetime(ak_last) - pd.to_datetime(tf_last)).days
        assert lag_days <= 1, f"{symbol}: tickflow last day {tf_last} lags akshare {ak_last}"

        for day in shared:
            a, b = tf_close[day], ak_close[day]
            rel = abs(a - b) / max(abs(b), 1e-9)
            assert rel <= 0.005, f"{symbol} {day}: close mismatch tickflow={a} akshare={b} rel={rel:.4%}"

    if compared == 0:
        pytest.skip("no AKShare reference series reachable; reconciliation not performed")


def test_live_unreachable_tickflow_falls_back_to_akshare(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Broken TickFlow endpoint must not break the fetch — AKShare covers it."""
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_ENABLED", "true")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", "http://127.0.0.1:9")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TIMEOUT", "2")
    client = TestClient(create_app())
    response = client.post(
        "/v1/instruments/fetch",
        json={
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "510300",
            "instrument_kind": "etf",
            "start_date": "2024-01-01",
            "end_date": "2026-07-04",
            "adjust_policy": "none",
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["source_name"].startswith("ak.")
    assert len(body["data"]["points"]) > 0
