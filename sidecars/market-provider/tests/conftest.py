"""Pytest configuration for market-provider tests."""

import pytest

from fireman_market_provider.timeout_util import UpstreamCall, dispatch_upstream_call


@pytest.fixture(autouse=True)
def _inline_upstream_calls_for_mocked_tests(request: pytest.FixtureRequest, monkeypatch: pytest.MonkeyPatch) -> None:
    """Run upstream calls inline so unittest.mock patches on akshare apply."""
    if request.node.get_closest_marker("subprocess"):
        return

    def _inline(call: UpstreamCall, timeout_seconds: int = 30):
        del timeout_seconds
        return dispatch_upstream_call(call)

    targets = [
        "fireman_market_provider.timeout_util.call_with_timeout",
        "fireman_market_provider.adapters.fallback.call_with_timeout",
        "fireman_market_provider.adapters.registry.call_with_timeout",
        "fireman_market_provider.adapters.names.call_with_timeout",
        "fireman_market_provider.adapters.cn_code.call_with_timeout",
        "fireman_market_provider.adapters.resolve.call_with_timeout",
    ]
    for target in targets:
        monkeypatch.setattr(target, _inline)


@pytest.fixture
def inline_upstream_calls(monkeypatch: pytest.MonkeyPatch) -> None:
    """Explicit alias for tests that declare the inline fixture."""
    def _inline(call: UpstreamCall, timeout_seconds: int = 30):
        del timeout_seconds
        return dispatch_upstream_call(call)

    monkeypatch.setattr(
        "fireman_market_provider.adapters.cn_code.call_with_timeout",
        _inline,
    )
