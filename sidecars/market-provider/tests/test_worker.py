"""Worker infrastructure tests: taskdb CAS lifecycle, runner flows, Go client.

The task table schema mirrors migrations/0020_worker_tasks_market_assets.sql
(worker scheduling part only). Business post-processing lives in Go and is
covered by the Go test suite; here we verify the sidecar side of the
contract: claim/heartbeat/pre_complete CAS transitions, janitor cleanup,
notify retry/backoff decisions and the upload protocol.
"""

from __future__ import annotations

import gzip
import hashlib
import json
import sqlite3
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from fireman_market_provider.worker.config import WorkerConfig
from fireman_market_provider.worker.errors import TaskFailure
from fireman_market_provider.worker.goclient import (
    GoAPIError,
    GoInternalClient,
    PostProcessOutcome,
)
from fireman_market_provider.worker.runner import WorkerRunner
from fireman_market_provider.worker.taskdb import TaskDB, now_ms

WORKER_TASKS_SCHEMA = """
CREATE TABLE worker_tasks (
  id            TEXT PRIMARY KEY,
  version_no    INTEGER NOT NULL,
  type          TEXT NOT NULL,
  status        TEXT NOT NULL,
  dedupe_key    TEXT NOT NULL DEFAULT '',
  payload_json  TEXT NOT NULL,
  result_data   TEXT NOT NULL DEFAULT '',
  heartbeat_at  INTEGER,
  error_code    TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  post_process_attempts INTEGER NOT NULL DEFAULT 0,
  next_post_process_at INTEGER,
  created_at    INTEGER NOT NULL,
  started_at    INTEGER,
  pre_completed_at INTEGER,
  finished_at   INTEGER
);
CREATE INDEX idx_worker_tasks_claim ON worker_tasks(status, created_at);
"""


@pytest.fixture
def db_path(tmp_path) -> str:
    path = str(tmp_path / "fireman.db")
    conn = sqlite3.connect(path)
    conn.executescript(WORKER_TASKS_SCHEMA)
    conn.commit()
    conn.close()
    return path


def insert_task(
    path: str,
    task_id: str = "wt_1",
    *,
    task_type: str = "asset_directory_sync",
    status: str = "pending",
    payload: str = "{}",
    version_no: int = 1,
    created_at: int | None = None,
    **columns,
) -> None:
    conn = sqlite3.connect(path)
    cols = {
        "id": task_id,
        "version_no": version_no,
        "type": task_type,
        "status": status,
        "payload_json": payload,
        "created_at": created_at if created_at is not None else now_ms(),
        **columns,
    }
    names = ", ".join(cols)
    marks = ", ".join("?" for _ in cols)
    conn.execute(f"INSERT INTO worker_tasks ({names}) VALUES ({marks})", list(cols.values()))
    conn.commit()
    conn.close()


def get_task(path: str, task_id: str) -> dict:
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    row = conn.execute("SELECT * FROM worker_tasks WHERE id=?", (task_id,)).fetchone()
    conn.close()
    assert row is not None, f"task {task_id} not found"
    return dict(row)


@pytest.fixture
def task_db(db_path: str):
    db = TaskDB(db_path)
    yield db
    db.close()


# --- TaskDB lifecycle ---


