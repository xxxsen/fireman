"""Eastmoney clist directory fetcher tests (mocked HTTP)."""

from __future__ import annotations

from typing import Any

import pytest

from fireman_market_provider.adapters import em_directory


class _FakeResponse:
    def __init__(self, payload: dict[str, Any]) -> None:
        self._payload = payload

    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, Any]:
        return self._payload


def _rows(start: int, count: int, market: int = 116) -> list[dict[str, Any]]:
    return [
        {"f12": f"{i:05d}", "f13": market, "f14": f"资产{i}"}
        for i in range(start, start + count)
    ]


@pytest.fixture(autouse=True)
def _fast_pagination(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setattr(em_directory, "_PAGE_SLEEP_SECONDS", 0.0)


def test_fetch_board_paginates_until_total(monkeypatch: pytest.MonkeyPatch) -> None:
    pages = {
        1: {"total": 250, "diff": _rows(0, 100)},
        2: {"total": 250, "diff": _rows(100, 100)},
        3: {"total": 250, "diff": _rows(200, 50)},
    }
    calls: list[tuple[str, int]] = []

    def fake_get(url, params=None, headers=None, timeout=None):
        calls.append((url, params["pn"]))
        return _FakeResponse({"data": pages[params["pn"]]})

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    rows = em_directory._fetch_board("m:116 t:1")
    assert len(rows) == 250
    assert [pn for _, pn in calls] == [1, 2, 3]
    # Only the first host is used when it works.
    assert all(url == em_directory._CLIST_HOSTS[0] for url, _ in calls)


def test_fetch_board_falls_back_to_next_host(monkeypatch: pytest.MonkeyPatch) -> None:
    def fake_get(url, params=None, headers=None, timeout=None):
        if url == em_directory._CLIST_HOSTS[0]:
            raise ConnectionError("host unreachable")
        return _FakeResponse({"data": {"total": 2, "diff": _rows(0, 2)}})

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    rows = em_directory._fetch_board("m:116 t:1")
    assert len(rows) == 2


def test_fetch_board_truncated_response_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    def fake_get(url, params=None, headers=None, timeout=None):
        if params["pn"] == 1:
            return _FakeResponse({"data": {"total": 300, "diff": _rows(0, 100)}})
        return _FakeResponse({"data": {"total": 300, "diff": []}})

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    with pytest.raises(RuntimeError, match="all eastmoney clist hosts failed"):
        em_directory._fetch_board("m:116 t:1")


def test_hk_and_us_frames_have_akshare_columns(monkeypatch: pytest.MonkeyPatch) -> None:
    def fake_get(url, params=None, headers=None, timeout=None):
        fs = params["fs"]
        if "m:116" in fs:
            diff = [{"f12": "02800", "f13": 116, "f14": "盈富基金"}]
        else:
            diff = [{"f12": "SPY", "f13": 107, "f14": "标普500ETF"}]
        return _FakeResponse({"data": {"total": 1, "diff": diff}})

    monkeypatch.setattr(em_directory.requests, "get", fake_get)

    hk = em_directory.em_hk_fund_list()
    assert list(hk.columns) == ["代码", "名称"]
    assert hk.iloc[0]["代码"] == "02800"

    us = em_directory.em_us_etf_list()
    # US codes keep the eastmoney market prefix, like ak.stock_us_spot_em.
    assert us.iloc[0]["代码"] == "107.SPY"


def test_dispatcher_routes_em_operations(monkeypatch: pytest.MonkeyPatch) -> None:
    from fireman_market_provider.timeout_util import UpstreamCall, dispatch_upstream_call

    def fake_get(url, params=None, headers=None, timeout=None):
        return _FakeResponse({"data": {"total": 1, "diff": _rows(0, 1)}})

    monkeypatch.setattr(em_directory.requests, "get", fake_get)
    df = dispatch_upstream_call(UpstreamCall("em_hk_equity_list"))
    assert not df.empty

    with pytest.raises(ValueError, match="unsupported upstream operation"):
        dispatch_upstream_call(UpstreamCall("em_nonexistent_list"))
