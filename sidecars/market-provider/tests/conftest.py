"""Pytest configuration for market-provider tests."""

import pytest

from fireman_market_provider.timeout_util import (
    UpstreamCall,
    clear_test_dispatch,
    dispatch_upstream_call,
)


@pytest.fixture(autouse=True)
def _clear_dispatch_overrides() -> None:
    """Prevent register_test_dispatch overrides from leaking across tests."""
    yield
    clear_test_dispatch()


@pytest.fixture(autouse=True)
def _default_offline_env(
    request: pytest.FixtureRequest, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Non-live tests never start the worker loop or the startup name warm."""
    if request.node.get_closest_marker("live"):
        return
    monkeypatch.setenv("FIREMAN_WORKER_ENABLED", "false")
    monkeypatch.setenv("FIREMAN_DISABLE_STARTUP_WARM", "1")


@pytest.fixture(autouse=True)
def _block_real_network(
    request: pytest.FixtureRequest, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Fail fast on any real upstream HTTP call in non-live tests.

    Tests must mock upstreams explicitly (register_test_dispatch,
    unittest.mock.patch, or module-level requests monkeypatching) or load
    recorded payloads from tests/testdata. Only ``-m live`` tests may touch
    the network. The worker's Go internal client (urllib against a local
    fake server) is intentionally not blocked.
    """
    if request.node.get_closest_marker("live"):
        return

    def _blocked(*_args: object, **_kwargs: object):
        raise RuntimeError(
            "network access is disabled in non-live tests; mock the upstream "
            "call or use a tests/testdata fixture (see tests/dataload.py). "
            "Real-network tests must be marked @pytest.mark.live."
        )

    for attr in ("get", "post", "put", "delete", "head", "options", "request"):
        monkeypatch.setattr(f"requests.api.{attr}", _blocked, raising=False)
        monkeypatch.setattr(f"requests.{attr}", _blocked, raising=False)
    monkeypatch.setattr("requests.sessions.Session.request", _blocked)
    # TickFlow SDK: block at the SDK HTTP layer so client construction and
    # get_tickflow_client() stay testable while real API calls fail fast.
    monkeypatch.setattr(
        "tickflow._base_client.SyncAPIClient._request", _blocked, raising=False
    )
    monkeypatch.setattr(
        "tickflow._base_client.AsyncAPIClient._request", _blocked, raising=False
    )


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
        "fireman_market_provider.worker.executors.directory.call_with_timeout",
        "fireman_market_provider.worker.executors.history.call_with_timeout",
        "fireman_market_provider.worker.executors.fx.call_with_timeout",
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
