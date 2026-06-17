"""Contract tests for the unified structured error envelope (td/037 phase 1)."""

from __future__ import annotations

from unittest.mock import patch

import pandas as pd
import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.provider_errors import (
    ERROR_HTTP_STATUS,
    ProviderError,
    provider_error_from_exception,
)


def _client() -> TestClient:
    return TestClient(create_app())


def _empty_spot() -> pd.DataFrame:
    return pd.DataFrame({"代码": [], "名称": []})


def test_error_http_status_map_is_closed_enum() -> None:
    assert ERROR_HTTP_STATUS == {
        "invalid_request": 400,
        "instrument_not_found": 404,
        "instrument_type_mismatch": 400,
        "source_data_conflict": 422,
        "market_provider_timeout": 504,
        "market_provider_unavailable": 503,
    }


def test_provider_error_from_exception_mapping() -> None:
    assert provider_error_from_exception(TimeoutError("x")).error_code == "market_provider_timeout"
    assert provider_error_from_exception(RuntimeError("x")).error_code == "market_provider_unavailable"
    assert provider_error_from_exception(ValueError("instrument_not_found")).error_code == "instrument_not_found"
    assert provider_error_from_exception(ValueError("instrument_type_mismatch")).error_code == "instrument_type_mismatch"
    # Unknown ValueError messages collapse to invalid_request, never leak as-is.
    assert provider_error_from_exception(ValueError("some weird text")).error_code == "invalid_request"
    # Unknown codes are coerced to a safe default.
    assert ProviderError("bogus", "m").error_code == "market_provider_unavailable"


def _assert_error_envelope(body: dict, error_code: str) -> None:
    assert set(body.keys()) == {"code", "error_code", "message", "data"}
    assert body["code"] == 1
    assert body["error_code"] == error_code
    assert isinstance(body["message"], str)
    assert body["data"] is None


def test_resolve_not_found_returns_structured_envelope() -> None:
    reset_name_caches()
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_empty_spot()
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()), patch(
        "fireman_market_provider.adapters.resolve.lookup_cn_mutual_fund_name_readonly", return_value=None
    ), patch(
        "fireman_market_provider.adapters.resolve.resolve_cn_mutual_fund_name", return_value=None
    ):
        response = _client().post(
            "/v1/instruments/resolve",
            json={"market": "CN", "instrument_type": "cn_exchange_fund", "code": "999999"},
        )
    assert response.status_code == 404
    _assert_error_envelope(response.json(), "instrument_not_found")


def test_resolve_timeout_returns_structured_envelope() -> None:
    reset_name_caches()
    with patch("akshare.fund_etf_spot_em", side_effect=TimeoutError("slow")), patch(
        "akshare.fund_lof_spot_em", side_effect=TimeoutError("slow")
    ), patch("akshare.stock_zh_a_spot_em", side_effect=TimeoutError("slow")):
        response = _client().post(
            "/v1/instruments/resolve",
            json={"market": "CN", "instrument_type": "cn_exchange_fund", "code": "510300"},
        )
    assert response.status_code == 504
    _assert_error_envelope(response.json(), "market_provider_timeout")


def test_fetch_all_sources_fail_returns_structured_envelope() -> None:
    """fetch failures must use the HTTP error channel, never 200+code=1+empty data."""
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=RuntimeError("all sources down"),
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
    assert response.status_code == 503
    body = response.json()
    _assert_error_envelope(body, "market_provider_unavailable")
    # Must not carry misleading default data (no empty equity/CNY payload).
    assert body["data"] is None


def test_fetch_timeout_returns_structured_envelope() -> None:
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=TimeoutError("upstream slow"),
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
    assert response.status_code == 504
    _assert_error_envelope(response.json(), "market_provider_timeout")


@pytest.mark.parametrize("target", ["bogus_target"])
def test_metadata_refresh_unsupported_target(target: str) -> None:
    response = _client().post("/v1/metadata/refresh", json={"target": target})
    # Body-validation failures join the unified structured error contract.
    assert response.status_code == 422
    _assert_error_envelope(response.json(), "invalid_request")


def test_request_validation_errors_share_one_envelope() -> None:
    """resolve/fetch/metadata body-validation failures all yield the same envelope."""
    client = _client()
    requests = [
        ("/v1/instruments/resolve", {"market": "CN"}),  # missing code/instrument_type
        ("/v1/instruments/fetch", {"market": "CN", "bogus": 1}),  # unknown + missing fields
        ("/v1/metadata/refresh", {"target": "nope"}),  # value outside Literal enum
    ]
    for path, body in requests:
        response = client.post(path, json=body)
        assert response.status_code == 422, path
        envelope = response.json()
        _assert_error_envelope(envelope, "invalid_request")
        assert set(envelope.keys()) == {"code", "error_code", "message", "data"}, path
