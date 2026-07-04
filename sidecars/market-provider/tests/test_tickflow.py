"""TickFlow priority fetch tests (td/074) — all network mocked."""

from unittest.mock import patch

import pandas as pd
import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.tickflow import (
    TICKFLOW_KLINES_SOURCE,
    tickflow_allowed_for_request,
    tickflow_base_url,
    tickflow_enabled,
    tickflow_enabled_types,
    tickflow_require_adjust_none,
    tickflow_symbol,
    tickflow_timeout_seconds,
    try_tickflow_klines,
)
from fireman_market_provider.schemas import FetchRequest

# 2024-01-02 / 2024-01-03 / 2024-01-04 at Beijing midnight, in UTC epoch ms.
_TS_20240102 = 1704124800000
_TS_20240103 = 1704211200000
_TS_20240104 = 1704297600000


def _client() -> TestClient:
    return TestClient(create_app())


def _payload(
    timestamps: list[int],
    closes: list[float],
    *,
    opens: list[float] | None = None,
) -> dict:
    n = len(timestamps)
    return {
        "data": {
            "timestamp": timestamps,
            "open": opens if opens is not None else [c - 0.5 for c in closes],
            "high": [c + 1 for c in closes],
            "low": [c - 1 for c in closes],
            "close": closes,
            "volume": [100] * n,
            "amount": [1000.0] * n,
        }
    }


def _stock_request(**overrides) -> FetchRequest:
    base = {
        "market": "CN",
        "instrument_type": "cn_exchange_stock",
        "source_code": "600519",
        "start_date": "2024-01-01",
        "end_date": "2024-12-31",
        "adjust_policy": "none",
    }
    base.update(overrides)
    return FetchRequest(**base)


def _enable_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_ENABLED", "true")


# ---------------------------------------------------------------------------
# Config helpers
# ---------------------------------------------------------------------------


