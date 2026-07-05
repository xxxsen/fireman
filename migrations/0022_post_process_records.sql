-- 回调记录：每次 POST /internal/tasks/{id}/post-process 由 Go 落一条。
-- append-only，仅用于管理后台观测；不参与任何业务判定（幂等仍由
-- market_data_versions 保证）。
--
-- 表名用 post_process_records 而非 post_process_attempts：
-- worker_tasks.post_process_attempts 是 sidecar 维护的"下一次重试前已尝试的
-- 次数"计数列，与本表"每次回调一条记录"不是同一事物。
--
-- 不加 FOREIGN KEY(task_id)：worker_tasks 行未来若引入清理策略，回调记录
-- 应可独立留存；两者生命周期解耦。
CREATE TABLE post_process_records (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id       TEXT    NOT NULL,
  task_type     TEXT    NOT NULL DEFAULT '',
  attempt_no    INTEGER NOT NULL DEFAULT 0,  -- 回调时刻 worker_tasks.post_process_attempts 快照
  result        TEXT    NOT NULL,            -- success | retryable_error | permanent_error
  error_code    TEXT    NOT NULL DEFAULT '',
  error_message TEXT    NOT NULL DEFAULT '',
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

CREATE INDEX idx_post_process_records_task
ON post_process_records(task_id, created_at DESC);

CREATE INDEX idx_post_process_records_created
ON post_process_records(created_at);
