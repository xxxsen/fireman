"""Executor tests: directory listings, source-pinned history, FX extraction.

Upstream AKShare operations are stubbed through register_test_dispatch; the
autouse conftest fixture routes the executors' call_with_timeout inline so
the stubs apply without subprocesses.
"""

from __future__ import annotations

import pandas as pd
import pytest

from fireman_market_provider.schemas import FetchData, HistoricalPoint
from fireman_market_provider.timeout_util import register_test_dispatch
from fireman_market_provider.worker.errors import SourceUnavailable, TaskFailure
from fireman_market_provider.worker.executors.directory import (
    _cache as directory_cache,
    execute_directory_sync,
)
from fireman_market_provider.worker.executors.fx import execute_fx_sync
from fireman_market_provider.worker.executors.history import execute_history_sync


@pytest.fixture(autouse=True)
def _clear_directory_cache():
    directory_cache.clear()
    yield
    directory_cache.clear()


# --- asset_directory_sync ---


def _register_hk_boards(
    equity: pd.DataFrame | None = None, fund: pd.DataFrame | None = None
) -> None:
    register_test_dispatch(
        "em_hk_equity_list",
        lambda **kwargs: equity
        if equity is not None
        else pd.DataFrame({"代码": ["00700", "5"], "名称": ["腾讯控股", "汇丰控股"]}),
    )
    register_test_dispatch(
        "em_hk_fund_list",
        lambda **kwargs: fund
        if fund is not None
        else pd.DataFrame(
            {
                "代码": ["02800", "09801", "83403", "00823", "87001"],
                "名称": ["盈富基金", "安硕中国-U", "华夏恒ESG-R", "领展房产基金", "汇贤产业信托"],
            }
        ),
    )


