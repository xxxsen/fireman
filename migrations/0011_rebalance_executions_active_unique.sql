-- At most one draft/in_progress rebalance execution per plan (concurrent create guard).
CREATE UNIQUE INDEX idx_rebalance_executions_one_active_per_plan
  ON rebalance_executions(plan_id) WHERE status IN ('draft', 'in_progress');
