"""Direct SQLite access to the worker_tasks scheduling table.

The task lifecycle is scheduling state, not business data, so the worker
operates on worker_tasks directly (claim, heartbeat, CAS state transitions,
stale cleanup). Business tables and resource_db are never touched from here.
"""

from __future__ import annotations

import sqlite3
import time
from dataclasses import dataclass
from typing import Any


def now_ms() -> int:
    return int(time.time() * 1000)


@dataclass(frozen=True)
class ClaimedTask:
    id: str
    version_no: int
    type: str
    payload_json: str
    created_at: int


class TaskDB:
    """One connection per TaskDB instance; instances are not thread-safe.

    Each worker thread owns its own TaskDB.
    """

    def __init__(self, db_path: str) -> None:
        self._conn = sqlite3.connect(db_path, timeout=30.0)
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA busy_timeout=30000")
        self._conn.execute("PRAGMA foreign_keys=ON")

    def close(self) -> None:
        self._conn.close()

    # --- claim ---

    def claim_next(self) -> ClaimedTask | None:
        """Atomically claim the oldest pending task (pending -> running)."""
        with self._conn:
            self._conn.execute("BEGIN IMMEDIATE")
            row = self._conn.execute(
                """
                SELECT id, version_no, type, payload_json, created_at
                FROM worker_tasks
                WHERE status='pending'
                ORDER BY created_at
                LIMIT 1
                """
            ).fetchone()
            if row is None:
                return None
            ts = now_ms()
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='running', started_at=?, heartbeat_at=?
                WHERE id=? AND status='pending'
                """,
                (ts, ts, row[0]),
            )
            if cur.rowcount != 1:
                return None
            return ClaimedTask(
                id=row[0],
                version_no=int(row[1]),
                type=row[2],
                payload_json=row[3],
                created_at=int(row[4]),
            )

    # --- running lifecycle ---

    def heartbeat(self, task_id: str) -> bool:
        """Refresh heartbeat; False means the task is no longer ours."""
        with self._conn:
            cur = self._conn.execute(
                "UPDATE worker_tasks SET heartbeat_at=? WHERE id=? AND status='running'",
                (now_ms(), task_id),
            )
            return cur.rowcount == 1

    def mark_pre_complete(self, task_id: str, result_data: str) -> bool:
        """CAS running -> pre_complete with the resource envelope."""
        ts = now_ms()
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='pre_complete',
                    result_data=?,
                    pre_completed_at=?,
                    next_post_process_at=?,
                    heartbeat_at=?
                WHERE id=? AND status='running'
                """,
                (result_data, ts, ts, ts, task_id),
            )
            return cur.rowcount == 1

    def fail_running(self, task_id: str, error_code: str, error_message: str) -> bool:
        """CAS running -> failed."""
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='failed', error_code=?, error_message=?, finished_at=?
                WHERE id=? AND status='running'
                """,
                (error_code, error_message[:2000], now_ms(), task_id),
            )
            return cur.rowcount == 1

    # --- pre_complete lifecycle ---

    def list_due_pre_complete(self, limit: int = 10) -> list[dict[str, Any]]:
        rows = self._conn.execute(
            """
            SELECT id, version_no, post_process_attempts, pre_completed_at
            FROM worker_tasks
            WHERE status='pre_complete'
              AND (next_post_process_at IS NULL OR next_post_process_at <= ?)
            ORDER BY pre_completed_at
            LIMIT ?
            """,
            (now_ms(), limit),
        ).fetchall()
        return [
            {
                "id": r[0],
                "version_no": int(r[1]),
                "post_process_attempts": int(r[2]),
                "pre_completed_at": int(r[3]) if r[3] is not None else None,
            }
            for r in rows
        ]

    def mark_complete(self, task_id: str) -> bool:
        """CAS pre_complete -> complete."""
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='complete', finished_at=?
                WHERE id=? AND status='pre_complete'
                """,
                (now_ms(), task_id),
            )
            return cur.rowcount == 1

    def fail_pre_complete(self, task_id: str, error_code: str, error_message: str) -> bool:
        """CAS pre_complete -> failed."""
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='failed', error_code=?, error_message=?, finished_at=?
                WHERE id=? AND status='pre_complete'
                """,
                (error_code, error_message[:2000], now_ms(), task_id),
            )
            return cur.rowcount == 1

    def schedule_post_process_retry(self, task_id: str, attempts: int, next_at_ms: int) -> bool:
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET post_process_attempts=?, next_post_process_at=?
                WHERE id=? AND status='pre_complete'
                """,
                (attempts, next_at_ms, task_id),
            )
            return cur.rowcount == 1

    # --- janitor ---

    def fail_stale_running(self, stale_before_ms: int) -> int:
        """Mark running tasks whose heartbeat is older than the threshold."""
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='failed',
                    error_code='worker_heartbeat_timeout',
                    error_message='worker heartbeat timeout',
                    finished_at=?
                WHERE status='running' AND heartbeat_at < ?
                """,
                (now_ms(), stale_before_ms),
            )
            return cur.rowcount

    def fail_pre_complete_timeout(self, timeout_before_ms: int) -> int:
        """Mark pre_complete tasks that exceeded the hard timeout."""
        with self._conn:
            cur = self._conn.execute(
                """
                UPDATE worker_tasks
                SET status='failed',
                    error_code='post_process_timeout',
                    error_message='post process timeout',
                    finished_at=?
                WHERE status='pre_complete' AND pre_completed_at < ?
                """,
                (now_ms(), timeout_before_ms),
            )
            return cur.rowcount