class TestDirectorySync:
    def test_hk_stock_combines_equities_and_reits(self) -> None:
        _register_hk_boards(
            equity=pd.DataFrame(
                {"代码": ["00700", "5", "nan"], "名称": ["腾讯控股", "汇丰控股", "x"]}
            )
        )
        result = execute_directory_sync(
            {"scope": "hk_all", "instrument_types": ["hk_stock"]}
        )
        assert result["type"] == "asset_directory_sync"
        assert result["scope"] == "hk_all"
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # digits are zero-padded to 5; the non-numeric row is dropped; REITs
        # and trusts from the fund board join hk_stock, ETFs do not.
        assert set(by_symbol) == {"00700", "00005", "00823", "87001"}
        assert by_symbol["00700"]["instrument_kind"] == "stock"
        assert by_symbol["00700"]["source_name"] == "em.hk_equity_list"
        assert by_symbol["00823"]["instrument_kind"] == "reit"
        assert by_symbol["00823"]["source_name"] == "em.hk_fund_list"
        for asset in by_symbol.values():
            assert asset["market"] == "HK"
            assert asset["instrument_type"] == "hk_stock"
            assert asset["currency"] == "HKD"

    def test_hk_etf_excludes_trusts_and_maps_counter_currencies(self) -> None:
        _register_hk_boards()
        result = execute_directory_sync(
            {"scope": "hk_all", "instrument_types": ["hk_etf"]}
        )
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # REITs/trusts (00823, 87001) stay out of hk_etf.
        assert set(by_symbol) == {"02800", "09801", "83403"}
        for asset in by_symbol.values():
            assert asset["instrument_type"] == "hk_etf"
            assert asset["instrument_kind"] == "etf"
        assert by_symbol["02800"]["currency"] == "HKD"
        assert by_symbol["09801"]["currency"] == "USD"  # -U USD counter
        assert by_symbol["83403"]["currency"] == "CNY"  # -R RMB counter

    def test_hk_all_scope_returns_both_categories(self) -> None:
        _register_hk_boards()
        result = execute_directory_sync(
            {"scope": "hk_all", "instrument_types": ["hk_stock", "hk_etf"]}
        )
        types = {a["instrument_type"] for a in result["assets"]}
        assert types == {"hk_stock", "hk_etf"}

    def test_us_listing_strips_eastmoney_prefix(self) -> None:
        register_test_dispatch(
            "em_us_equity_list",
            lambda **kwargs: pd.DataFrame(
                {"代码": ["105.AAPL", "106.BRK_A"], "名称": ["苹果", "伯克希尔"]}
            ),
        )
        result = execute_directory_sync(
            {"scope": "us_all", "instrument_types": ["us_stock"]}
        )
        assert {a["symbol"] for a in result["assets"]} == {"AAPL", "BRK_A"}
        first = result["assets"][0]
        assert first["instrument_type"] == "us_stock"
        assert first["instrument_kind"] == "stock"
        assert first["source_name"] == "em.us_equity_list"

    def test_us_all_scope_returns_stock_and_etf(self) -> None:
        register_test_dispatch(
            "em_us_equity_list",
            lambda **kwargs: pd.DataFrame({"代码": ["105.AAPL"], "名称": ["苹果"]}),
        )
        register_test_dispatch(
            "em_us_etf_list",
            lambda **kwargs: pd.DataFrame(
                {"代码": ["107.SPY", "105.QQQ"], "名称": ["标普500ETF-SPDR", "纳指100ETF"]}
            ),
        )
        result = execute_directory_sync(
            {"scope": "us_all", "instrument_types": ["us_stock", "us_etf"]}
        )
        etfs = [a for a in result["assets"] if a["instrument_type"] == "us_etf"]
        stocks = [a for a in result["assets"] if a["instrument_type"] == "us_stock"]
        assert {a["symbol"] for a in etfs} == {"SPY", "QQQ"}
        assert {a["symbol"] for a in stocks} == {"AAPL"}
        assert all(a["instrument_kind"] == "etf" for a in etfs)
        assert all(a["currency"] == "USD" for a in etfs)
        assert all(a["source_name"] == "em.us_etf_list" for a in etfs)

    def test_empty_listing_fails_whole_task(self) -> None:
        register_test_dispatch("em_hk_equity_list", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "hk_all", "instrument_types": ["hk_stock"]})
        assert exc.value.error_code == "directory_data_incomplete"

    def test_empty_etf_listing_fails_whole_task(self) -> None:
        register_test_dispatch("em_us_etf_list", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "us_all", "instrument_types": ["us_etf"]})
        assert exc.value.error_code == "directory_data_incomplete"

    def test_upstream_error_maps_to_unavailable(self) -> None:
        def boom(**kwargs):
            raise RuntimeError("rate limited")

        register_test_dispatch("em_hk_equity_list", boom)
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "hk_all", "instrument_types": ["hk_stock"]})
        assert exc.value.error_code == "market_provider_unavailable"

    def test_cache_serves_repeat_and_force_bypasses(self) -> None:
        calls = {"equity": 0, "fund": 0}

        def equity_listing(**kwargs):
            calls["equity"] += 1
            return pd.DataFrame({"代码": ["00700"], "名称": ["腾讯控股"]})

        def fund_listing(**kwargs):
            calls["fund"] += 1
            return pd.DataFrame({"代码": ["02800"], "名称": ["盈富基金"]})

        register_test_dispatch("em_hk_equity_list", equity_listing)
        register_test_dispatch("em_hk_fund_list", fund_listing)
        payload = {"scope": "hk_all", "instrument_types": ["hk_stock"]}
        execute_directory_sync(payload)
        execute_directory_sync(payload)
        assert calls["equity"] == 1
        execute_directory_sync({**payload, "force": True})
        assert calls["equity"] == 2

    def test_missing_payload_fields_rejected(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "", "instrument_types": []})
        assert exc.value.error_code == "invalid_task_payload"


# --- asset_history_sync ---


HISTORY_PAYLOAD = {
    "asset_key": "cn:cn_exchange_fund:sh:510300",
    "market": "CN",
    "instrument_type": "cn_exchange_fund",
    "region_code": "sh",
    "symbol": "510300",
    "adjust_policy": "none",
    "point_type": "adjusted_close",
}


