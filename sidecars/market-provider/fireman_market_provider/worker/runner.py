"""Worker loops: claim/execute, heartbeat, stale janitor, pre_complete retry.

Thread layout (single-process, no worker ids; concurrency safety relies on
status CAS updates in worker_tasks):

- claim loop: polls for pending tasks, executes one at a time, uploads the
  result to Go, CAS running -> pre_complete;
- heartbeat thread: refreshes heartbeat_at every 10s while a task runs; a CAS
  miss (0 rows) tells the executor to abandon the task;
- janitor loop: every 30s fails stale running tasks (heartbeat older than
  60s) and pre_complete tasks past the 1h hard timeout;
- notify loop: scans due pre_complete tasks, calls Go post-process, and maps
  success/retryable_error/permanent_error onto CAS terminal transitions with
  exponential backoff (max 10 attempts, max 5min interval).
"""

from __future__ import annotations

import json
import threading
from typing import Any

from ..logutil import get_logger
from .config import WorkerConfig, worker_enabled
from .errors import TaskFailure
from .executors import execute_task
from .goclient import GoAPIError, GoInternalClient
from .taskdb import ClaimedTask, TaskDB, now_ms

logger = get_logger(__name__)


class _HeartbeatLostError(Exception):
    """The running-task heartbeat CAS updated 0 rows: the task is not ours."""


