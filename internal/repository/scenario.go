package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrScenarioNotFound = errors.New("scenario not found")
	ErrBuiltinScenario  = errors.New("builtin scenario cannot be deleted")
	ErrScenarioInUse    = errors.New("scenario is referenced by plans")
)

// ScenarioRepo manages allocation scenarios.
type ScenarioRepo struct {
	db *sql.DB
}

func NewScenarioRepo(db *sql.DB) *ScenarioRepo {
	return &ScenarioRepo{db: db}
}

func (r *ScenarioRepo) List(ctx context.Context) ([]AllocationScenario, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.description, s.is_builtin, s.created_at, s.updated_at,
			(SELECT COUNT(*) FROM plan_parameters p WHERE p.selected_scenario_id = s.id) AS plan_count
		FROM allocation_scenarios s ORDER BY s.is_builtin DESC, s.name`)
	if err != nil {
		return nil, wrapSQL("list allocation scenarios", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AllocationScenario
	for rows.Next() {
		var s AllocationScenario
		var builtin int
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Description, &builtin,
			&s.CreatedAt, &s.UpdatedAt, &s.PlanCount,
		); err != nil {
			return nil, wrapSQL("scan allocation scenario", err)
		}
		s.IsBuiltin = builtin == 1
		weights, err := r.getWeights(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		s.Weights = weights
		regions, err := r.getRegionTargets(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		s.RegionTargets = regions
		out = append(out, s)
	}
	return out, wrapSQL("iterate allocation scenarios", rows.Err())
}

func (r *ScenarioRepo) GetByID(ctx context.Context, id string) (AllocationScenario, error) {
	var s AllocationScenario
	var builtin int
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_builtin, created_at, updated_at
		FROM allocation_scenarios WHERE id=?`, id).Scan(
		&s.ID, &s.Name, &s.Description, &builtin, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AllocationScenario{}, ErrScenarioNotFound
	}
	if err != nil {
		return AllocationScenario{}, wrapSQL("scan allocation scenario", err)
	}
	s.IsBuiltin = builtin == 1
	s.Weights, err = r.getWeights(ctx, id)
	if err != nil {
		return AllocationScenario{}, err
	}
	s.RegionTargets, err = r.getRegionTargets(ctx, id)
	if err != nil {
		return AllocationScenario{}, err
	}
	var count int
	_ = r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM plan_parameters WHERE selected_scenario_id=?`, id).Scan(&count)
	s.PlanCount = count
	return s, nil
}

func (r *ScenarioRepo) getWeights(ctx context.Context, scenarioID string) ([]AssetClassTarget, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT asset_class, weight FROM allocation_scenario_weights
		WHERE scenario_id=? ORDER BY asset_class`, scenarioID)
	if err != nil {
		return nil, wrapSQL("list scenario weights", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AssetClassTarget
	for rows.Next() {
		var t AssetClassTarget
		if err := rows.Scan(&t.AssetClass, &t.Weight); err != nil {
			return nil, wrapSQL("scan scenario weight", err)
		}
		out = append(out, t)
	}
	return out, wrapSQL("iterate scenario weights", rows.Err())
}

func (r *ScenarioRepo) getRegionTargets(ctx context.Context, scenarioID string) ([]RegionTarget, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT asset_class, region, weight_within_class FROM allocation_scenario_region_targets
		WHERE scenario_id=? ORDER BY asset_class, region`, scenarioID)
	if err != nil {
		return nil, wrapSQL("list scenario region targets", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RegionTarget
	for rows.Next() {
		var t RegionTarget
		if err := rows.Scan(&t.AssetClass, &t.Region, &t.WeightWithinClass); err != nil {
			return nil, wrapSQL("scan scenario region target", err)
		}
		out = append(out, t)
	}
	return out, wrapSQL("iterate scenario region targets", rows.Err())
}

func (r *ScenarioRepo) Create(ctx context.Context, s AllocationScenario) error {
	now := time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin create scenario tx", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO allocation_scenarios (id, name, description, is_builtin, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)`,
		s.ID, s.Name, s.Description, s.CreatedAt, s.UpdatedAt); err != nil {
		return fmt.Errorf("create scenario: %w", err)
	}
	for _, w := range s.Weights {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO allocation_scenario_weights (scenario_id, asset_class, weight) VALUES (?,?,?)`,
			s.ID, w.AssetClass, w.Weight); err != nil {
			return wrapSQL("insert scenario weight", err)
		}
	}
	for _, t := range s.RegionTargets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO allocation_scenario_region_targets
				(scenario_id, asset_class, region, weight_within_class) VALUES (?,?,?,?)`,
			s.ID, t.AssetClass, t.Region, t.WeightWithinClass); err != nil {
			return wrapSQL("insert scenario region target", err)
		}
	}
	return wrapSQL("commit create scenario tx", tx.Commit())
}

func (r *ScenarioRepo) Update(ctx context.Context, s AllocationScenario) error {
	now := time.Now().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin update scenario tx", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE allocation_scenarios SET name=?, description=?, updated_at=? WHERE id=?`,
		s.Name, s.Description, now, s.ID)
	if err != nil {
		return wrapSQL("update scenario", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrScenarioNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM allocation_scenario_weights WHERE scenario_id=?`, s.ID); err != nil {
		return wrapSQL("delete scenario weights", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM allocation_scenario_region_targets WHERE scenario_id=?`, s.ID); err != nil {
		return wrapSQL("delete scenario region targets", err)
	}
	for _, w := range s.Weights {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO allocation_scenario_weights (scenario_id, asset_class, weight) VALUES (?,?,?)`,
			s.ID, w.AssetClass, w.Weight); err != nil {
			return wrapSQL("insert scenario weight", err)
		}
	}
	for _, t := range s.RegionTargets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO allocation_scenario_region_targets
				(scenario_id, asset_class, region, weight_within_class) VALUES (?,?,?,?)`,
			s.ID, t.AssetClass, t.Region, t.WeightWithinClass); err != nil {
			return wrapSQL("insert scenario region target", err)
		}
	}
	return wrapSQL("commit update scenario tx", tx.Commit())
}

func (r *ScenarioRepo) Delete(ctx context.Context, id string) error {
	s, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.IsBuiltin {
		return ErrBuiltinScenario
	}
	if s.PlanCount > 0 {
		return ErrScenarioInUse
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM allocation_scenarios WHERE id=?`, id)
	if err != nil {
		return wrapSQL("delete scenario", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrScenarioNotFound
	}
	return nil
}