class TestHistorySync:
    def test_pinned_source_merge_window(self) -> None:
        captured: dict = {}

        def etf_hist(**kwargs):
            captured.update(kwargs)
            return pd.DataFrame(
                {
                    "日期": ["2024-01-05", "2024-01-08", "2024-01-09"],
                    "收盘": [3.9, 4.0, 4.1],
                }
            )

        register_test_dispatch("fund_etf_hist_em", etf_hist)
        result = execute_history_sync(
            {
                **HISTORY_PAYLOAD,
                "required_source_name": "ak.fund_etf_hist_em",
                "replacement_mode": "merge",
                "requested_range": "incremental",
                "start_date": "2024-01-08",
            }
        )
        assert result["source_name"] == "ak.fund_etf_hist_em"
        # The pinned window is passed through in compact form.
        assert captured["start_date"] == "20240108"
        # Points before the incremental start date are trimmed off.
        assert result["points"] == [
            {"date": "2024-01-08", "value": 4.0},
            {"date": "2024-01-09", "value": 4.1},
        ]
        assert "no_new_data" not in result

    def test_pinned_source_empty_window_is_no_new_data(self) -> None:
        register_test_dispatch(
            "fund_etf_hist_em",
            lambda **kwargs: pd.DataFrame({"日期": ["2024-01-05"], "收盘": [3.9]}),
        )
        result = execute_history_sync(
            {
                **HISTORY_PAYLOAD,
                "required_source_name": "ak.fund_etf_hist_em",
                "replacement_mode": "merge",
                "start_date": "2024-02-01",
            }
        )
        assert result["points"] == []
        assert result["no_new_data"] is True

    def test_pinned_source_failure_never_falls_back(self) -> None:
        def boom(**kwargs):
            raise RuntimeError("upstream 500")

        register_test_dispatch("fund_etf_hist_em", boom)
        with pytest.raises(SourceUnavailable) as exc:
            execute_history_sync(
                {
                    **HISTORY_PAYLOAD,
                    "required_source_name": "ak.fund_etf_hist_em",
                    "replacement_mode": "merge",
                }
            )
        assert exc.value.error_code == "source_unavailable"

    def test_pinned_source_wrong_type_is_source_unavailable(self) -> None:
        with pytest.raises(SourceUnavailable):
            execute_history_sync(
                {
                    **HISTORY_PAYLOAD,
                    "required_source_name": "ak.stock_hk_hist",
                    "replacement_mode": "merge",
                }
            )

    def test_unpinned_uses_fetch_chain(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def fake_fetch(req):
            assert req.market == "CN"
            assert req.source_code == "sh510300"
            return FetchData(
                provider="akshare",
                provider_symbol="510300",
                name="沪深300ETF",
                asset_class="equity",
                currency="CNY",
                point_type="adjusted_close",
                expense_ratio_status="unavailable",
                source_name="ak.fund_etf_hist_em",
                source_quality="full",
                points=[
                    HistoricalPoint(date="2024-01-08", value=4.0),
                    HistoricalPoint(date="2024-01-09", value=4.1),
                ],
            )

        monkeypatch.setattr(
            "fireman_market_provider.worker.executors.history.fetch_instrument", fake_fetch
        )
        result = execute_history_sync(
            {**HISTORY_PAYLOAD, "replacement_mode": "full", "requested_range": "full"}
        )
        assert result["source_name"] == "ak.fund_etf_hist_em"
        assert len(result["points"]) == 2

    def test_unpinned_failure_maps_to_unavailable(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def boom(req):
            raise RuntimeError("all sources failed")

        monkeypatch.setattr(
            "fireman_market_provider.worker.executors.history.fetch_instrument", boom
        )
        with pytest.raises(TaskFailure) as exc:
            execute_history_sync({**HISTORY_PAYLOAD, "replacement_mode": "full"})
        assert exc.value.error_code == "market_provider_unavailable"

    def test_invalid_payload_rejected(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_history_sync({"asset_key": "", "instrument_type": "", "symbol": ""})
        assert exc.value.error_code == "invalid_task_payload"


# --- fx_rate_sync ---


class TestFXSync:
    def test_extracts_and_scales_boc_rates(self) -> None:
        seen_symbols: list[str] = []

        def boc(**kwargs):
            seen_symbols.append(kwargs["symbol"])
            return pd.DataFrame(
                {
                    "日期": ["2026-07-02", "2026-07-03", "2026-07-03", "bad"],
                    "央行中间价": [721.0, 722.5, 722.9, 700.0],
                }
            )

        register_test_dispatch("currency_boc_sina", boc)
        result = execute_fx_sync({"pairs": ["USDCNY", "HKDCNY"]})
        assert result["type"] == "fx_rate_sync"
        assert result["source_name"] == "ak.currency_boc_sina"
        assert seen_symbols == ["美元", "港币"]
        usd = [r for r in result["rates"] if r["pair"] == "USDCNY"]
        # Quotes are per 100 units; duplicate dates keep the last value and
        # unparseable dates are dropped.
        assert usd == [
            {"date": "2026-07-02", "pair": "USDCNY", "value": 7.21},
            {"date": "2026-07-03", "pair": "USDCNY", "value": 7.229},
        ]

    def test_no_usable_rates_fails(self) -> None:
        register_test_dispatch("currency_boc_sina", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_fx_sync({"pairs": ["USDCNY"]})
        assert exc.value.error_code == "provider_data_incomplete"

    def test_unknown_pair_rejected(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_fx_sync({"pairs": ["EURJPY"]})
        assert exc.value.error_code == "invalid_task_payload"

    def test_empty_pairs_rejected(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_fx_sync({"pairs": []})
        assert exc.value.error_code == "invalid_task_payload"
