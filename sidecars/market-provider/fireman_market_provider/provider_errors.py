"""Structured provider error taxonomy shared by all sidecar endpoints.

Every failure path maps to a single ``error_code`` from a closed enum plus a
deterministic HTTP status. The Go client classifies failures by ``error_code``
only (never by free-text message), so this module is the single source of truth
for the error contract.
"""

from __future__ import annotations

# error_code -> HTTP status. Keep in sync with internal/marketdata error mapping.
ERROR_HTTP_STATUS: dict[str, int] = {
    "invalid_request": 400,
    "instrument_not_found": 404,
    "instrument_type_mismatch": 400,
    "source_data_conflict": 422,
    "market_provider_timeout": 504,
    "market_provider_unavailable": 503,
}

# ValueError messages raised by adapters that already are valid error codes.
_VALUE_ERROR_CODES = frozenset(
    {
        "invalid_request",
        "instrument_not_found",
        "instrument_type_mismatch",
        "source_data_conflict",
    }
)


class ProviderError(Exception):
    """Carries a structured ``error_code`` and a human-readable message."""

    def __init__(self, error_code: str, message: str) -> None:
        if error_code not in ERROR_HTTP_STATUS:
            error_code = "market_provider_unavailable"
        self.error_code = error_code
        self.message = message
        super().__init__(f"{error_code}: {message}")

    @property
    def http_status(self) -> int:
        return ERROR_HTTP_STATUS[self.error_code]


def provider_error_from_exception(exc: Exception) -> ProviderError:
    """Translate adapter exceptions into a structured :class:`ProviderError`."""
    if isinstance(exc, ProviderError):
        return exc
    if isinstance(exc, TimeoutError):
        return ProviderError("market_provider_timeout", "upstream timeout")
    if isinstance(exc, ValueError):
        message = str(exc).strip()
        code = message if message in _VALUE_ERROR_CODES else "invalid_request"
        return ProviderError(code, message or "invalid request")
    if isinstance(exc, RuntimeError):
        return ProviderError("market_provider_unavailable", str(exc) or "upstream unavailable")
    return ProviderError("market_provider_unavailable", str(exc) or "upstream unavailable")
