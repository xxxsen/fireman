-- Scenario templates store region weights within each asset class.

CREATE TABLE allocation_scenario_region_targets (
  scenario_id         TEXT NOT NULL,
  asset_class         TEXT NOT NULL,
  region              TEXT NOT NULL,
  weight_within_class REAL NOT NULL,
  PRIMARY KEY (scenario_id, asset_class, region),
  FOREIGN KEY (scenario_id) REFERENCES allocation_scenarios(id) ON DELETE CASCADE
);

-- Backfill built-in and custom scenarios with the wizard default (100% domestic per class).
INSERT INTO allocation_scenario_region_targets (scenario_id, asset_class, region, weight_within_class)
SELECT s.id, ac.asset_class, r.region,
  CASE WHEN r.region = 'domestic' THEN 1.0 ELSE 0.0 END
FROM allocation_scenarios s
CROSS JOIN (
  SELECT 'equity' AS asset_class UNION ALL
  SELECT 'bond' UNION ALL
  SELECT 'cash'
) ac
CROSS JOIN (
  SELECT 'domestic' AS region UNION ALL
  SELECT 'foreign'
) r;
