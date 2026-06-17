-- Persist the resolved instrument kind (etf/index_etf/lof/stock/mutual_fund/...)
-- so the refresh path can request identity-consistent history sources instead of
-- relying on the legacy ETF->LOF->stock fallback chain that can mix data across
-- instruments sharing a bare code (td/037 现象-4, td/038 P1-1).
ALTER TABLE instruments ADD COLUMN instrument_kind TEXT NOT NULL DEFAULT '';
