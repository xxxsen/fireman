-- Bind stress / sensitivity analysis results to the Monte Carlo simulation run
-- they belong to (td/050). Historical rows keep simulation_run_id='' which the
-- service treats as "unattributed / legacy" until they are pruned.
ALTER TABLE analysis_results ADD COLUMN simulation_run_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_analysis_results_run_type_created
ON analysis_results(simulation_run_id, type, created_at DESC);
