"""Tests for instrument display name resolution."""

import json
import threading
from datetime import UTC, datetime, timedelta
from unittest.mock import patch

import pandas as pd
from fireman_market_provider.adapters.names import (
    _load_mutual_fund_name_map,
    lookup_cn_exchange_fund_name,
    lookup_cn_mutual_fund_name,
    lookup_cn_mutual_fund_name_readonly,
    name_from_dataframe,
    refresh_cn_mutual_fund_names,
    reset_name_caches,
    resolve_cn_exchange_fund_name,
)
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch


def setup_function() -> None:
    reset_name_caches()
    clear_test_dispatch()


def teardown_function() -> None:
    clear_test_dispatch()


def test_name_from_dataframe_prefers_fund_short_name() -> None:
    df = pd.DataFrame({"基金简称": ["沪深300ETF"], "日期": ["2024-01-02"]})
    assert name_from_dataframe(df, "510300") == "沪深300ETF"


def test_resolve_cn_exchange_fund_name_uses_spot_lookup() -> None:
    hist = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.0]})
    spot = pd.DataFrame({"代码": ["510300"], "名称": ["沪深300ETF华泰柏瑞"]})
    with patch("akshare.fund_etf_spot_em", return_value=spot):
        assert resolve_cn_exchange_fund_name("510300", hist) == "沪深300ETF华泰柏瑞"
        assert lookup_cn_exchange_fund_name("510300") == "沪深300ETF华泰柏瑞"


def test_lookup_cn_exchange_fund_name_prefers_lof_for_27_prefix() -> None:
    empty = pd.DataFrame({"代码": [], "名称": []})
    lof = pd.DataFrame({"代码": ["270042"], "名称": ["测试LOF名称"]})
    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.fund_lof_spot_em", return_value=lof
    ):
        assert lookup_cn_exchange_fund_name("270042") == "测试LOF名称"


def test_lookup_cn_exchange_fund_name_uses_xq_when_spot_missing() -> None:
    empty = pd.DataFrame({"代码": [], "名称": []})
    xq = pd.DataFrame(
        {"item": ["基金代码", "基金名称"], "value": ["270042", "广发纳指100ETF联接（QDII）人民币A"]}
    )
    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.fund_individual_basic_info_xq", return_value=xq):
        assert lookup_cn_exchange_fund_name("270042") == "广发纳指100ETF联接（QDII）人民币A"


def test_lookup_cross_listed_etf_name_uses_index_not_xq() -> None:
    empty = pd.DataFrame({"代码": [], "名称": []})
    index = pd.DataFrame({"index_code": ["000510"], "display_name": ["中证A500"], "publish_date": ["2005-01-04"]})
    xq = pd.DataFrame({"item": ["基金名称"], "value": ["诺安永鑫一年定开债券"]})

    def _xq_should_not_run(**_kwargs: str) -> pd.DataFrame:
        raise AssertionError("XQ lookup must not run for cross-listed bare codes")

    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.index_stock_info", return_value=index
    ), patch("akshare.fund_individual_basic_info_xq", side_effect=_xq_should_not_run):
        assert lookup_cn_exchange_fund_name("000510") == "中证A500"


def test_lookup_cn_stock_name_uses_individual_when_spot_missing() -> None:
    empty = pd.DataFrame({"代码": [], "名称": []})
    info = pd.DataFrame({"item": ["股票简称"], "value": ["新金路"]})
    with patch("akshare.stock_zh_a_spot_em", return_value=empty), patch(
        "akshare.stock_individual_info_em", return_value=info
    ):
        from fireman_market_provider.adapters.names import lookup_cn_stock_name

        assert lookup_cn_stock_name("000510") == "新金路"


def _fresh_refreshed_at() -> str:
    return datetime.now(UTC).replace(microsecond=0).isoformat()