class TestTaskDB:
    def test_claim_next_takes_oldest_pending(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_new", created_at=2000, version_no=2)
        insert_task(db_path, "wt_old", created_at=1000, version_no=1)

        claimed = task_db.claim_next()
        assert claimed is not None
        assert claimed.id == "wt_old"
        assert claimed.version_no == 1
        row = get_task(db_path, "wt_old")
        assert row["status"] == "running"
        assert row["started_at"] is not None
        assert row["heartbeat_at"] is not None

        second = task_db.claim_next()
        assert second is not None and second.id == "wt_new"
        assert task_db.claim_next() is None

    def test_claim_next_empty_queue_takes_no_write_lock(
        self, db_path: str, task_db: TaskDB
    ) -> None:
        """Idle polling must stay read-only: an empty queue is probed without
        BEGIN IMMEDIATE, so workers never compete for SQLite's RESERVED lock
        with the Go process."""
        statements: list[str] = []
        task_db._conn.set_trace_callback(statements.append)  # noqa: SLF001 - test seam
        try:
            assert task_db.claim_next() is None
        finally:
            task_db._conn.set_trace_callback(None)  # noqa: SLF001 - test seam
        writes = [s for s in statements if "BEGIN IMMEDIATE" in s.upper()]
        assert writes == [], f"empty-queue claim_next must not open a write tx: {statements}"

    def test_claim_next_with_pending_enters_write_tx_and_claims(
        self, db_path: str, task_db: TaskDB
    ) -> None:
        insert_task(db_path, "wt_probe")
        statements: list[str] = []
        task_db._conn.set_trace_callback(statements.append)  # noqa: SLF001 - test seam
        try:
            claimed = task_db.claim_next()
        finally:
            task_db._conn.set_trace_callback(None)  # noqa: SLF001 - test seam
        assert claimed is not None and claimed.id == "wt_probe"
        assert get_task(db_path, "wt_probe")["status"] == "running"
        # The actual claim still runs under BEGIN IMMEDIATE (CAS semantics).
        assert any("BEGIN IMMEDIATE" in s.upper() for s in statements)

    def test_claim_next_returns_none_when_probed_task_is_stolen(
        self, db_path: str, task_db: TaskDB, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The probe runs outside the transaction; if another worker takes the
        task in between, the in-transaction re-check must miss gracefully."""
        insert_task(db_path, "wt_race")
        original = TaskDB._claim_oldest_pending

        def steal_then_claim(self: TaskDB):
            other = sqlite3.connect(db_path)
            other.execute("UPDATE worker_tasks SET status='running' WHERE id='wt_race'")
            other.commit()
            other.close()
            return original(self)

        monkeypatch.setattr(TaskDB, "_claim_oldest_pending", steal_then_claim)
        assert task_db.claim_next() is None
        assert get_task(db_path, "wt_race")["status"] == "running"

    def test_heartbeat_only_updates_running(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_run")
        assert task_db.claim_next() is not None
        assert task_db.heartbeat("wt_run") is True

        assert task_db.fail_running("wt_run", "boom", "x") is True
        assert task_db.heartbeat("wt_run") is False

    def test_mark_pre_complete_cas(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_a")
        assert task_db.claim_next() is not None
        assert task_db.mark_pre_complete("wt_a", '{"resource_key":"abc"}') is True
        row = get_task(db_path, "wt_a")
        assert row["status"] == "pre_complete"
        assert row["result_data"] == '{"resource_key":"abc"}'
        assert row["pre_completed_at"] is not None
        assert row["next_post_process_at"] is not None

        # Second CAS on a non-running task misses.
        assert task_db.mark_pre_complete("wt_a", "{}") is False

    def test_fail_running_truncates_message(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_f")
        assert task_db.claim_next() is not None
        assert task_db.fail_running("wt_f", "upstream_error", "x" * 5000) is True
        row = get_task(db_path, "wt_f")
        assert row["status"] == "failed"
        assert row["error_code"] == "upstream_error"
        assert len(row["error_message"]) == 2000

    def test_list_due_pre_complete_respects_backoff(self, db_path: str, task_db: TaskDB) -> None:
        ts = now_ms()
        insert_task(
            db_path, "wt_due", status="pre_complete",
            pre_completed_at=ts - 1000, next_post_process_at=ts - 500,
        )
        insert_task(
            db_path, "wt_later", status="pre_complete",
            pre_completed_at=ts - 1000, next_post_process_at=ts + 60_000,
        )
        due = task_db.list_due_pre_complete()
        assert [item["id"] for item in due] == ["wt_due"]

    def test_terminal_cas_transitions(self, db_path: str, task_db: TaskDB) -> None:
        ts = now_ms()
        insert_task(db_path, "wt_ok", status="pre_complete", pre_completed_at=ts)
        insert_task(db_path, "wt_bad", status="pre_complete", pre_completed_at=ts)

        assert task_db.mark_complete("wt_ok") is True
        assert get_task(db_path, "wt_ok")["status"] == "complete"
        # complete is terminal: a duplicate CAS misses.
        assert task_db.mark_complete("wt_ok") is False
        assert task_db.fail_pre_complete("wt_ok", "x", "y") is False

        assert task_db.fail_pre_complete("wt_bad", "post_process_failed", "nope") is True
        row = get_task(db_path, "wt_bad")
        assert row["status"] == "failed"
        assert row["error_code"] == "post_process_failed"

    def test_schedule_post_process_retry(self, db_path: str, task_db: TaskDB) -> None:
        ts = now_ms()
        insert_task(db_path, "wt_r", status="pre_complete", pre_completed_at=ts)
        assert task_db.schedule_post_process_retry("wt_r", 3, ts + 8000) is True
        row = get_task(db_path, "wt_r")
        assert row["post_process_attempts"] == 3
        assert row["next_post_process_at"] == ts + 8000

    def test_janitor_fails_stale_and_timed_out(self, db_path: str, task_db: TaskDB) -> None:
        ts = now_ms()
        insert_task(db_path, "wt_stale", status="running", heartbeat_at=ts - 120_000)
        insert_task(db_path, "wt_alive", status="running", heartbeat_at=ts)
        insert_task(db_path, "wt_old_pc", status="pre_complete", pre_completed_at=ts - 7_200_000)
        insert_task(db_path, "wt_new_pc", status="pre_complete", pre_completed_at=ts)

        assert task_db.fail_stale_running(ts - 60_000) == 1
        stale = get_task(db_path, "wt_stale")
        assert stale["status"] == "failed"
        assert stale["error_code"] == "worker_heartbeat_timeout"
        assert get_task(db_path, "wt_alive")["status"] == "running"

        assert task_db.fail_pre_complete_timeout(ts - 3_600_000) == 1
        timed_out = get_task(db_path, "wt_old_pc")
        assert timed_out["status"] == "failed"
        assert timed_out["error_code"] == "post_process_timeout"
        assert get_task(db_path, "wt_new_pc")["status"] == "pre_complete"


# --- WorkerRunner flows (no threads; loops exercised synchronously) ---


class StubGoClient:
    def __init__(
        self,
        outcome: PostProcessOutcome | Exception = PostProcessOutcome("success"),
        upload_error: Exception | None = None,
    ) -> None:
        self.outcome = outcome
        self.upload_error = upload_error
        self.uploaded: list[dict] = []
        self.notified: list[tuple[str, int]] = []

    def upload_result(self, result: dict) -> dict:
        if self.upload_error is not None:
            raise self.upload_error
        self.uploaded.append(result)
        raw = json.dumps(result, separators=(",", ":")).encode()
        digest = hashlib.sha256(gzip.compress(raw, mtime=0)).hexdigest()
        return {"resource_key": digest, "sha256": digest, "size_bytes": len(raw)}

    def notify_post_process(self, task_id: str, version_no: int) -> PostProcessOutcome:
        self.notified.append((task_id, version_no))
        if isinstance(self.outcome, Exception):
            raise self.outcome
        return self.outcome


def make_runner(db_path: str, client: StubGoClient, **config_overrides) -> WorkerRunner:
    config = WorkerConfig(
        db_path=db_path, internal_api_url="http://unused", **config_overrides
    )
    runner = WorkerRunner(config)
    runner._client = client  # noqa: SLF001 - test seam
    return runner


class TestWorkerRunner:
    def test_success_flow_uploads_then_completes(
        self, db_path: str, task_db: TaskDB, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        insert_task(db_path, "wt_s", payload='{"scope":"hk_all"}')
        monkeypatch.setattr(
            "fireman_market_provider.worker.runner.execute_task",
            lambda task_type, payload: {"type": task_type, "echo": payload},
        )
        client = StubGoClient(outcome=PostProcessOutcome("success"))
        runner = make_runner(db_path, client)

        task = task_db.claim_next()
        assert task is not None
        runner._run_task(task_db, task)  # noqa: SLF001

        assert client.uploaded == [{"type": "asset_directory_sync", "echo": {"scope": "hk_all"}}]
        assert client.notified == [("wt_s", 1)]
        row = get_task(db_path, "wt_s")
        assert row["status"] == "complete"
        envelope = json.loads(row["result_data"])
        assert envelope["resource_key"]

    def test_task_failure_marks_failed_with_code(
        self, db_path: str, task_db: TaskDB, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        insert_task(db_path, "wt_f")

        def boom(task_type, payload):
            raise TaskFailure("source_unavailable", "pinned source rejected symbol")

        monkeypatch.setattr("fireman_market_provider.worker.runner.execute_task", boom)
        runner = make_runner(db_path, StubGoClient())

        task = task_db.claim_next()
        runner._run_task(task_db, task)  # noqa: SLF001

        row = get_task(db_path, "wt_f")
        assert row["status"] == "failed"
        assert row["error_code"] == "source_unavailable"
        assert "pinned source" in row["error_message"]

    def test_upload_failure_marks_failed(
        self, db_path: str, task_db: TaskDB, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        insert_task(db_path, "wt_u")
        monkeypatch.setattr(
            "fireman_market_provider.worker.runner.execute_task",
            lambda task_type, payload: {"type": task_type},
        )
        client = StubGoClient(upload_error=GoAPIError("backend is down"))
        runner = make_runner(db_path, client)

        task = task_db.claim_next()
        runner._run_task(task_db, task)  # noqa: SLF001

        row = get_task(db_path, "wt_u")
        assert row["status"] == "failed"
        assert row["error_code"] == "resource_upload_failed"

    def test_unexpected_crash_marks_internal_error(
        self, db_path: str, task_db: TaskDB, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        insert_task(db_path, "wt_c")
        monkeypatch.setattr(
            "fireman_market_provider.worker.runner.execute_task",
            lambda task_type, payload: 1 / 0,
        )
        runner = make_runner(db_path, StubGoClient())

        task = task_db.claim_next()
        runner._run_task(task_db, task)  # noqa: SLF001

        row = get_task(db_path, "wt_c")
        assert row["status"] == "failed"
        assert row["error_code"] == "worker_internal_error"

    def test_notify_permanent_error_fails_task(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_p", status="pre_complete", pre_completed_at=now_ms())
        client = StubGoClient(
            outcome=PostProcessOutcome("permanent_error", "source_mismatch", "wrong source")
        )
        runner = make_runner(db_path, client)

        runner._notify_once(task_db, "wt_p", 1, attempts=0)  # noqa: SLF001

        row = get_task(db_path, "wt_p")
        assert row["status"] == "failed"
        assert row["error_code"] == "source_mismatch"

    def test_notify_retryable_schedules_backoff(self, db_path: str, task_db: TaskDB) -> None:
        before = now_ms()
        insert_task(db_path, "wt_r", status="pre_complete", pre_completed_at=before)
        client = StubGoClient(
            outcome=PostProcessOutcome("retryable_error", "internal_error", "db busy")
        )
        runner = make_runner(db_path, client)

        runner._notify_once(task_db, "wt_r", 1, attempts=0)  # noqa: SLF001

        row = get_task(db_path, "wt_r")
        assert row["status"] == "pre_complete"
        assert row["post_process_attempts"] == 1
        assert row["next_post_process_at"] >= before + 2000  # 2^1 seconds backoff

    def test_notify_unreachable_exhausts_attempts(self, db_path: str, task_db: TaskDB) -> None:
        insert_task(db_path, "wt_x", status="pre_complete", pre_completed_at=now_ms())
        client = StubGoClient(outcome=GoAPIError("connection refused"))
        runner = make_runner(db_path, client, max_post_process_attempts=3)

        # Attempt count comes from the row; the final attempt flips to failed.
        runner._notify_once(task_db, "wt_x", 1, attempts=2)  # noqa: SLF001

        row = get_task(db_path, "wt_x")
        assert row["status"] == "failed"
        assert row["error_code"] == "post_process_unreachable"


# --- GoInternalClient protocol ---


class _FakeGoHandler(BaseHTTPRequestHandler):
    behavior = "ok"

    def do_POST(self) -> None:  # noqa: N802 - http.server API
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.path == "/internal/resources":
            self._handle_upload(body)
        elif self.path.endswith("/post-process"):
            self._handle_post_process()
        else:
            self._reply(404, {"code": "not_found"})

    def _handle_upload(self, body: bytes) -> None:
        if self.behavior == "http_error":
            self._reply(500, {"code": "resource_store_failed"})
            return
        digest = hashlib.sha256(body).hexdigest()
        declared = self.headers.get("X-Fireman-Content-SHA256", "")
        if declared != digest:
            self._reply(400, {"code": "resource_checksum_mismatch"})
            return
        assert self.headers.get("X-Fireman-Content-Encoding") == "gzip"
        assert json.loads(gzip.decompress(body))  # payload is valid gzipped JSON
        key = "1" * 64 if self.behavior == "wrong_key" else digest
        self._reply(200, {"code": "ok", "data": {
            "resource_key": key, "sha256": key, "size_bytes": len(body),
        }})

    def _handle_post_process(self) -> None:
        if self.behavior == "missing_result":
            self._reply(200, {"code": "ok", "data": {}})
            return
        self._reply(200, {"code": "ok", "data": {
            "result": "permanent_error",
            "error_code": "source_mismatch",
            "error_message": "wrong source",
        }})

    def _reply(self, status: int, payload: dict) -> None:
        raw = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *args) -> None:  # silence test output
        del args


@pytest.fixture
def fake_go_server():
    server = ThreadingHTTPServer(("127.0.0.1", 0), _FakeGoHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield f"http://127.0.0.1:{server.server_port}"
    server.shutdown()
    thread.join(timeout=2.0)
    _FakeGoHandler.behavior = "ok"


class TestGoInternalClient:
    def test_upload_result_roundtrip(self, fake_go_server: str) -> None:
        _FakeGoHandler.behavior = "ok"
        client = GoInternalClient(fake_go_server)
        envelope = client.upload_result({"type": "fx_rate_sync", "rates": []})
        raw = json.dumps(
            {"type": "fx_rate_sync", "rates": []}, ensure_ascii=False, separators=(",", ":")
        ).encode()
        expected = hashlib.sha256(gzip.compress(raw, mtime=0)).hexdigest()
        assert envelope["resource_key"] == expected

    def test_upload_result_rejects_key_mismatch(self, fake_go_server: str) -> None:
        _FakeGoHandler.behavior = "wrong_key"
        client = GoInternalClient(fake_go_server)
        with pytest.raises(GoAPIError, match="resource key mismatch"):
            client.upload_result({"type": "fx_rate_sync"})

    def test_upload_result_wraps_http_errors(self, fake_go_server: str) -> None:
        _FakeGoHandler.behavior = "http_error"
        client = GoInternalClient(fake_go_server)
        with pytest.raises(GoAPIError, match="HTTP 500"):
            client.upload_result({"type": "fx_rate_sync"})

    def test_notify_post_process_parses_outcome(self, fake_go_server: str) -> None:
        _FakeGoHandler.behavior = "ok"
        client = GoInternalClient(fake_go_server)
        outcome = client.notify_post_process("wt_1", 7)
        assert outcome == PostProcessOutcome(
            "permanent_error", "source_mismatch", "wrong source"
        )

    def test_notify_post_process_requires_result(self, fake_go_server: str) -> None:
        _FakeGoHandler.behavior = "missing_result"
        client = GoInternalClient(fake_go_server)
        with pytest.raises(GoAPIError, match="missing result"):
            client.notify_post_process("wt_1", 7)

    def test_unreachable_backend_raises(self) -> None:
        client = GoInternalClient("http://127.0.0.1:1", timeout_seconds=0.5)
        with pytest.raises(GoAPIError, match="unreachable"):
            client.notify_post_process("wt_1", 1)
