"""Tests for the structural Eastmoney open-fund history parser."""

from __future__ import annotations

from dataclasses import dataclass

import pytest

from fireman_market_provider.adapters import em_fund_history


@dataclass
class _Response:
    status_code: int
    body: str

    @property
    def content(self) -> bytes:
        return self.body.encode()

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            raise RuntimeError(f"HTTP {self.status_code}")


def _history_script() -> str:
    return (
        'var Data_netWorthTrend = [{"x": 1609459200000, "y": 1.1},'
        '{"x": 1609545600000, "y": 1.2}];'
        "var Data_ACWorthTrend = [[1609459200000, 1.3], [1609545600000, 1.4]];"
    )


def test_legacy_fund_code_follows_explicit_canonical_redirect(monkeypatch) -> None:
    requested_urls: list[str] = []

    def fake_get(url: str, **kwargs: object) -> _Response:
        del kwargs
        requested_urls.append(url)
        if url.endswith("/pingzhongdata/000157.js"):
            return _Response(301, "")
        if url.endswith("/000157.html"):
            return _Response(
                200,
                '<script>location.href = "http://fund.eastmoney.com/100055.html";</script>',
            )
        if url.endswith("/pingzhongdata/100055.js"):
            return _Response(200, _history_script())
        raise AssertionError(f"unexpected URL {url}")

    monkeypatch.setattr(em_fund_history.requests, "get", fake_get)

    frame = em_fund_history.em_fund_open_history("000157", "累计净值走势")

    assert list(frame.columns) == ["净值日期", "累计净值"]
    assert frame["累计净值"].tolist() == [1.3, 1.4]
    assert requested_urls == [
        "https://fund.eastmoney.com/pingzhongdata/000157.js",
        "https://fund.eastmoney.com/000157.html",
        "https://fund.eastmoney.com/pingzhongdata/100055.js",
    ]


def test_unit_nav_is_decoded_without_executing_javascript(monkeypatch) -> None:
    monkeypatch.setattr(
        em_fund_history.requests,
        "get",
        lambda *args, **kwargs: _Response(200, _history_script()),
    )

    frame = em_fund_history.em_fund_open_history("100055", "单位净值走势")

    assert list(frame.columns) == ["净值日期", "单位净值"]
    assert frame["单位净值"].tolist() == [1.1, 1.2]


def test_directory_canonical_code_must_match_explicit_page_redirect(
    monkeypatch,
) -> None:
    def fake_get(url: str, **kwargs: object) -> _Response:
        del kwargs
        if url.endswith("/pingzhongdata/000157.js"):
            return _Response(301, "")
        if url.endswith("/000157.html"):
            return _Response(
                200,
                '<script>location.href = "http://fund.eastmoney.com/100055.html";</script>',
            )
        raise AssertionError(f"unexpected URL {url}")

    monkeypatch.setattr(em_fund_history.requests, "get", fake_get)

    with pytest.raises(ValueError, match="not directory canonical code 100099"):
        em_fund_history.em_fund_open_history(
            "000157", "累计净值走势", expected_canonical_symbol="100099"
        )
