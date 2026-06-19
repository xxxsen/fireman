-- td/033 snapshot metrics columns.
-- Intentionally a no-op: instrument_simulation_snapshots already declares
-- daily_observation_count / monthly_return_count / volatility_method /
-- metrics_version / history_depth in 0001_init, so a freshly built database
-- needs no further DDL here. (Historically the runtime db.repairSnapshotSchema
-- patched pre-existing databases; that runtime path has been removed in favor of
-- migrations as the single source of schema truth — rebuild old DBs from migrations.)
SELECT 1;