def test_tickflow_disabled_by_default(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_ENABLED", raising=False)
    assert tickflow_enabled() is False
    assert tickflow_allowed_for_request(_stock_request()) is False


def test_tickflow_config_defaults(monkeypatch: pytest.MonkeyPatch) -> None:
    for key in (
        "MARKET_PROVIDER_TICKFLOW_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_TIMEOUT",
        "MARKET_PROVIDER_TICKFLOW_TYPES",
        "MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE",
    ):
        monkeypatch.delenv(key, raising=False)
    assert tickflow_base_url() == "https://free-api.tickflow.org"
    assert tickflow_timeout_seconds() == 8.0
    assert tickflow_enabled_types() == {"cn_exchange_stock", "cn_exchange_fund"}
    assert tickflow_require_adjust_none() is True


def test_tickflow_config_overrides(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", "http://localhost:9999/")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TIMEOUT", "3s")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TYPES", "cn_exchange_stock")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "false")
    assert tickflow_base_url() == "http://localhost:9999"
    assert tickflow_timeout_seconds() == 3.0
    assert tickflow_enabled_types() == {"cn_exchange_stock"}
    assert tickflow_require_adjust_none() is False


def test_tickflow_symbol_mapping() -> None:
    assert tickflow_symbol("600000", "SH") == "600000.SH"
    assert tickflow_symbol("000001", "sz") == "000001.SZ"
    assert tickflow_symbol("830799", "BJ") == "830799.BJ"


# ---------------------------------------------------------------------------
# Gating rules
# ---------------------------------------------------------------------------


def test_gate_requires_adjust_none_by_default(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    assert tickflow_allowed_for_request(_stock_request(adjust_policy="none")) is True
    assert tickflow_allowed_for_request(_stock_request(adjust_policy="qfq")) is False
    assert tickflow_allowed_for_request(_stock_request(adjust_policy="hfq")) is False


def test_gate_fund_kind(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)

    def fund_req(kind: str | None) -> FetchRequest:
        return _stock_request(
            instrument_type="cn_exchange_fund", source_code="510300", instrument_kind=kind
        )

    assert tickflow_allowed_for_request(fund_req("etf")) is True
    assert tickflow_allowed_for_request(fund_req("index_etf")) is True
    assert tickflow_allowed_for_request(fund_req("lof")) is False
    assert tickflow_allowed_for_request(fund_req("")) is False
    assert tickflow_allowed_for_request(fund_req(None)) is False


def test_gate_type_allowlist(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TYPES", "cn_exchange_fund")
    assert tickflow_allowed_for_request(_stock_request()) is False


def test_gate_never_allows_mutual_fund(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TYPES", "cn_mutual_fund")
    req = _stock_request(instrument_type="cn_mutual_fund", source_code="110022")
    assert tickflow_allowed_for_request(req) is False


# ---------------------------------------------------------------------------
# Kline payload conversion
# ---------------------------------------------------------------------------


def test_try_tickflow_klines_converts_payload(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    payload = _payload(
        [_TS_20240103, _TS_20240102, _TS_20240104],
        [10.3, 10.2, 10.4],
        opens=[9.3, 9.2, 9.4],
    )
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload
    ) as mock_get:
        df = try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231")
    assert df is not None
    # Beijing-midnight timestamps must map to the Beijing calendar date, ascending.
    assert [str(d) for d in df["日期"]] == ["2024-01-02", "2024-01-03", "2024-01-04"]
    # Close (not open) is the value column consumed by normalize_dataframe.
    assert list(df["收盘"]) == [10.2, 10.3, 10.4]
    params = mock_get.call_args[0][1]
    assert params["symbol"] == "600519.SH"
    assert params["period"] == "1d"
    assert params["adjust"] == "none"


def test_try_tickflow_klines_filters_to_requested_range(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    payload = _payload([_TS_20240102, _TS_20240103, _TS_20240104], [10.2, 10.3, 10.4])
    with patch("fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload):
        df = try_tickflow_klines(_stock_request(), "600519.SH", "20240103", "20240103")
    assert df is not None
    assert [str(d) for d in df["日期"]] == ["2024-01-03"]

    with patch("fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload):
        empty = try_tickflow_klines(_stock_request(), "600519.SH", "20250101", "20251231")
    assert empty is None


@pytest.mark.parametrize(
    "payload",
    [
        {},
        {"data": None},
        {"data": {"timestamp": []}},
        {"data": {"timestamp": [_TS_20240102], "open": [1.0], "high": [1.0], "low": [1.0], "close": []}},
        {"data": {"timestamp": ["not-a-ts"], "open": [1.0], "high": [1.0], "low": [1.0], "close": [1.0]}},
        {"data": {"symbol": "000001.SZ", "timestamp": [_TS_20240102], "open": [1.0], "high": [1.0], "low": [1.0], "close": [1.0]}},
    ],
)
def test_try_tickflow_klines_invalid_payloads_miss(
    monkeypatch: pytest.MonkeyPatch, payload: dict
) -> None:
    _enable_tickflow(monkeypatch)
    with patch("fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231") is None


def test_try_tickflow_klines_maps_adjust_policy(monkeypatch: pytest.MonkeyPatch) -> None:
    """When the adjust-none gate is lifted, qfq/hfq map to TickFlow's enum values."""
    _enable_tickflow(monkeypatch)
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "false")
    payload = _payload([_TS_20240102], [10.2])
    for policy, expected in (("none", "none"), ("qfq", "forward"), ("hfq", "backward")):
        with patch(
            "fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload
        ) as mock_get:
            df = try_tickflow_klines(
                _stock_request(adjust_policy=policy), "600519.SH", "20240101", "20241231"
            )
        assert df is not None
        assert mock_get.call_args[0][1]["adjust"] == expected


def test_try_tickflow_klines_timeout_returns_none(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=TimeoutError("timed out"),
    ):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231") is None


def test_http_get_json_sends_explicit_user_agent(monkeypatch: pytest.MonkeyPatch) -> None:
    """TickFlow's edge rejects the default Python-urllib UA with 403 (live finding)."""
    import io
    import json as jsonlib

    from fireman_market_provider.adapters import tickflow as tickflow_module

    captured: dict[str, str] = {}

    class _FakeResponse(io.BytesIO):
        status = 200

        def __enter__(self):
            return self

        def __exit__(self, *args):
            return False

    def _fake_urlopen(request, timeout):
        captured["user_agent"] = request.get_header("User-agent") or ""
        captured["timeout"] = timeout
        return _FakeResponse(jsonlib.dumps({"data": {}}).encode())

    monkeypatch.setattr(tickflow_module.urllib.request, "urlopen", _fake_urlopen)
    tickflow_module._http_get_json("/v1/klines", {"symbol": "600519.SH"})
    assert captured["user_agent"], "User-Agent header must be set explicitly"
    assert not captured["user_agent"].lower().startswith("python-urllib")


# ---------------------------------------------------------------------------
# Fetch integration through the API (mocked upstreams)
# ---------------------------------------------------------------------------


def test_fetch_stock_prefers_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    payload = _payload([_TS_20240102, _TS_20240103], [10.2, 10.3])
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json", return_value=payload
    ), patch(
        "akshare.stock_zh_a_hist",
        side_effect=AssertionError("akshare must not be called on tickflow hit"),
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "resolved_name": "贵州茅台",
                "start_date": "2024-01-01",
                "end_date": "2024-12-31",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["source_name"] == TICKFLOW_KLINES_SOURCE
    assert body["data"]["name"] == "贵州茅台"
    assert body["data"]["asset_class"] == "equity"
    assert body["data"]["currency"] == "CNY"
    assert body["data"]["point_type"] == "adjusted_close"
    assert [p["value"] for p in body["data"]["points"]] == [10.2, 10.3]
    assert [p["date"] for p in body["data"]["points"]] == ["2024-01-02", "2024-01-03"]


def test_fetch_stock_tickflow_empty_falls_back_to_akshare(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [11.0]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        return_value=_payload([], []),
    ), patch("akshare.stock_zh_a_hist", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    body = response.json()
    assert body["data"]["source_name"] == "ak.stock_zh_a_hist"
    assert len(body["data"]["points"]) == 1


def test_fetch_stock_tickflow_timeout_falls_back_to_akshare(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [11.0]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=TimeoutError("tickflow timed out"),
    ), patch("akshare.stock_zh_a_hist", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.stock_zh_a_hist"


def test_fetch_stock_qfq_does_not_call_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [11.0]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=AssertionError("tickflow must not be called for qfq"),
    ), patch("akshare.stock_zh_a_hist", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.stock_zh_a_hist"


def test_fetch_etf_kind_prefers_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    payload = _payload([_TS_20240102], [4.959])
    captured: dict[str, str] = {}

    def _get_json(path: str, params: dict[str, str]) -> dict:
        captured["path"] = path
        captured["symbol"] = params["symbol"]
        return payload

    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json", side_effect=_get_json
    ), patch(
        "akshare.fund_etf_hist_em",
        side_effect=AssertionError("akshare must not be called on tickflow hit"),
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "sh510300",
                "resolved_name": "沪深300ETF",
                "instrument_kind": "etf",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    body = response.json()
    assert body["data"]["source_name"] == TICKFLOW_KLINES_SOURCE
    assert body["data"]["name"] == "沪深300ETF"
    assert body["data"]["provider_symbol"] == "sh510300"
    assert captured == {"path": "/v1/klines", "symbol": "510300.SH"}


def test_fetch_lof_kind_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.2]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=AssertionError("tickflow must not be called for lof"),
    ), patch("akshare.fund_lof_hist_em", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "sz161725",
                "instrument_kind": "lof",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.fund_lof_hist_em"


def test_fetch_unknown_fund_kind_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.2]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=AssertionError("tickflow must not be called for unknown kind"),
    ), patch("akshare.fund_etf_hist_em", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "sh510300",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.fund_etf_hist_em"


def test_fetch_mutual_fund_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    monkeypatch.setenv(
        "MARKET_PROVIDER_TICKFLOW_TYPES",
        "cn_exchange_stock,cn_exchange_fund,cn_mutual_fund",
    )
    open_df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [3.45],
            "基金简称": ["易方达消费行业"],
            "基金类型": ["股票型基金"],
        }
    )
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=AssertionError("tickflow must never serve cn_mutual_fund"),
    ), patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value="易方达消费行业",
    ), patch("akshare.fund_open_fund_info_em", return_value=open_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "110022",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"].startswith("ak.fund_open_fund_info_em")


def test_fetch_default_config_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_ENABLED", raising=False)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [11.0]})
    with patch(
        "fireman_market_provider.adapters.tickflow._http_get_json",
        side_effect=AssertionError("tickflow must not be called when disabled"),
    ), patch("akshare.stock_zh_a_hist", return_value=ak_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.stock_zh_a_hist"
