from __future__ import annotations

import os
import subprocess
import threading
import time

import pytest

from fireman_market_provider.worker.errors import TaskFailure
from fireman_market_provider.worker.process_runner import (
    TaskProcessCanceled,
    TaskProcessRunner,
)


def successful_executor(_task_type: str, payload: dict) -> dict:
    return {"value": payload["value"]}


def failing_executor(_task_type: str, _payload: dict) -> dict:
    raise TaskFailure("upstream_failed", "expected failure", retryable=True)


def blocking_process_tree_executor(_task_type: str, payload: dict) -> dict:
    child = subprocess.Popen(["sleep", "30"])  # noqa: S603,S607
    with open(payload["pid_file"], "w", encoding="ascii") as handle:
        handle.write(f"{os.getpid()} {child.pid}")
    time.sleep(30)
    return {}


def test_process_runner_returns_result_and_task_failure():
    clear = threading.Event()
    result = TaskProcessRunner(successful_executor).run(
        "test", {"value": 7}, clear, clear, clear
    )
    assert result == {"value": 7}

    with pytest.raises(TaskFailure) as error:
        TaskProcessRunner(failing_executor).run("test", {}, clear, clear, clear)
    assert error.value.error_code == "upstream_failed"
    assert error.value.retryable is True


def test_cancel_terminates_task_process_group(tmp_path):
    pid_file = tmp_path / "processes"
    canceled = threading.Event()
    clear = threading.Event()

    def request_cancel() -> None:
        deadline = time.monotonic() + 5
        while not pid_file.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        canceled.set()

    trigger = threading.Thread(target=request_cancel)
    trigger.start()
    with pytest.raises(TaskProcessCanceled):
        TaskProcessRunner(blocking_process_tree_executor).run(
            "test", {"pid_file": str(pid_file)}, canceled, clear, clear
        )
    trigger.join(timeout=1)

    pids = [int(value) for value in pid_file.read_text(encoding="ascii").split()]
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        if all(not process_exists(pid) for pid in pids):
            break
        time.sleep(0.05)
    assert all(not process_exists(pid) for pid in pids)


def process_exists(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    return True
