-- System FX instruments (USDCNY, HKDCNY). Not plan holdings; rates are stored in market_data_points.

INSERT INTO instruments (
  id, code, name, market, instrument_type,
  asset_class, region, currency,
  provider, provider_symbol, adjust_policy,
  is_system, expense_ratio, expense_ratio_status, fee_treatment,
  status, created_at, updated_at
) VALUES
  (
    'system_fx_usdcny', 'USDCNY', '美元/人民币', 'SYSTEM', 'fx_rate',
    'fx', 'domestic', 'CNY',
    'system', 'USDCNY', 'none',
    1, NULL, 'not_applicable', 'none',
    'active', 0, 0
  ),
  (
    'system_fx_hkdcny', 'HKDCNY', '港币/人民币', 'SYSTEM', 'fx_rate',
    'fx', 'domestic', 'CNY',
    'system', 'HKDCNY', 'none',
    1, NULL, 'not_applicable', 'none',
    'active', 0, 0
  );
