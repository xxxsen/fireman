"""TickFlow priority fetch tests (official SDK based) — all network mocked."""

from types import SimpleNamespace
from unittest.mock import patch

import pandas as pd
import pytest
from fastapi.testclient import TestClient
from tickflow import RateLimitError
from tickflow import TimeoutError as TickFlowTimeoutError

from fireman_market_provider import create_app
from fireman_market_provider.adapters import tickflow as tickflow_module
from fireman_market_provider.adapters.tickflow import (
    TICKFLOW_KLINES_SOURCE,
    get_tickflow_client,
    reset_tickflow_client,
    tickflow_allowed_for_request,
    tickflow_api_key,
    tickflow_base_url,
    tickflow_enabled,
    tickflow_enabled_types,
    tickflow_max_retries,
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
_MS_PER_DAY = 86400000


@pytest.fixture(autouse=True)
def _clean_client_cache():
    reset_tickflow_client()
    yield
    reset_tickflow_client()


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
        "timestamp": timestamps,
        "open": opens if opens is not None else [c - 0.5 for c in closes],
        "high": [c + 1 for c in closes],
        "low": [c - 1 for c in closes],
        "close": closes,
        "volume": [100] * n,
        "amount": [1000.0] * n,
    }


class _FakeClient:
    """Stub for the TickFlow SDK client capturing klines.get calls."""

    def __init__(self, result=None, error: Exception | None = None):
        self.calls: list[dict] = []
        self.closed = False
        self._result = result
        self._error = error

        def _get(symbol, **kwargs):
            self.calls.append({"symbol": symbol, **kwargs})
            if self._error is not None:
                raise self._error
            return self._result

        self.klines = SimpleNamespace(get=_get)

    def close(self) -> None:
        self.closed = True


def _patch_client(fake: _FakeClient):
    return patch.object(tickflow_module, "get_tickflow_client", return_value=fake)


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
        "MARKET_PROVIDER_TICKFLOW_API_KEY",
        "MARKET_PROVIDER_TICKFLOW_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_TIMEOUT",
        "MARKET_PROVIDER_TICKFLOW_MAX_RETRIES",
        "MARKET_PROVIDER_TICKFLOW_TYPES",
        "MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE",
    ):
        monkeypatch.delenv(key, raising=False)
    assert tickflow_api_key() == ""
    assert tickflow_base_url() == "https://free-api.tickflow.org"
    assert tickflow_timeout_seconds() == 8.0
    assert tickflow_max_retries() == 0
    assert tickflow_enabled_types() == {"cn_exchange_stock", "cn_exchange_fund"}
    assert tickflow_require_adjust_none() is True


def test_tickflow_base_url_selection(monkeypatch: pytest.MonkeyPatch) -> None:
    for key in (
        "MARKET_PROVIDER_TICKFLOW_API_KEY",
        "MARKET_PROVIDER_TICKFLOW_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL",
        "MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL",
    ):
        monkeypatch.delenv(key, raising=False)
    # No key: free endpoint.
    assert tickflow_base_url() == "https://free-api.tickflow.org"
    # Key present: paid endpoint.
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", "unit-test-key")
    assert tickflow_base_url() == "https://api.tickflow.org"
    # Explicit override beats both.
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", "http://localhost:9999/")
    assert tickflow_base_url() == "http://localhost:9999"
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_API_KEY")
    assert tickflow_base_url() == "http://localhost:9999"


