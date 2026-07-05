"""HTTP contract tests for the worker sidecar app (healthz only).

Starlette's ``TestClient.__enter__`` can hang in this environment, so the
contract tests drive the FastAPI lifespan explicitly with ``asgi-lifespan``
and issue in-process requests through ``httpx.ASGITransport`` instead.
"""

import asyncio

import httpx
from asgi_lifespan import LifespanManager

from fireman_market_provider import create_app


async def _request_app(method: str, path: str, **kwargs) -> httpx.Response:
    """Run one in-process request against the app with its lifespan active."""
    async with LifespanManager(create_app()) as manager:
        transport = httpx.ASGITransport(app=manager.app)
        async with httpx.AsyncClient(
            transport=transport, base_url="http://sidecar.test"
        ) as client:
            return await client.request(method, path, **kwargs)


def test_healthz_returns_ok(monkeypatch) -> None:
    monkeypatch.setenv("FIREMAN_WORKER_ENABLED", "false")
    monkeypatch.setenv("FIREMAN_DISABLE_STARTUP_WARM", "1")
    response = asyncio.run(_request_app("GET", "/healthz"))
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_legacy_fetch_endpoints_removed(monkeypatch) -> None:
    """The sidecar is a pure worker; the sync HTTP surface is gone."""
    monkeypatch.setenv("FIREMAN_WORKER_ENABLED", "false")
    monkeypatch.setenv("FIREMAN_DISABLE_STARTUP_WARM", "1")

    async def _all_legacy_404() -> None:
        async with LifespanManager(create_app()) as manager:
            transport = httpx.ASGITransport(app=manager.app)
            async with httpx.AsyncClient(
                transport=transport, base_url="http://sidecar.test"
            ) as client:
                for path in (
                    "/v1/instruments/resolve",
                    "/v1/instruments/fetch",
                    "/v1/metadata/refresh",
                ):
                    response = await client.post(path, json={})
                    assert response.status_code == 404, path

    asyncio.run(_all_legacy_404())
