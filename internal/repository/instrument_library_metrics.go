package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// LibraryMetricsRecord is one row of the instrument_library_metrics projection:
// the precomputed asset-library list view (market metadata, simulation
// eligibility and trailing 1/3/5y annualized returns) for a single instrument.
type LibraryMetricsRecord struct {
	InstrumentID        string
	DataAsOf            string
	DataSourceName      string
	PointType           string
	QualityStatus       string
	SimulationEligible  bool
	HistoryDepth        string
	CompleteYearCount   int
	MonthlyReturnCount  int
	MetricsVersion      string
	WarningsJSON        string
	TrailingAsOf        string
	OneYearAnnualized   *float64
	ThreeYearAnnualized *float64
	FiveYearAnnualized  *float64
}

// InstrumentLibraryMetricsRepo persists and reads the asset-library list
// projection. The projection is written only inside the transaction that stores
// market_data_points (import/refresh/retry) and read via a single LEFT JOIN in
// InstrumentRepo.ListWithMetrics / Search, so the library list never recomputes
// full history per row.
type InstrumentLibraryMetricsRepo struct {
	db *sql.DB
}

func NewInstrumentLibraryMetricsRepo(db *sql.DB) *InstrumentLibraryMetricsRepo {
	return &InstrumentLibraryMetricsRepo{db: db}
}

func (r *InstrumentLibraryMetricsRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// Upsert writes (or replaces) the projection for one instrument. It must be
// called inside the same transaction that persists market_data_points so the
// list projection and stored history can never diverge.
func (r *InstrumentLibraryMetricsRepo) Upsert(ctx context.Context, tx *sql.Tx, rec LibraryMetricsRecord) error {
	warnings := rec.WarningsJSON
	if warnings == "" {
		warnings = "[]"
	}
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO instrument_library_metrics (
			instrument_id, data_as_of, data_source_name, point_type, quality_status,
			simulation_eligible, history_depth, complete_year_count, monthly_return_count,
			metrics_version, warnings_json, trailing_as_of,
			trailing_1y_annualized, trailing_3y_annualized, trailing_5y_annualized, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(instrument_id) DO UPDATE SET
			data_as_of=excluded.data_as_of,
			data_source_name=excluded.data_source_name,
			point_type=excluded.point_type,
			quality_status=excluded.quality_status,
			simulation_eligible=excluded.simulation_eligible,
			history_depth=excluded.history_depth,
			complete_year_count=excluded.complete_year_count,
			monthly_return_count=excluded.monthly_return_count,
			metrics_version=excluded.metrics_version,
			warnings_json=excluded.warnings_json,
			trailing_as_of=excluded.trailing_as_of,
			trailing_1y_annualized=excluded.trailing_1y_annualized,
			trailing_3y_annualized=excluded.trailing_3y_annualized,
			trailing_5y_annualized=excluded.trailing_5y_annualized,
			updated_at=excluded.updated_at`,
		rec.InstrumentID, rec.DataAsOf, rec.DataSourceName, rec.PointType, rec.QualityStatus,
		boolToInt(rec.SimulationEligible), rec.HistoryDepth, rec.CompleteYearCount, rec.MonthlyReturnCount,
		rec.MetricsVersion, warnings, rec.TrailingAsOf,
		rec.OneYearAnnualized, rec.ThreeYearAnnualized, rec.FiveYearAnnualized, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert instrument library metrics: %w", err)
	}
	return nil
}

// instrumentBaseColumnsAliased is the instruments column list qualified with the
// "i" alias for joined list/search queries (instruments shares updated_at with
// the projection table, so qualification is required).
const instrumentBaseColumnsAliased = `i.id, i.code, i.name, i.market, i.instrument_type,
		i.asset_class, i.region, i.currency,
		i.provider, i.provider_symbol, i.adjust_policy, i.instrument_kind, i.is_system,
		i.expense_ratio, i.expense_ratio_status, i.fee_treatment, i.status,
		i.created_at, i.updated_at`

// instrumentProjectionColumns is the instrument_library_metrics column list
// (aliased "m") in the exact order scanInstrumentListRow expects.
const instrumentProjectionColumns = `m.data_as_of, m.data_source_name, m.point_type, m.quality_status,
		m.simulation_eligible, m.history_depth, m.complete_year_count, m.monthly_return_count,
		m.metrics_version, m.warnings_json, m.trailing_as_of,
		m.trailing_1y_annualized, m.trailing_3y_annualized, m.trailing_5y_annualized`

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func scanInstrumentListRecords(rows *sql.Rows) ([]InstrumentRecord, error) {
	var out []InstrumentRecord
	for rows.Next() {
		rec, err := scanInstrumentListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instruments: %w", err)
	}
	return out, nil
}

func scanInstrumentListRow(rows *sql.Rows) (InstrumentRecord, error) {
	var inst InstrumentRecord
	var isSystem int
	var expenseRatio sql.NullFloat64
	var (
		dataAsOf, dataSource, pointType, quality   sql.NullString
		historyDepth, metricsVer, warningsJSON     sql.NullString
		trailingAsOf                               sql.NullString
		simEligible, completeYears, monthlyReturns sql.NullInt64
		oneY, threeY, fiveY                        sql.NullFloat64
	)
	if err := rows.Scan(
		&inst.ID, &inst.Code, &inst.Name, &inst.Market, &inst.InstrumentType,
		&inst.AssetClass, &inst.Region, &inst.Currency,
		&inst.Provider, &inst.ProviderSymbol, &inst.AdjustPolicy, &inst.InstrumentKind, &isSystem,
		&expenseRatio, &inst.ExpenseRatioStatus, &inst.FeeTreatment, &inst.Status,
		&inst.CreatedAt, &inst.UpdatedAt,
		&dataAsOf, &dataSource, &pointType, &quality,
		&simEligible, &historyDepth, &completeYears, &monthlyReturns,
		&metricsVer, &warningsJSON, &trailingAsOf,
		&oneY, &threeY, &fiveY,
	); err != nil {
		return InstrumentRecord{}, fmt.Errorf("scan instrument row: %w", err)
	}
	inst.IsSystem = isSystem == 1
	if expenseRatio.Valid {
		v := expenseRatio.Float64
		inst.ExpenseRatio = &v
	}
	// dataAsOf is NOT NULL in the projection table, so a valid value here means a
	// projection row exists (LEFT JOIN matched).
	if dataAsOf.Valid {
		inst.DataAsOf = dataAsOf.String
		inst.DataSourceName = dataSource.String
		inst.PointType = pointType.String
		inst.QualityStatus = quality.String
		inst.SimulationEligible = simEligible.Valid && simEligible.Int64 == 1
		inst.HistoryDepth = historyDepth.String
		inst.CompleteYearCount = int(completeYears.Int64)
		inst.MonthlyReturnCount = int(monthlyReturns.Int64)
		inst.MetricsVersion = metricsVer.String
		if warningsJSON.Valid && warningsJSON.String != "" {
			var ws []string
			if err := json.Unmarshal([]byte(warningsJSON.String), &ws); err == nil {
				inst.Warnings = ws
			}
		}
		inst.TrailingReturns = &InstrumentTrailingReturns{
			AsOfDate:                  trailingAsOf.String,
			OneYearAnnualizedReturn:   nullFloatPtr(oneY),
			ThreeYearAnnualizedReturn: nullFloatPtr(threeY),
			FiveYearAnnualizedReturn:  nullFloatPtr(fiveY),
		}
	}
	return inst, nil
}
