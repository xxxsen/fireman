"""Market data worker: claims tasks through the Go API, executes
remote fetches, uploads results to the Go backend via the internal resource
upload API, and drives the task state machine (running -> pre_complete ->
complete/failed).

The worker never touches resource_db: that database is owned exclusively by
the Go layer. Result payloads travel through the Go internal HTTP API and the
worker only ever holds resource keys (payload sha256 values).
"""

from .runner import WorkerRunner, start_worker_from_env

__all__ = ["WorkerRunner", "start_worker_from_env"]
