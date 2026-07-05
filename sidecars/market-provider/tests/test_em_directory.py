"""Eastmoney clist directory fetcher tests (recorded testdata, no network)."""

from __future__ import annotations

from typing import Any

import pytest

from fireman_market_provider.adapters import em_directory

from .dataload import load_json_gz


class _FakeResponse:
    def __init__(self, payload: dict[str, Any]) -> None:
        self._payload = payload

    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, Any]:
        return self._payload


_HK_EQUITY_PAGES = {
    1: load_json_gz("em_hk_equity_list.page1.json.gz"),
    2: load_json_gz("em_hk_equity_list.page2.json.gz"),
    3: load_json_gz("em_hk_equity_list.page3.json.gz"),
}


@pytest.fixture(autouse=True)
def _fast_pagination(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setattr(em_directory, "_PAGE_SLEEP_SECONDS", 0.0)


def test_fetch_board_paginates_until_total(monkeypatch: pytest.MonkeyPatch) -> None:
    calls: list[tuple[str, int]] = []

    def fake_get(url, params=None, headers=None, timeout=None):
        calls.append((url, params["pn"]))
        return _FakeResponse(_HK_EQUITY_PAGES[params["pn"]])

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    rows = em_directory._fetch_board("m:116 t:1")
    assert len(rows) == 250
    assert [pn for _, pn in calls] == [1, 2, 3]
    # Only the first host is used when it works.
    assert all(url == em_directory._CLIST_HOSTS[0] for url, _ in calls)


def test_fetch_board_falls_back_to_next_host(monkeypatch: pytest.MonkeyPatch) -> None:
    payload = load_json_gz("em_hk_fund_list.page1.json.gz")

    def fake_get(url, params=None, headers=None, timeout=None):
        if url == em_directory._CLIST_HOSTS[0]:
            raise ConnectionError("host unreachable")
        return _FakeResponse(payload)

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    rows = em_directory._fetch_board("m:116 t:1")
    assert len(rows) == 2


def test_fetch_board_truncated_response_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    page1 = load_json_gz("em_hk_equity_list.page1.json.gz")
    truncated = load_json_gz("em_hk_equity_list.truncated_page2.json.gz")

    def fake_get(url, params=None, headers=None, timeout=None):
        if params["pn"] == 1:
            # total=300 forces a second page which then comes back empty.
            return _FakeResponse(
                {"data": {"total": 300, "diff": page1["data"]["diff"]}}
            )
        return _FakeResponse(truncated)

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    with pytest.raises(RuntimeError, match="all eastmoney clist hosts failed"):
        em_directory._fetch_board("m:116 t:1")


def test_hk_and_us_frames_have_akshare_columns(monkeypatch: pytest.MonkeyPatch) -> None:
    hk_fund = load_json_gz("em_hk_fund_list.page1.json.gz")
    us_etf = load_json_gz("em_us_etf_list.page1.json.gz")

    def fake_get(url, params=None, headers=None, timeout=None):
        fs = params["fs"]
        if "m:116" in fs:
            return _FakeResponse(hk_fund)
        return _FakeResponse(us_etf)

    monkeypatch.setattr(em_directory.requests, "get", fake_get)

    hk = em_directory.em_hk_fund_list()
    assert list(hk.columns) == ["代码", "名称"]
    assert hk.iloc[0]["代码"] == "02800"

    us = em_directory.em_us_etf_list()
    # US codes keep the eastmoney market prefix, like ak.stock_us_spot_em.
    assert us.iloc[0]["代码"] == "107.SPY"


def test_us_equity_frame_uses_market_prefixed_codes(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    us_equity = load_json_gz("em_us_equity_list.page1.json.gz")

    def fake_get(url, params=None, headers=None, timeout=None):
        return _FakeResponse(us_equity)

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    us = em_directory.em_us_equity_list()
    assert list(us["代码"]) == ["105.AAPL", "106.BRK.B"]


def test_dispatcher_routes_em_operations(monkeypatch: pytest.MonkeyPatch) -> None:
    from fireman_market_provider.timeout_util import UpstreamCall, dispatch_upstream_call

    payload = load_json_gz("em_hk_fund_list.page1.json.gz")

    def fake_get(url, params=None, headers=None, timeout=None):
        return _FakeResponse(payload)

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    df = dispatch_upstream_call(UpstreamCall("em_hk_equity_list"))
    assert not df.empty

    with pytest.raises(ValueError, match="unsupported upstream operation"):
        dispatch_upstream_call(UpstreamCall("em_nonexistent_list"))


def test_network_guard_blocks_unmocked_requests() -> None:
    """Unmocked requests.get in a non-live test must fail with guidance."""
    import requests

    with pytest.raises(RuntimeError, match="tests/testdata"):
        requests.get("https://push2.eastmoney.com/api/qt/clist/get")