class WorkerRunner:
    def __init__(self, config: WorkerConfig) -> None:
        self._config = config
        self._client = GoInternalClient(config.internal_api_url, config.http_timeout_seconds)
        self._stop = threading.Event()
        self._threads: list[threading.Thread] = []

    # --- lifecycle ---

    def start(self) -> None:
        for name, target in (
            ("worker-claim", self._claim_loop),
            ("worker-janitor", self._janitor_loop),
            ("worker-notify", self._notify_loop),
        ):
            thread = threading.Thread(target=target, name=name, daemon=True)
            thread.start()
            self._threads.append(thread)
        logger.info(
            "worker started db=%s internal_api=%s",
            self._config.db_path,
            self._config.internal_api_url,
        )

    def stop(self) -> None:
        self._stop.set()
        for thread in self._threads:
            thread.join(timeout=5.0)
        self._threads.clear()

    def _wait(self, seconds: float) -> None:
        self._stop.wait(seconds)

    # --- claim + execute ---

    def _claim_loop(self) -> None:
        db = TaskDB(self._config.db_path)
        try:
            while not self._stop.is_set():
                try:
                    task = db.claim_next()
                except Exception:  # noqa: BLE001
                    logger.exception("claim failed")
                    self._wait(self._config.poll_interval_seconds)
                    continue
                if task is None:
                    self._wait(self._config.poll_interval_seconds)
                    continue
                self._run_task(db, task)
        finally:
            db.close()

    def _run_task(self, db: TaskDB, task: ClaimedTask) -> None:
        logger.info("task %s claimed type=%s version=%d", task.id, task.type, task.version_no)
        lost = threading.Event()
        stop_heartbeat = threading.Event()
        heartbeat = threading.Thread(
            target=self._heartbeat_loop,
            args=(task.id, stop_heartbeat, lost),
            name=f"heartbeat-{task.id[:12]}",
            daemon=True,
        )
        heartbeat.start()
        try:
            result = self._execute(task)
            if lost.is_set():
                logger.warning("task %s abandoned: heartbeat lost during execution", task.id)
                return
            self._upload_and_pre_complete(db, task, result)
        except TaskFailure as exc:
            if not db.fail_running(task.id, exc.error_code, exc.message):
                logger.warning("task %s fail CAS missed (status changed)", task.id)
            else:
                logger.info("task %s failed code=%s: %s", task.id, exc.error_code, exc.message)
        except Exception as exc:  # noqa: BLE001
            logger.exception("task %s crashed", task.id)
            db.fail_running(task.id, "worker_internal_error", str(exc)[:500])
        finally:
            stop_heartbeat.set()
            heartbeat.join(timeout=2.0)

    def _execute(self, task: ClaimedTask) -> dict[str, Any]:
        try:
            payload = json.loads(task.payload_json) if task.payload_json else {}
        except json.JSONDecodeError as exc:
            raise TaskFailure("invalid_task_payload", "task payload is not valid JSON") from exc
        if not isinstance(payload, dict):
            raise TaskFailure("invalid_task_payload", "task payload must be a JSON object")
        return execute_task(task.type, payload)

    def _upload_and_pre_complete(self, db: TaskDB, task: ClaimedTask, result: dict[str, Any]) -> None:
        # Order per td/078: resource first (via the Go upload API), envelope +
        # pre_complete second. A failure after upload leaves an orphan resource
        # that Go's TTL cleanup removes.
        try:
            envelope = self._client.upload_result(result)
        except GoAPIError as exc:
            raise TaskFailure("resource_upload_failed", f"resource upload failed: {exc}") from exc

        result_data = json.dumps(envelope, separators=(",", ":"))
        if not db.mark_pre_complete(task.id, result_data):
            logger.warning("task %s pre_complete CAS missed (status changed)", task.id)
            return
        logger.info(
            "task %s pre_complete resource=%s size=%s",
            task.id,
            envelope.get("resource_key", "")[:16],
            envelope.get("size_bytes"),
        )
        # First notification attempt happens immediately; failures are retried
        # by the notify loop with backoff.
        self._notify_once(db, task.id, task.version_no, attempts=0)

    def _heartbeat_loop(self, task_id: str, stop: threading.Event, lost: threading.Event) -> None:
        db = TaskDB(self._config.db_path)
        try:
            while not stop.wait(self._config.heartbeat_interval_seconds):
                if self._stop.is_set():
                    return
                try:
                    if not db.heartbeat(task_id):
                        lost.set()
                        return
                except Exception:  # noqa: BLE001
                    logger.exception("heartbeat failed for task %s", task_id)
        finally:
            db.close()

    # --- janitor ---

    def _janitor_loop(self) -> None:
        db = TaskDB(self._config.db_path)
        try:
            while not self._stop.is_set():
                try:
                    stale = db.fail_stale_running(now_ms() - self._config.stale_after_seconds * 1000)
                    if stale:
                        logger.warning("janitor failed %d stale running tasks", stale)
                    timed_out = db.fail_pre_complete_timeout(
                        now_ms() - self._config.pre_complete_hard_timeout_seconds * 1000
                    )
                    if timed_out:
                        logger.warning("janitor failed %d pre_complete tasks past hard timeout", timed_out)
                except Exception:  # noqa: BLE001
                    logger.exception("janitor scan failed")
                self._wait(self._config.stale_scan_interval_seconds)
        finally:
            db.close()

    # --- pre_complete notification ---

    def _notify_loop(self) -> None:
        db = TaskDB(self._config.db_path)
        try:
            while not self._stop.is_set():
                try:
                    due = db.list_due_pre_complete()
                except Exception:  # noqa: BLE001
                    logger.exception("pre_complete scan failed")
                    due = []
                for item in due:
                    if self._stop.is_set():
                        return
                    self._notify_once(
                        db, item["id"], item["version_no"], attempts=item["post_process_attempts"]
                    )
                self._wait(self._config.pre_complete_scan_interval_seconds)
        finally:
            db.close()

    def _notify_once(self, db: TaskDB, task_id: str, version_no: int, attempts: int) -> None:
        try:
            outcome = self._client.notify_post_process(task_id, version_no)
        except GoAPIError as exc:
            logger.warning("post-process notify failed for %s: %s", task_id, exc)
            self._schedule_retry(db, task_id, attempts, "post_process_unreachable", str(exc))
            return

        if outcome.result == "success":
            if db.mark_complete(task_id):
                logger.info("task %s complete", task_id)
            return
        if outcome.result == "permanent_error":
            if db.fail_pre_complete(task_id, outcome.error_code or "post_process_failed",
                                    outcome.error_message or "post process permanent error"):
                logger.info(
                    "task %s failed permanently code=%s: %s",
                    task_id, outcome.error_code, outcome.error_message,
                )
            return
        # retryable_error (or unknown classification treated as retryable)
        self._schedule_retry(
            db, task_id, attempts,
            outcome.error_code or "post_process_retryable",
            outcome.error_message or "post process retryable error",
        )

    def _schedule_retry(
        self, db: TaskDB, task_id: str, attempts: int, error_code: str, error_message: str
    ) -> None:
        next_attempts = attempts + 1
        if next_attempts >= self._config.max_post_process_attempts:
            if db.fail_pre_complete(task_id, error_code, error_message):
                logger.warning(
                    "task %s failed after %d post-process attempts code=%s",
                    task_id, next_attempts, error_code,
                )
            return
        backoff = min(2**next_attempts, self._config.max_backoff_seconds)
        db.schedule_post_process_retry(task_id, next_attempts, now_ms() + backoff * 1000)
        logger.info(
            "task %s post-process retry %d/%d in %ds",
            task_id, next_attempts, self._config.max_post_process_attempts, backoff,
        )


_runner: WorkerRunner | None = None


def start_worker_from_env() -> WorkerRunner | None:
    """Start the worker loops if enabled; returns the runner (or None)."""
    global _runner
    if not worker_enabled():
        logger.info("worker disabled via FIREMAN_WORKER_ENABLED")
        return None
    if _runner is not None:
        return _runner
    _runner = WorkerRunner(WorkerConfig.from_env())
    _runner.start()
    return _runner


def stop_worker() -> None:
    global _runner
    if _runner is not None:
        _runner.stop()
        _runner = None
