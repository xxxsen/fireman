-- td/057 P1: precomputed asset-library list projection.
--
-- The library list (`GET /api/v1/instruments`) and holdings search must read
-- each instrument's market metadata, simulation eligibility and trailing
-- 1/3/5y annualized returns in a constant number of queries (a single
-- LEFT JOIN), instead of recomputing the full price history per row on every
-- HTTP request (the previous N+1 LastTradeDate / LatestPointMeta /
-- ListByInstrument pattern).
--
-- The projection is (re)written only inside the same transaction that persists
-- market_data_points on a successful import / refresh / retry. Trailing windows
-- end at the instrument's own data_as_of (its last trade date), so a 停更
-- instrument still exposes its 3/5y returns instead of being truncated by the
-- server's current date. Failed / pending / system rows are never projected and
-- render "—". This table is managed exclusively through migrations (no runtime
-- DDL); rebuild existing development databases and re-run import/refresh to
-- populate it.
CREATE TABLE instrument_library_metrics (
  instrument_id          TEXT    PRIMARY KEY,
  data_as_of             TEXT    NOT NULL DEFAULT '',
  data_source_name       TEXT    NOT NULL DEFAULT '',
  point_type             TEXT    NOT NULL DEFAULT '',
  quality_status         TEXT    NOT NULL DEFAULT '',
  simulation_eligible    INTEGER NOT NULL DEFAULT 0,
  history_depth          TEXT    NOT NULL DEFAULT '',
  complete_year_count    INTEGER NOT NULL DEFAULT 0,
  monthly_return_count   INTEGER NOT NULL DEFAULT 0,
  metrics_version        TEXT    NOT NULL DEFAULT '',
  warnings_json          TEXT    NOT NULL DEFAULT '[]',
  trailing_as_of         TEXT    NOT NULL DEFAULT '',
  trailing_1y_annualized REAL,
  trailing_3y_annualized REAL,
  trailing_5y_annualized REAL,
  updated_at             INTEGER NOT NULL,
  FOREIGN KEY(instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);
