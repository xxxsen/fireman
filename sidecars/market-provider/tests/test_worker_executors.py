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


def _hkex_frame() -> pd.DataFrame:
    """HKEX List of Securities rows: structured category/sub-category and the
    per-counter Trading Currency are the authoritative identity fields."""
    return pd.DataFrame(
        {
            "symbol": ["00700", "00005", "00823", "02800", "09801", "83403", "04231"],
            "name_en": [
                "TENCENT",
                "HSBC HOLDINGS",
                "LINK REIT",
                "TRACKER FUND",
                "ISHARES CHINA (USD)",
                "CSOP ESG (RMB)",
                "GOVT BOND",
            ],
            "category": [
                "Equity",
                "Equity",
                "Real Estate Investment Trusts",
                "Exchange Traded Products",
                "Exchange Traded Products",
                "Exchange Traded Products",
                "Debt Securities",
            ],
            "sub_category": [
                "Equity Securities (Main Board)",
                "Equity Securities (Main Board)",
                "",
                "Exchange Traded Funds",
                "Exchange Traded Funds",
                "Exchange Traded Funds",
                "",
            ],
            "currency": ["HKD", "HKD", "HKD", "HKD", "USD", "CNY", "HKD"],
        }
    )


def _register_hk_boards(hkex: pd.DataFrame | None = None) -> None:
    register_test_dispatch(
        "hkex_list_of_securities",
        lambda **kwargs: hkex if hkex is not None else _hkex_frame(),
    )
    register_test_dispatch(
        "em_hk_equity_list",
        lambda **kwargs: pd.DataFrame({"代码": ["00700", "5"], "名称": ["腾讯控股", "汇丰控股"]}),
    )
    register_test_dispatch(
        "em_hk_fund_list",
        lambda **kwargs: pd.DataFrame(
            {"代码": ["02800", "00823"], "名称": ["盈富基金", "领展房产基金"]}
        ),
    )


def _unit_payload(sync_key: str, scope: str, **extra) -> dict:
    return {
        "sync_key": sync_key,
        "scope": scope,
        "instrument_types": [sync_key],
        **extra,
    }


