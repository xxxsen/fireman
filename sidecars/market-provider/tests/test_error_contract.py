"""Contract tests for the provider error taxonomy used by the worker."""

from __future__ import annotations

from unittest.mock import patch

from fireman_market_provider.provider_errors import (
    ERROR_HTTP_STATUS,
    ProviderError,
    provider_error_from_exception,
)

from .fetch_compat import fetch


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


def test_fetch_all_sources_fail_classified_unavailable() -> None:
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=RuntimeError("all sources down"),
    ):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "sh510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            }
        )
    assert response.status_code == 503
    _assert_error_envelope(response.json(), "market_provider_unavailable")


def test_fetch_timeout_classified_timeout() -> None:
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=TimeoutError("upstream slow"),
    ):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "sh510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            }
        )
    assert response.status_code == 504
    _assert_error_envelope(response.json(), "market_provider_timeout")


def test_fetch_rejects_unknown_fields() -> None:
    response = fetch(
        {
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "sh510300",
            "end_date": "2026-06-09",
            "expense_ratio": 0.5,
        }
    )
    assert response.status_code == 422
    _assert_error_envelope(response.json(), "invalid_request")
