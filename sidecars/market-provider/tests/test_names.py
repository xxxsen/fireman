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