class TestDirectorySync:
    def test_hk_stock_uses_hkex_categories_for_equities_and_reits(self) -> None:
        _register_hk_boards()
        result = execute_directory_sync(_unit_payload("hk_stock", "hk_all"))
        assert result["type"] == "asset_directory_sync"
        assert result["sync_key"] == "hk_stock"
        assert result["scope"] == "hk_all"
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # Equity + REIT categories from HKEX; ETPs and debt stay out.
        assert set(by_symbol) == {"00700", "00005", "00823"}
        assert by_symbol["00700"]["instrument_kind"] == "stock"
        assert by_symbol["00823"]["instrument_kind"] == "reit"
        assert by_symbol["00700"]["source_name"] == "hkex.list_of_securities"
        # Chinese display names come from the Eastmoney boards (display only).
        assert by_symbol["00700"]["name"] == "腾讯控股"
        assert by_symbol["00823"]["name"] == "领展房产基金"
        # No Chinese name available: keep the HKEX English name.
        assert by_symbol["00005"]["name"] == "汇丰控股"
        for asset in by_symbol.values():
            assert asset["market"] == "HK"
            assert asset["instrument_type"] == "hk_stock"
            assert asset["currency"] == "HKD"

    def test_hk_etf_uses_hkex_trading_currency_not_name_suffix(self) -> None:
        _register_hk_boards()
        result = execute_directory_sync(_unit_payload("hk_etf", "hk_all"))
        assert result["sync_key"] == "hk_etf"
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # Only Exchange Traded Products enter hk_etf; REITs/debt stay out.
        assert set(by_symbol) == {"02800", "09801", "83403"}
        for asset in by_symbol.values():
            assert asset["instrument_type"] == "hk_etf"
            assert asset["instrument_kind"] == "etf"
            assert asset["source_name"] == "hkex.list_of_securities"
        # Trading currency is the structured HKEX field, never a -U/-R guess.
        assert by_symbol["02800"]["currency"] == "HKD"
        assert by_symbol["09801"]["currency"] == "USD"
        assert by_symbol["83403"]["currency"] == "CNY"

    def test_hk_missing_currency_rows_are_skipped_not_guessed(self) -> None:
        frame = _hkex_frame()
        frame.loc[frame["symbol"] == "09801", "currency"] = ""
        _register_hk_boards(hkex=frame)
        result = execute_directory_sync(_unit_payload("hk_etf", "hk_all"))
        assert {a["symbol"] for a in result["assets"]} == {"02800", "83403"}

    def test_single_unit_only_calls_its_own_lister(self) -> None:
        """A cn_exchange_fund unit task must not touch stock or mutual-fund
        listers (090: unit-level retry granularity)."""
        calls: set[str] = set()

        def tracked(operation, frame):
            def handler(**kwargs):
                calls.add(operation)
                return frame

            register_test_dispatch(operation, handler)

        tracked(
            "em_cn_etf_list",
            pd.DataFrame({"代码": ["510300"], "名称": ["沪深300ETF"], "市场标识": [1]}),
        )
        tracked(
            "em_cn_lof_list",
            pd.DataFrame({"代码": ["161725"], "名称": ["招商白酒LOF"], "市场标识": [0]}),
        )
        tracked("em_cn_sh_a_list", pd.DataFrame({"代码": ["600000"], "名称": ["浦发银行"]}))
        tracked("fund_name_em", pd.DataFrame({"基金代码": ["000001"], "基金简称": ["华夏成长"]}))

        result = execute_directory_sync(_unit_payload("cn_exchange_fund", "cn_all"))
        assert result["sync_key"] == "cn_exchange_fund"
        assert {a["symbol"] for a in result["assets"]} == {"510300", "161725"}
        assert calls == {"em_cn_etf_list", "em_cn_lof_list"}

    def test_us_listing_strips_eastmoney_prefix(self) -> None:
        register_test_dispatch(
            "em_us_equity_list",
            lambda **kwargs: pd.DataFrame(
                {"代码": ["105.AAPL", "106.BRK_A"], "名称": ["苹果", "伯克希尔"]}
            ),
        )
        result = execute_directory_sync(_unit_payload("us_stock", "us_all"))
        assert {a["symbol"] for a in result["assets"]} == {"AAPL", "BRK_A"}
        first = result["assets"][0]
        assert first["instrument_type"] == "us_stock"
        assert first["instrument_kind"] == "stock"
        assert first["source_name"] == "em.us_equity_list"

    def test_empty_listing_fails_whole_task(self) -> None:
        register_test_dispatch("hkex_list_of_securities", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync(_unit_payload("hk_stock", "hk_all"))
        assert exc.value.error_code == "directory_data_incomplete"
        # Failures carry the sync_key for admin/task-detail triage.
        assert "sync_key=hk_stock" in exc.value.message

    def test_empty_etf_listing_fails_whole_task(self) -> None:
        register_test_dispatch("em_us_etf_list", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync(_unit_payload("us_etf", "us_all"))
        assert exc.value.error_code == "directory_data_incomplete"

    def test_upstream_error_maps_to_unavailable(self) -> None:
        def boom(**kwargs):
            raise RuntimeError("rate limited")

        register_test_dispatch("hkex_list_of_securities", boom)
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync(_unit_payload("hk_stock", "hk_all"))
        assert exc.value.error_code == "market_provider_unavailable"

    def test_cache_serves_repeat_and_force_bypasses(self) -> None:
        calls = {"hkex": 0}

        def hkex_listing(**kwargs):
            calls["hkex"] += 1
            return _hkex_frame()

        _register_hk_boards()
        register_test_dispatch("hkex_list_of_securities", hkex_listing)
        payload = _unit_payload("hk_stock", "hk_all")
        execute_directory_sync(payload)
        execute_directory_sync(payload)
        assert calls["hkex"] == 1
        execute_directory_sync({**payload, "force": True})
        assert calls["hkex"] == 2

    def test_missing_payload_fields_rejected(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "", "instrument_types": []})
        assert exc.value.error_code == "invalid_task_payload"

    def test_missing_sync_key_rejected(self) -> None:
        _register_hk_boards()
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync({"scope": "hk_all", "instrument_types": ["hk_stock"]})
        assert exc.value.error_code == "invalid_task_payload"


# --- asset_history_sync ---


HISTORY_PAYLOAD = {
    "asset_key": "cn:cn_exchange_fund:sh:510300",
    "market": "CN",
    "instrument_type": "cn_exchange_fund",
    "region_code": "sh",
    "symbol": "510300",
    "adjust_policy": "hfq",
    "point_type": "adjusted_close",
}


class TestHistorySync:
    def test_raw_price_cannot_be_labeled_adjusted(self, monkeypatch: pytest.MonkeyPatch) -> None:
        def fake_fetch(req):
            return FetchData(
                provider="akshare",
                provider_symbol="510300",
                name="沪深300ETF",
                currency="CNY",
                point_type="close",
                expense_ratio_status="unavailable",
                source_name="ak.fund_etf_hist_em",
                source_quality="full",
                points=[HistoricalPoint(date="2024-01-08", value=4.0)],
            )

        monkeypatch.setattr(
            "fireman_market_provider.worker.executors.history.fetch_instrument", fake_fetch
        )
        with pytest.raises(TaskFailure) as exc:
            execute_history_sync(
                {
                    **HISTORY_PAYLOAD,
                    "adjust_policy": "none",
                    "point_type": "adjusted_close",
                    "replacement_mode": "full",
                }
            )
        assert exc.value.error_code == "history_dimension_mismatch"

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

    def test_cn_exchange_history_uses_directory_region_without_prefix_guess(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The upstream query code is built from region_code+symbol only."""
        captured: dict = {}

        def fake_fetch(req):
            captured["source_code"] = req.source_code
            return FetchData(
                provider="akshare",
                provider_symbol="sh600036",
                name="招商银行",
                currency="CNY",
                point_type="adjusted_close",
                expense_ratio_status="unavailable",
                source_name="ak.stock_zh_a_hist",
                source_quality="full",
                points=[HistoricalPoint(date="2024-01-08", value=30.0)],
            )

        monkeypatch.setattr(
            "fireman_market_provider.worker.executors.history.fetch_instrument", fake_fetch
        )
        result = execute_history_sync(
            {
                "asset_key": "cn:cn_exchange_stock:sh:600036",
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "region_code": "sh",
                "symbol": "600036",
                "adjust_policy": "none",
                "point_type": "adjusted_close",
                "replacement_mode": "full",
            }
        )
        assert captured["source_code"] == "sh600036"
        assert result["points"]

    def test_cn_exchange_history_rejects_missing_directory_region(self) -> None:
        """No region_code/exchange -> non-retryable asset_identity_incomplete;
        never a silent fallback to a guessed sh600036."""
        with pytest.raises(TaskFailure) as exc:
            execute_history_sync(
                {
                    "asset_key": "cn:cn_exchange_stock::600036",
                    "market": "CN",
                    "instrument_type": "cn_exchange_stock",
                    "region_code": "",
                    "exchange": "",
                    "symbol": "600036",
                    "adjust_policy": "none",
                    "point_type": "adjusted_close",
                    "replacement_mode": "full",
                }
            )
        assert exc.value.error_code == "asset_identity_incomplete"

    def test_cn_exchange_history_rejects_conflicting_directory_identity(self) -> None:
        with pytest.raises(TaskFailure) as exc:
            execute_history_sync(
                {
                    "asset_key": "cn:cn_exchange_stock:sh:600036",
                    "market": "CN",
                    "instrument_type": "cn_exchange_stock",
                    "region_code": "sh",
                    "exchange": "SZ",
                    "symbol": "600036",
                    "adjust_policy": "none",
                    "point_type": "adjusted_close",
                    "replacement_mode": "full",
                }
            )
        assert exc.value.error_code == "directory_identity_invalid"

    def test_cn_exchange_history_accepts_exchange_when_region_missing(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        def fake_fetch(req):
            assert req.source_code == "sz159915"
            return FetchData(
                provider="akshare",
                provider_symbol="sz159915",
                name="创业板ETF",
                currency="CNY",
                point_type="adjusted_close",
                expense_ratio_status="unavailable",
                source_name="ak.fund_etf_hist_em",
                source_quality="full",
                points=[HistoricalPoint(date="2024-01-08", value=2.0)],
            )

        monkeypatch.setattr(
            "fireman_market_provider.worker.executors.history.fetch_instrument", fake_fetch
        )
        result = execute_history_sync(
            {
                "asset_key": "cn:cn_exchange_fund:sz:159915",
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "region_code": "",
                "exchange": "SZ",
                "symbol": "159915",
                "adjust_policy": "none",
                "point_type": "adjusted_close",
                "replacement_mode": "full",
            }
        )
        assert result["points"]


class TestCNDirectorySync:
    def test_cn_stock_boards_provide_region_and_exchange(self) -> None:
        register_test_dispatch(
            "em_cn_sh_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["600036"], "名称": ["招商银行"]}),
        )
        register_test_dispatch(
            "em_cn_sz_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["000001"], "名称": ["平安银行"]}),
        )
        register_test_dispatch(
            "em_cn_bj_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["830799"], "名称": ["艾融软件"]}),
        )
        result = execute_directory_sync(_unit_payload("cn_exchange_stock", "cn_all"))
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # region/exchange come from the queried board, never from row fields.
        assert by_symbol["600036"]["region_code"] == "sh"
        assert by_symbol["600036"]["exchange"] == "SH"
        assert by_symbol["000001"]["region_code"] == "sz"
        assert by_symbol["000001"]["exchange"] == "SZ"
        assert by_symbol["830799"]["region_code"] == "bj"
        assert by_symbol["830799"]["exchange"] == "BJ"
        assert by_symbol["600036"]["source_name"] == "em.cn_sh_a_list"
        assert by_symbol["000001"]["source_name"] == "em.cn_sz_a_list"
        assert by_symbol["830799"]["source_name"] == "em.cn_bj_a_list"

    def test_cn_stock_non_six_digit_codes_are_skipped(self) -> None:
        register_test_dispatch(
            "em_cn_sh_a_list",
            lambda **kwargs: pd.DataFrame(
                {"代码": ["600036", "12345", "ABCDEF"], "名称": ["招商银行", "坏码", "坏码2"]}
            ),
        )
        register_test_dispatch(
            "em_cn_sz_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["000001"], "名称": ["平安银行"]}),
        )
        register_test_dispatch(
            "em_cn_bj_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["830799"], "名称": ["艾融软件"]}),
        )
        result = execute_directory_sync(_unit_payload("cn_exchange_stock", "cn_all"))
        assert {a["symbol"] for a in result["assets"]} == {"600036", "000001", "830799"}

    def test_cn_stock_empty_board_fails_whole_task(self) -> None:
        register_test_dispatch(
            "em_cn_sh_a_list",
            lambda **kwargs: pd.DataFrame({"代码": ["600036"], "名称": ["招商银行"]}),
        )
        register_test_dispatch("em_cn_sz_a_list", lambda **kwargs: pd.DataFrame())
        with pytest.raises(TaskFailure) as exc:
            execute_directory_sync(_unit_payload("cn_exchange_stock", "cn_all"))
        assert exc.value.error_code == "directory_data_incomplete"
        assert "sync_key=cn_exchange_stock" in exc.value.message

    def test_cn_fund_market_id_drives_region_and_unknown_rows_skipped(self) -> None:
        register_test_dispatch(
            "em_cn_etf_list",
            lambda **kwargs: pd.DataFrame(
                {
                    "代码": ["510300", "159915", "999999"],
                    "名称": ["沪深300ETF", "创业板ETF", "坏数据"],
                    "市场标识": [1, 0, None],
                }
            ),
        )
        register_test_dispatch(
            "em_cn_lof_list",
            lambda **kwargs: pd.DataFrame(
                {"代码": ["166009"], "名称": ["中欧价值LOF"], "市场标识": [0]}
            ),
        )
        result = execute_directory_sync(_unit_payload("cn_exchange_fund", "cn_all"))
        by_symbol = {a["symbol"]: a for a in result["assets"]}
        # The row without a market id is skipped, never guessed from prefix.
        assert set(by_symbol) == {"510300", "159915", "166009"}
        assert by_symbol["510300"]["region_code"] == "sh"
        assert by_symbol["510300"]["instrument_kind"] == "etf"
        assert by_symbol["159915"]["region_code"] == "sz"
        assert by_symbol["166009"]["region_code"] == "sz"
        assert by_symbol["166009"]["instrument_kind"] == "lof"


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