def test_mutual_fund_name_map_uses_ttl_not_permanent_cache(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_TTL", "3600")
    mutual = pd.DataFrame({"基金代码": ["007194"], "基金简称": ["长城短债A"]})
    calls = {"count": 0}

    def fetch() -> pd.DataFrame:
        calls["count"] += 1
        return mutual

    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name("007194") == "长城短债A"
    assert lookup_cn_mutual_fund_name("007194") == "长城短债A"
    assert calls["count"] == 1

    stale_at = (datetime.now(UTC) - timedelta(hours=2)).replace(microsecond=0).isoformat()
    cache_path.write_text(
        json.dumps({"version": 1, "refreshed_at": stale_at, "names": {"007194": "过期名称"}}),
        encoding="utf-8",
    )
    reset_name_caches()
    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name("007194") == "长城短债A"
    assert calls["count"] == 2


def test_mutual_fund_name_lookup_uses_recorded_fixture(tmp_path, monkeypatch) -> None:
    """Recorded ak.fund_name_em payload drives lookups without network."""
    from .dataload import load_dataframe_gz

    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    sample = load_dataframe_gz("ak_fund_name_em.sample.json.gz")

    register_test_dispatch("fund_name_em", lambda: sample)
    assert lookup_cn_mutual_fund_name("000001") == "华夏成长混合"
    assert lookup_cn_mutual_fund_name("110011") == "易方达中小盘混合"


def test_mutual_fund_name_map_loads_from_disk_without_upstream(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    cache_path.write_text(
        json.dumps(
            {
                "version": 1,
                "refreshed_at": _fresh_refreshed_at(),
                "names": {"007194": "长城短债A"},
            }
        ),
        encoding="utf-8",
    )

    def fetch() -> pd.DataFrame:
        raise AssertionError("upstream should not be called when disk cache exists")

    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name("007194") == "长城短债A"


def test_mutual_fund_name_readonly_miss_without_cache(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    reset_name_caches()

    def fetch() -> pd.DataFrame:
        raise AssertionError("readonly lookup must not call upstream")

    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name_readonly("007194") is None


def test_mutual_fund_name_readonly_from_disk(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    reset_name_caches()
    cache_path.write_text(
        json.dumps(
            {
                "version": 1,
                "refreshed_at": _fresh_refreshed_at(),
                "names": {"007194": "长城短债A"},
            }
        ),
        encoding="utf-8",
    )

    def fetch() -> pd.DataFrame:
        raise AssertionError("readonly lookup must not call upstream")

    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name_readonly("007194") == "长城短债A"


def test_expired_disk_cache_triggers_upstream(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    stale_at = (datetime.now(UTC) - timedelta(days=2)).replace(microsecond=0).isoformat()
    cache_path.write_text(
        json.dumps({"version": 1, "refreshed_at": stale_at, "names": {"007194": "旧名称"}}),
        encoding="utf-8",
    )
    mutual = pd.DataFrame({"基金代码": ["007194"], "基金简称": ["长城短债A"]})
    calls = {"count": 0}

    def fetch() -> pd.DataFrame:
        calls["count"] += 1
        return mutual

    register_test_dispatch("fund_name_em", fetch)
    assert lookup_cn_mutual_fund_name("007194") == "长城短债A"
    assert calls["count"] == 1


def test_refresh_cn_mutual_fund_names_overwrites_cache(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    cache_path.write_text(
        json.dumps({"version": 1, "refreshed_at": "old", "names": {"007194": "旧名称"}}),
        encoding="utf-8",
    )
    mutual = pd.DataFrame({"基金代码": ["007194"], "基金简称": ["长城短债A"]})
    calls = {"count": 0}

    def fetch() -> pd.DataFrame:
        calls["count"] += 1
        return mutual

    register_test_dispatch("fund_name_em", fetch)
    names = refresh_cn_mutual_fund_names()
    assert names["007194"] == "长城短债A"
    assert calls["count"] == 1
    payload = json.loads(cache_path.read_text(encoding="utf-8"))
    assert payload["names"]["007194"] == "长城短债A"
    assert payload["refreshed_at"] != "old"

    def fetch_again() -> pd.DataFrame:
        raise AssertionError("in-memory cache should survive without upstream")

    register_test_dispatch("fund_name_em", fetch_again)
    assert _load_mutual_fund_name_map()["007194"] == "长城短债A"


def test_mutual_fund_refresh_singleflight(tmp_path, monkeypatch) -> None:
    cache_path = tmp_path / "mutual_fund_names.json"
    monkeypatch.setenv("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", str(cache_path))
    stale_at = (datetime.now(UTC) - timedelta(days=2)).replace(microsecond=0).isoformat()
    cache_path.write_text(
        json.dumps({"version": 1, "refreshed_at": stale_at, "names": {"007194": "旧名称"}}),
        encoding="utf-8",
    )
    mutual = pd.DataFrame({"基金代码": ["007194"], "基金简称": ["长城短债A"]})
    calls = {"count": 0}
    started = threading.Event()
    release = threading.Event()

    def fetch() -> pd.DataFrame:
        calls["count"] += 1
        started.set()
        release.wait(timeout=5)
        return mutual

    register_test_dispatch("fund_name_em", fetch)
    results: list[str | None] = []
    errors: list[Exception] = []

    def worker() -> None:
        try:
            results.append(lookup_cn_mutual_fund_name("007194"))
        except Exception as exc:  # noqa: BLE001
            errors.append(exc)

    t1 = threading.Thread(target=worker)
    t2 = threading.Thread(target=worker)
    t1.start()
    t2.start()
    assert started.wait(timeout=5)
    release.set()
    t1.join(timeout=5)
    t2.join(timeout=5)
    assert not errors
    assert results == ["长城短债A", "长城短债A"]
    assert calls["count"] == 1
