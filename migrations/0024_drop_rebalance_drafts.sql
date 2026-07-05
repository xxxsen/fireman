-- 调仓入口二选一：保留「调仓执行」，整体下线「调仓计划（draft）」链路。
-- draft 相关三张表连同历史数据一并删除；0007 的 asset_refresh_events 与 draft
-- 无关，继续有效。

DROP TABLE IF EXISTS rebalance_draft_events;
DROP TABLE IF EXISTS rebalance_draft_lines;
DROP TABLE IF EXISTS rebalance_drafts;
