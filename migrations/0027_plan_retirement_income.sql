-- Stable after-retirement net income (pension, net rent, long-term side income).
-- Defaults preserve every existing plan's behavior.
ALTER TABLE plan_parameters
ADD COLUMN annual_retirement_income_minor INTEGER NOT NULL DEFAULT 0;

ALTER TABLE plan_parameters
ADD COLUMN annual_retirement_income_growth_rate REAL NOT NULL DEFAULT 0;
