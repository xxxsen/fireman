-- Research CVaR configuration. Values are fractions (0.95 = 95%) and effective
-- return-day counts. Service validation owns the supported enum values.
ALTER TABLE research_collections
  ADD COLUMN tail_risk_confidence REAL NOT NULL DEFAULT 0.95;

ALTER TABLE research_collections
  ADD COLUMN tail_risk_horizon_days INTEGER NOT NULL DEFAULT 20;
