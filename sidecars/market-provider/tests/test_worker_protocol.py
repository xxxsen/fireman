from __future__ import annotations

import time

from fireman_market_provider.worker.config import WorkerConfig
from fireman_market_provider.worker.goclient import GoAPIError, WorkerTask
from fireman_market_provider.worker.runner import WorkerRunner, _ActiveAttempt


def task(task_id: str = "task_1") -> WorkerTask:
    return WorkerTask(
        id=task_id,
        version_no=1,
        type="asset_history_sync",
        status="running",
        payload_json="{}",
    )


class FakeClient:
    def __init__(self) -> None:
        self.uploads = 0
        self.reports: list[str] = []
        self.releases: list[str] = []
        self.heartbeats = 0

    def heartbeat(self, *args, **kwargs):  # noqa: ANN002, ANN003
        self.heartbeats += 1
        return task(str(args[0]))

    def upload_result(self, *args, **kwargs):  # noqa: ANN002, ANN003
        self.uploads += 1
        return "resource:abc"

    def report(self, *args, **kwargs):  # noqa: ANN002, ANN003
        self.reports.append(str(args[3]))
        current = task(str(args[0]))
        return WorkerTask(**{**current.__dict__, "status": "pre_complete"})

    def release(self, task_id, *_args):
        self.releases.append(task_id)
        return task(task_id)


def runner(client: FakeClient, heartbeat: float = 10.0) -> WorkerRunner:
    value = WorkerRunner(WorkerConfig("http://go", heartbeat_interval_seconds=heartbeat))
    value._client = client  # type: ignore[attr-defined]
    return value


def test_success_uploads_task_bound_resource_and_reports_result(monkeypatch):
    client = FakeClient()
    value = runner(client)
    monkeypatch.setattr(
        "fireman_market_provider.worker.runner.execute_task",
        lambda _task_type, _payload: {"type": "asset_history_sync"},
    )

    value._run_task(task(), "token-0123456789abcdef")

    assert client.uploads == 1
    assert client.reports == ["success"]


def test_result_transport_failure_keeps_heartbeat_and_retries(monkeypatch):
    client = FakeClient()
    attempts = 0

    def flaky_report(*args, **kwargs):  # noqa: ANN002, ANN003
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            time.sleep(0.03)
            raise GoAPIError("temporary transport failure")
        return FakeClient.report(client, *args, **kwargs)

    client.report = flaky_report  # type: ignore[method-assign]
    value = runner(client, heartbeat=0.005)
    monkeypatch.setattr(value._stop, "wait", lambda _delay: False)
    monkeypatch.setattr(
        "fireman_market_provider.worker.runner.execute_task",
        lambda _task_type, _payload: {"type": "asset_history_sync"},
    )

    value._run_task(task(), "token-0123456789abcdef")

    assert attempts == 2
    assert client.heartbeats > 0
    assert client.reports == ["success"]


def test_accepted_result_stops_heartbeat(monkeypatch):
    client = FakeClient()
    value = runner(client, heartbeat=0.005)
    monkeypatch.setattr(
        "fireman_market_provider.worker.runner.execute_task",
        lambda _task_type, _payload: {"type": "asset_history_sync"},
    )

    value._run_task(task(), "token-0123456789abcdef")
    accepted_count = client.heartbeats
    time.sleep(0.02)

    assert client.heartbeats == accepted_count


def test_lease_lost_aborts_without_upload_or_result(monkeypatch):
    client = FakeClient()

    def lose(*_args, **_kwargs):
        raise GoAPIError("lost", 409, "task_lease_lost")

    client.heartbeat = lose  # type: ignore[method-assign]
    value = runner(client, heartbeat=0.01)

    def slow_execute(_task_type, _payload):
        time.sleep(0.04)
        return {"type": "asset_history_sync"}

    monkeypatch.setattr("fireman_market_provider.worker.runner.execute_task", slow_execute)
    value._run_task(task(), "token-0123456789abcdef")

    assert client.uploads == 0
    assert client.reports == []


def test_shutdown_releases_active_attempt():
    client = FakeClient()
    value = runner(client)
    value._active = _ActiveAttempt("task_active", "token-0123456789abcdef")

    value.stop()

    assert client.releases == ["task_active"]


def test_claim_conflict_moves_to_next_candidate(monkeypatch):
    client = FakeClient()
    candidates = [task("task_stolen"), task("task_available")]
    client.list_pending = lambda _types: candidates  # type: ignore[attr-defined]

    def claim(task_id, *_args):
        if task_id == "task_stolen":
            raise GoAPIError("conflict", 409, "task_claim_conflict")
        return task(task_id)

    client.claim = claim  # type: ignore[attr-defined]
    value = runner(client)
    executed: list[str] = []

    def run_once(item, _token):
        executed.append(item.id)
        value._stop.set()

    monkeypatch.setattr(value, "_run_task", run_once)
    value._claim_loop()

    assert executed == ["task_available"]