def test_tickflow_config_overrides(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL", "http://free.local/")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL", "http://paid.local/")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TIMEOUT", "3s")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_MAX_RETRIES", "2")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_TYPES", "cn_exchange_stock")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "false")
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_API_KEY", raising=False)
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", raising=False)
    assert tickflow_base_url() == "http://free.local"
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", "unit-test-key")
    assert tickflow_base_url() == "http://paid.local"
    assert tickflow_timeout_seconds() == 3.0
    assert tickflow_max_retries() == 2
    assert tickflow_enabled_types() == {"cn_exchange_stock"}
    assert tickflow_require_adjust_none() is False


def test_tickflow_symbol_mapping() -> None:
    assert tickflow_symbol("600000", "SH") == "600000.SH"
    assert tickflow_symbol("000001", "sz") == "000001.SZ"
    assert tickflow_symbol("830799", "BJ") == "830799.BJ"


# ---------------------------------------------------------------------------
# SDK client construction and caching
# ---------------------------------------------------------------------------


def test_client_free_tier_without_key(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_API_KEY", raising=False)
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", raising=False)
    # Ambient SDK env vars must not leak into the sidecar client.
    monkeypatch.setenv("TICKFLOW_API_KEY", "ambient-key-must-be-ignored")
    monkeypatch.setenv("TICKFLOW_BASE_URL", "http://ambient.example")
    client = get_tickflow_client()
    assert client.base_url == "https://free-api.tickflow.org"
    assert not client.api_key


def test_client_paid_tier_with_key(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", "unit-test-key")
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", raising=False)
    client = get_tickflow_client()
    assert client.base_url == "https://api.tickflow.org"
    assert client.api_key == "unit-test-key"


def test_client_explicit_base_url_override(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", "unit-test-key")
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", "http://localhost:9999")
    client = get_tickflow_client()
    assert client.base_url == "http://localhost:9999"


def test_client_cached_and_rebuilt_on_config_change(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_API_KEY", raising=False)
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_BASE_URL", raising=False)
    first = get_tickflow_client()
    assert get_tickflow_client() is first
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", "unit-test-key")
    second = get_tickflow_client()
    assert second is not first
    assert second.base_url == "https://api.tickflow.org"


def test_api_key_not_in_fallback_logs(
    monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    _enable_tickflow(monkeypatch)
    secret = "super-secret-key-value"
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_API_KEY", secret)
    fake = _FakeClient(error=RateLimitError("rate limited", code="RATE_LIMIT", status_code=429))
    with _patch_client(fake), caplog.at_level("DEBUG"):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231") is None
    assert secret not in caplog.text


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
    fake = _FakeClient(
        result=_payload(
            [_TS_20240103, _TS_20240102, _TS_20240104],
            [10.3, 10.2, 10.4],
            opens=[9.3, 9.2, 9.4],
        )
    )
    with _patch_client(fake):
        df = try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231")
    assert df is not None
    # Beijing-midnight timestamps must map to the Beijing calendar date, ascending.
    assert [str(d) for d in df["日期"]] == ["2024-01-02", "2024-01-03", "2024-01-04"]
    # Close (not open) is the value column consumed by normalize_dataframe.
    assert list(df["收盘"]) == [10.2, 10.3, 10.4]
    call = fake.calls[0]
    assert call["symbol"] == "600519.SH"
    assert call["period"] == "1d"
    assert call["adjust"] == "none"
    assert call["count"] == 10000
    assert call["as_dataframe"] is False
    assert isinstance(call["start_time"], int)
    assert isinstance(call["end_time"], int)


def test_try_tickflow_klines_filters_to_requested_range(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    payload = _payload([_TS_20240102, _TS_20240103, _TS_20240104], [10.2, 10.3, 10.4])
    with _patch_client(_FakeClient(result=payload)):
        df = try_tickflow_klines(_stock_request(), "600519.SH", "20240103", "20240103")
    assert df is not None
    assert [str(d) for d in df["日期"]] == ["2024-01-03"]

    with _patch_client(_FakeClient(result=payload)):
        empty = try_tickflow_klines(_stock_request(), "600519.SH", "20250101", "20251231")
    assert empty is None


@pytest.mark.parametrize(
    "payload",
    [
        None,
        [],
        {},
        {"timestamp": []},
        {"timestamp": [_TS_20240102], "open": [1.0], "high": [1.0], "low": [1.0], "close": []},
        {"timestamp": ["not-a-ts"], "open": [1.0], "high": [1.0], "low": [1.0], "close": [1.0]},
        {"symbol": "000001.SZ", "timestamp": [_TS_20240102], "open": [1.0], "high": [1.0], "low": [1.0], "close": [1.0]},
    ],
)
def test_try_tickflow_klines_invalid_payloads_miss(
    monkeypatch: pytest.MonkeyPatch, payload
) -> None:
    _enable_tickflow(monkeypatch)
    with _patch_client(_FakeClient(result=payload)):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231") is None


def test_try_tickflow_klines_maps_adjust_policy(monkeypatch: pytest.MonkeyPatch) -> None:
    """When the adjust-none gate is lifted, qfq/hfq map to the SDK adjust enum."""
    _enable_tickflow(monkeypatch)
    monkeypatch.setenv("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "false")
    for policy, expected in (("none", "none"), ("qfq", "forward"), ("hfq", "backward")):
        fake = _FakeClient(result=_payload([_TS_20240102], [10.2]))
        with _patch_client(fake):
            df = try_tickflow_klines(
                _stock_request(adjust_policy=policy), "600519.SH", "20240101", "20241231"
            )
        assert df is not None
        assert fake.calls[0]["adjust"] == expected


@pytest.mark.parametrize(
    "error",
    [
        TickFlowTimeoutError("request timed out"),
        RateLimitError("rate limited", code="RATE_LIMIT", status_code=429),
        RuntimeError("unexpected sdk failure"),
    ],
)
def test_try_tickflow_klines_sdk_errors_return_none(
    monkeypatch: pytest.MonkeyPatch, error: Exception
) -> None:
    _enable_tickflow(monkeypatch)
    with _patch_client(_FakeClient(error=error)):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "20240101", "20241231") is None


def test_try_tickflow_klines_truncation_guard(monkeypatch: pytest.MonkeyPatch) -> None:
    """A capped response starting after the requested range start is a miss.

    The SDK returns at most 10000 rows per request; when the cap is filled and
    the earliest bar is later than the requested start, older history may have
    been cut off server-side and the data must not pass as full history.
    """
    _enable_tickflow(monkeypatch)
    n = 10000
    timestamps = [_TS_20240102 + i * _MS_PER_DAY for i in range(n)]
    payload = _payload(timestamps, [10.0 + (i % 50) * 0.01 for i in range(n)])
    with _patch_client(_FakeClient(result=payload)):
        assert try_tickflow_klines(_stock_request(), "600519.SH", "19900101", "20991231") is None

    # Same capped response is a valid hit when it actually reaches the range start.
    with _patch_client(_FakeClient(result=payload)):
        df = try_tickflow_klines(_stock_request(), "600519.SH", "20240102", "20991231")
    assert df is not None
    assert len(df) == n


# ---------------------------------------------------------------------------
# Fetch integration through the API (mocked SDK)
# ---------------------------------------------------------------------------


def test_fetch_stock_prefers_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    fake = _FakeClient(result=_payload([_TS_20240102, _TS_20240103], [10.2, 10.3]))
    with _patch_client(fake), patch(
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
    with _patch_client(_FakeClient(result=_payload([], []))), patch(
        "akshare.stock_zh_a_hist", return_value=ak_df
    ):
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
    with _patch_client(_FakeClient(error=TickFlowTimeoutError("tickflow timed out"))), patch(
        "akshare.stock_zh_a_hist", return_value=ak_df
    ):
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
    fake = _FakeClient(error=AssertionError("tickflow must not be called for qfq"))
    with _patch_client(fake), patch("akshare.stock_zh_a_hist", return_value=ak_df):
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
    assert fake.calls == []


def test_fetch_etf_kind_prefers_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    fake = _FakeClient(result=_payload([_TS_20240102], [4.959]))
    with _patch_client(fake), patch(
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
    assert fake.calls[0]["symbol"] == "510300.SH"


def test_fetch_lof_kind_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.2]})
    fake = _FakeClient(error=AssertionError("tickflow must not be called for lof"))
    with _patch_client(fake), patch("akshare.fund_lof_hist_em", return_value=ak_df):
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
    assert fake.calls == []


def test_fetch_unknown_fund_kind_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    _enable_tickflow(monkeypatch)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.2]})
    fake = _FakeClient(error=AssertionError("tickflow must not be called for unknown kind"))
    with _patch_client(fake), patch("akshare.fund_etf_hist_em", return_value=ak_df):
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
    assert fake.calls == []


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
    fake = _FakeClient(error=AssertionError("tickflow must never serve cn_mutual_fund"))
    with _patch_client(fake), patch(
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
    assert fake.calls == []


def test_fetch_default_config_never_calls_tickflow(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_TICKFLOW_ENABLED", raising=False)
    ak_df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [11.0]})
    fake = _FakeClient(error=AssertionError("tickflow must not be called when disabled"))
    with _patch_client(fake), patch("akshare.stock_zh_a_hist", return_value=ak_df):
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
    assert fake.calls == []
