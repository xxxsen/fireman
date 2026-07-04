"""Test helper: drive the fetch chain with the legacy response contract.

The synchronous /v1/instruments/fetch endpoint was removed by td/078 (the
sidecar is a pure task worker now), but the underlying fetch chain
(adapters.registry.fetch_instrument) still powers asset_history_sync's
unpinned path. This helper preserves the old envelope shape so the fetch
chain regression tests keep their assertions while exercising the current
code path directly.
"""

from __future__ import annotations

from typing import Any

from pydantic import ValidationError

from fireman_market_provider.adapters.registry import fetch_instrument
from fireman_market_provider.provider_errors import (
    ProviderError,
    provider_error_from_exception,
)
from fireman_market_provider.schemas import FetchRequest


class FetchResult:
    def __init__(self, status_code: int, body: dict[str, Any]) -> None:
        self.status_code = status_code
        self._body = body

    def json(self) -> dict[str, Any]:
        return self._body


def fetch(payload: dict[str, Any]) -> FetchResult:
    try:
        req = FetchRequest(**payload)
    except ValidationError as exc:
        return FetchResult(
            422,
            {"code": 1, "error_code": "invalid_request", "message": str(exc), "data": None},
        )
    try:
        data = fetch_instrument(req)
    except ProviderError as exc:
        return FetchResult(
            exc.http_status,
            {"code": 1, "error_code": exc.error_code, "message": exc.message, "data": None},
        )
    except Exception as exc:  # noqa: BLE001
        provider_exc = provider_error_from_exception(exc)
        return FetchResult(
            provider_exc.http_status,
            {
                "code": 1,
                "error_code": provider_exc.error_code,
                "message": provider_exc.message,
                "data": None,
            },
        )
    return FetchResult(200, {"code": 0, "message": "success", "data": data.model_dump()})
